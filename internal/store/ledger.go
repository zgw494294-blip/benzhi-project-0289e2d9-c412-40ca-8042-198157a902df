package store

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"edna-workbench/internal/domain"
)

const schemaVersion = 1

var ErrNotFound = errors.New("记录不存在")

type ConflictError struct {
	BatchID  string `json:"batchID"`
	Expected int64  `json:"expectedVersion"`
	Actual   int64  `json:"actualVersion"`
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf("批次 %s 版本冲突：期望 %d，当前 %d", e.BatchID, e.Expected, e.Actual)
}

type Event struct {
	SchemaVersion  int                   `json:"schemaVersion"`
	Sequence       int64                 `json:"sequence"`
	EventID        string                `json:"eventID"`
	EventType      string                `json:"eventType"`
	BatchID        string                `json:"batchID"`
	BatchVersion   int64                 `json:"batchVersion"`
	IdempotencyKey string                `json:"idempotencyKey,omitempty"`
	OccurredAt     time.Time             `json:"occurredAt"`
	PreviousDigest string                `json:"previousDigest"`
	Payload        *domain.SamplingBatch `json:"payload"`
	Digest         string                `json:"digest"`
}

type Receipt struct {
	Sequence     int64  `json:"sequence"`
	EventID      string `json:"eventID"`
	BatchID      string `json:"batchID"`
	BatchVersion int64  `json:"batchVersion"`
	Digest       string `json:"digest"`
	Replayed     bool   `json:"replayed"`
}

type idempotentEntry struct {
	receipt Receipt
	batch   *domain.SamplingBatch
}

type diskSnapshot struct {
	SchemaVersion int                              `json:"schemaVersion"`
	LastSequence  int64                            `json:"lastSequence"`
	LastDigest    string                           `json:"lastDigest"`
	GeneratedAt   time.Time                        `json:"generatedAt"`
	Batches       map[string]*domain.SamplingBatch `json:"batches"`
}

type Ledger struct {
	mu               sync.RWMutex
	directory        string
	ledgerPath       string
	snapshotPath     string
	file             *os.File
	sequence         int64
	lastDigest       string
	batches          map[string]*domain.SamplingBatch
	idempotency      map[string]idempotentEntry
	credentialMisses map[string]struct{}
}

func Open(directory string) (*Ledger, error) {
	if strings.TrimSpace(directory) == "" {
		return nil, errors.New("数据目录不能为空")
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return nil, fmt.Errorf("创建数据目录: %w", err)
	}
	ledgerPath := filepath.Join(directory, "events.jsonl")
	file, err := os.OpenFile(ledgerPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o640)
	if err != nil {
		return nil, fmt.Errorf("打开事件账本: %w", err)
	}
	ledger := &Ledger{
		directory: directory, ledgerPath: ledgerPath, snapshotPath: filepath.Join(directory, "snapshot.json"),
		file: file, batches: make(map[string]*domain.SamplingBatch), idempotency: make(map[string]idempotentEntry),
		credentialMisses: make(map[string]struct{}),
	}
	if err := ledger.replay(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := ledger.compareSnapshot(); err != nil {
		_ = file.Close()
		return nil, err
	}
	return ledger, nil
}

func (l *Ledger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

func (l *Ledger) Commit(eventType, idempotencyKey string, expectedVersion int64, batch *domain.SamplingBatch, now time.Time) (Receipt, error) {
	if batch == nil || strings.TrimSpace(batch.BatchID) == "" {
		return Receipt{}, errors.New("提交的批次不能为空")
	}
	if strings.TrimSpace(eventType) == "" {
		return Receipt{}, errors.New("事件类型不能为空")
	}
	if err := batch.ValidateIntegrity(); err != nil {
		return Receipt{}, fmt.Errorf("批次聚合完整性校验失败: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return Receipt{}, errors.New("事件账本已关闭")
	}
	compoundKey := makeIdempotencyKey(batch.BatchID, idempotencyKey)
	if idempotencyKey != "" {
		if existing, ok := l.idempotency[compoundKey]; ok {
			receipt := existing.receipt
			receipt.Replayed = true
			return receipt, nil
		}
	}
	current, exists := l.batches[batch.BatchID]
	actual := int64(0)
	if exists {
		actual = current.Version
	}
	if actual != expectedVersion {
		return Receipt{}, &ConflictError{BatchID: batch.BatchID, Expected: expectedVersion, Actual: actual}
	}
	if batch.Version != expectedVersion+1 {
		return Receipt{}, fmt.Errorf("提交批次版本无效：应为 %d，实际为 %d", expectedVersion+1, batch.Version)
	}
	event := Event{
		SchemaVersion: schemaVersion, Sequence: l.sequence + 1,
		EventID: fmt.Sprintf("evt-%020d", l.sequence+1), EventType: eventType,
		BatchID: batch.BatchID, BatchVersion: batch.Version, IdempotencyKey: idempotencyKey,
		OccurredAt: now.UTC(), PreviousDigest: l.lastDigest, Payload: batch.Clone(),
	}
	digest, err := eventDigest(event)
	if err != nil {
		return Receipt{}, err
	}
	event.Digest = digest
	line, err := json.Marshal(event)
	if err != nil {
		return Receipt{}, fmt.Errorf("编码事件: %w", err)
	}
	line = append(line, '\n')
	if _, err := l.file.Write(line); err != nil {
		return Receipt{}, fmt.Errorf("追加事件: %w", err)
	}
	if err := l.file.Sync(); err != nil {
		return Receipt{}, fmt.Errorf("同步事件账本: %w", err)
	}
	l.sequence = event.Sequence
	l.lastDigest = event.Digest
	l.batches[batch.BatchID] = batch.Clone()
	receipt := Receipt{Sequence: event.Sequence, EventID: event.EventID, BatchID: batch.BatchID, BatchVersion: batch.Version, Digest: event.Digest}
	if idempotencyKey != "" {
		l.idempotency[compoundKey] = idempotentEntry{receipt: receipt, batch: batch.Clone()}
	}
	if err := l.writeSnapshotLocked(now); err != nil {
		return Receipt{}, fmt.Errorf("事件已写入但快照更新失败: %w", err)
	}
	return receipt, nil
}

func (l *Ledger) IdempotentResult(batchID, key string) (*domain.SamplingBatch, Receipt, bool) {
	if key == "" {
		return nil, Receipt{}, false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	entry, ok := l.idempotency[makeIdempotencyKey(batchID, key)]
	if !ok {
		return nil, Receipt{}, false
	}
	receipt := entry.receipt
	receipt.Replayed = true
	return entry.batch.Clone(), receipt, true
}

func (l *Ledger) GetBatch(batchID string) (*domain.SamplingBatch, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	batch, ok := l.batches[batchID]
	if !ok {
		return nil, ErrNotFound
	}
	return batch.Clone(), nil
}

func (l *Ledger) ListBatches() []*domain.SamplingBatch {
	l.mu.RLock()
	defer l.mu.RUnlock()
	items := make([]*domain.SamplingBatch, 0, len(l.batches))
	for _, batch := range l.batches {
		items = append(items, batch.Clone())
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].BatchID < items[j].BatchID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items
}

func (l *Ledger) FindCredential(credentialID string) (*domain.ReleaseCredential, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, cachedMiss := l.credentialMisses[credentialID]; cachedMiss {
		return nil, ErrNotFound
	}
	for _, batch := range l.batches {
		if batch.Credential != nil && batch.Credential.CredentialID == credentialID {
			credential := *batch.Credential
			return &credential, nil
		}
	}
	l.credentialMisses[credentialID] = struct{}{}
	return nil, ErrNotFound
}

func (l *Ledger) Events(batchID string) ([]Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	defer func() { _, _ = l.file.Seek(0, io.SeekEnd) }()
	scanner := bufio.NewScanner(l.file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var events []Event
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return nil, fmt.Errorf("解析事件: %w", err)
		}
		if batchID == "" || event.BatchID == batchID {
			event.Payload = nil
			events = append(events, event)
		}
	}
	return events, scanner.Err()
}

func (l *Ledger) Stats() (int64, string, int) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.sequence, l.lastDigest, len(l.batches)
}

func (l *Ledger) replay() error {
	if _, err := l.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("定位事件账本: %w", err)
	}
	scanner := bufio.NewScanner(l.file)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	var previous string
	var sequence int64
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		if len(bytes.TrimSpace(scanner.Bytes())) == 0 {
			return fmt.Errorf("事件账本第 %d 行为空", lineNumber)
		}
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return fmt.Errorf("解析事件账本第 %d 行: %w", lineNumber, err)
		}
		if event.SchemaVersion != schemaVersion {
			return fmt.Errorf("事件账本第 %d 行 schemaVersion 不受支持", lineNumber)
		}
		if event.Sequence != sequence+1 || event.PreviousDigest != previous {
			return fmt.Errorf("事件账本第 %d 行序号或前序摘要断裂", lineNumber)
		}
		actualDigest, err := eventDigest(event)
		if err != nil || actualDigest != event.Digest {
			return fmt.Errorf("事件账本第 %d 行摘要校验失败", lineNumber)
		}
		if event.Payload == nil || event.Payload.BatchID != event.BatchID || event.Payload.Version != event.BatchVersion {
			return fmt.Errorf("事件账本第 %d 行投影载荷无效", lineNumber)
		}
		if err := event.Payload.ValidateIntegrity(); err != nil {
			return fmt.Errorf("事件账本第 %d 行聚合完整性校验失败: %w", lineNumber, err)
		}
		if current := l.batches[event.BatchID]; current != nil && event.BatchVersion != current.Version+1 {
			return fmt.Errorf("事件账本第 %d 行批次版本不连续", lineNumber)
		}
		l.batches[event.BatchID] = event.Payload.Clone()
		receipt := Receipt{Sequence: event.Sequence, EventID: event.EventID, BatchID: event.BatchID, BatchVersion: event.BatchVersion, Digest: event.Digest}
		if event.IdempotencyKey != "" {
			compound := makeIdempotencyKey(event.BatchID, event.IdempotencyKey)
			if _, duplicate := l.idempotency[compound]; duplicate {
				return fmt.Errorf("事件账本第 %d 行幂等键重复", lineNumber)
			}
			l.idempotency[compound] = idempotentEntry{receipt: receipt, batch: event.Payload.Clone()}
		}
		sequence = event.Sequence
		previous = event.Digest
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取事件账本: %w", err)
	}
	l.sequence = sequence
	l.lastDigest = previous
	_, err := l.file.Seek(0, io.SeekEnd)
	return err
}

func (l *Ledger) compareSnapshot() error {
	raw, err := os.ReadFile(l.snapshotPath)
	if errors.Is(err, os.ErrNotExist) {
		if l.sequence > 0 {
			return l.writeSnapshotLocked(time.Now())
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取快照: %w", err)
	}
	var snapshot diskSnapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("解析快照: %w", err)
	}
	if snapshot.SchemaVersion != schemaVersion || snapshot.LastSequence != l.sequence || snapshot.LastDigest != l.lastDigest {
		return l.writeSnapshotLocked(time.Now())
	}
	if len(snapshot.Batches) != len(l.batches) {
		return l.writeSnapshotLocked(time.Now())
	}
	for id, current := range l.batches {
		stored := snapshot.Batches[id]
		if stored == nil || stored.Version != current.Version {
			return l.writeSnapshotLocked(time.Now())
		}
	}
	return nil
}

func (l *Ledger) writeSnapshotLocked(now time.Time) error {
	snapshot := diskSnapshot{
		SchemaVersion: schemaVersion, LastSequence: l.sequence, LastDigest: l.lastDigest,
		GeneratedAt: now.UTC(), Batches: make(map[string]*domain.SamplingBatch, len(l.batches)),
	}
	for id, batch := range l.batches {
		snapshot.Batches[id] = batch.Clone()
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("编码快照: %w", err)
	}
	temporary, err := os.CreateTemp(l.directory, ".snapshot-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	cleanup := func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}
	if err := temporary.Chmod(0o640); err != nil {
		cleanup()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(temporaryName, l.snapshotPath); err != nil {
		cleanup()
		return err
	}
	directory, err := os.Open(l.directory)
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func eventDigest(event Event) (string, error) {
	event.Digest = ""
	raw, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("生成事件摘要: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func makeIdempotencyKey(batchID, key string) string {
	return batchID + "\x00" + key
}
