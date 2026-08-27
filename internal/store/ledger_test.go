package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"edna-workbench/internal/domain"
)

func testBatch(t *testing.T, now time.Time) *domain.SamplingBatch {
	t.Helper()
	batch, err := domain.NewSamplingBatch(domain.NewBatchInput{
		BatchID: "BATCH-001", RiverName: "嘉陵江", SamplingDate: "2026-08-19", Collector: "测试员",
		Sites:   []domain.SamplingSite{{SiteID: "SITE-001", Name: "断面", Latitude: 30, Longitude: 105}},
		Samples: []domain.Sample{{SampleID: "SAMPLE-001", Barcode: "EDNA-0001", SiteID: "SITE-001", Matrix: "water"}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	return batch
}

func TestLedgerReplayIdempotencyAndConflict(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	ledger, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	batch := testBatch(t, now)
	receipt, err := ledger.Commit("batch.created", "create-1", 0, batch, now)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Sequence != 1 || receipt.Digest == "" {
		t.Fatalf("提交回执无效: %+v", receipt)
	}
	copyBatch, replayReceipt, ok := ledger.IdempotentResult(batch.BatchID, "create-1")
	if !ok || copyBatch.Version != 1 || !replayReceipt.Replayed {
		t.Fatal("未找到幂等命令结果")
	}
	batch.Touch(now.Add(time.Minute))
	if _, err := ledger.Commit("batch.changed", "change-1", 0, batch, now); err == nil {
		t.Fatal("过期 expectedVersion 应产生冲突")
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restored, err := reopened.GetBatch(batch.BatchID)
	if err != nil || restored.Version != 1 {
		t.Fatalf("账本重放未恢复版本 1: batch=%+v err=%v", restored, err)
	}
	sequence, digest, count := reopened.Stats()
	if sequence != 1 || digest != receipt.Digest || count != 1 {
		t.Fatalf("重放统计异常: %d %s %d", sequence, digest, count)
	}
}

func TestLedgerRejectsTamperedChain(t *testing.T) {
	directory := t.TempDir()
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	ledger, err := Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Commit("batch.created", "create-1", 0, testBatch(t, now), now); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "events.jsonl")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)/2] ^= 1
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(directory); err == nil {
		t.Fatal("被篡改的事件账本不应启动成功")
	}
}
