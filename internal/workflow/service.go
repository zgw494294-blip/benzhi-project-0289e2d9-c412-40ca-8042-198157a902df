package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"edna-workbench/internal/domain"
	"edna-workbench/internal/store"
)

type Clock func() time.Time

type Service struct {
	store *store.Ledger
	now   Clock
	locks sync.Map
}

type CommandMeta struct {
	ExpectedVersion int64  `json:"expectedVersion"`
	IdempotencyKey  string `json:"idempotencyKey"`
}

type CommandResult struct {
	Batch   *domain.SamplingBatch `json:"batch"`
	Receipt store.Receipt         `json:"receipt"`
}

type CreateBatchCommand struct {
	Meta         CommandMeta           `json:"meta"`
	BatchID      string                `json:"batchID"`
	RiverName    string                `json:"riverName"`
	SamplingDate string                `json:"samplingDate"`
	Collector    string                `json:"collector"`
	Sites        []domain.SamplingSite `json:"sites"`
	Samples      []domain.Sample       `json:"samples"`
}

type RegisterResultCommand struct {
	Meta                CommandMeta             `json:"meta"`
	BatchID             string                  `json:"batchID"`
	ResultID            string                  `json:"resultID"`
	SampleID            string                  `json:"sampleID"`
	ExtractionLot       string                  `json:"extractionLot"`
	RunID               string                  `json:"runID"`
	ReadCount           int64                   `json:"readCount"`
	Coverage            float64                 `json:"coverage"`
	NegativeControlRate float64                 `json:"negativeControlRate"`
	CandidateTaxa       []domain.CandidateTaxon `json:"candidateTaxa"`
	SupersedesResultID  string                  `json:"supersedesResultID"`
}

type QualityCheckCommand struct {
	Meta     CommandMeta `json:"meta"`
	BatchID  string      `json:"batchID"`
	ReviewID string      `json:"reviewID"`
}

type RetestCommand struct {
	Meta             CommandMeta `json:"meta"`
	BatchID          string      `json:"batchID"`
	RequestID        string      `json:"requestID"`
	SampleID         string      `json:"sampleID"`
	OriginalResultID string      `json:"originalResultID"`
	Reason           string      `json:"reason"`
	RequestedBy      string      `json:"requestedBy"`
}

type ExpertReviewCommand struct {
	Meta     CommandMeta           `json:"meta"`
	BatchID  string                `json:"batchID"`
	Expert   string                `json:"expert"`
	Decision domain.ReviewDecision `json:"decision"`
	Remarks  string                `json:"remarks"`
}

type FreezeCommand struct {
	Meta     CommandMeta `json:"meta"`
	BatchID  string      `json:"batchID"`
	FrozenBy string      `json:"frozenBy"`
}

type IssueCredentialCommand struct {
	Meta         CommandMeta `json:"meta"`
	BatchID      string      `json:"batchID"`
	CredentialID string      `json:"credentialID"`
	IssuedBy     string      `json:"issuedBy"`
}

type VerificationRequest struct {
	CredentialID     string `json:"credentialID"`
	SnapshotDigest   string `json:"snapshotDigest"`
	VerificationCode string `json:"verificationCode"`
}

type VerificationResult struct {
	Valid      bool                      `json:"valid"`
	Message    string                    `json:"message"`
	Credential *domain.ReleaseCredential `json:"credential,omitempty"`
}

type BatchSummary struct {
	BatchID       string             `json:"batchID"`
	RiverName     string             `json:"riverName"`
	SamplingDate  string             `json:"samplingDate"`
	Status        domain.BatchStatus `json:"status"`
	Version       int64              `json:"version"`
	SampleCount   int                `json:"sampleCount"`
	ResultCount   int                `json:"resultCount"`
	FailedCount   int                `json:"failedCount"`
	RetestPending int                `json:"retestPending"`
	UpdatedAt     time.Time          `json:"updatedAt"`
}

type ReadinessCheck struct {
	Code    string `json:"code"`
	Label   string `json:"label"`
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

type ReleaseReadiness struct {
	BatchID string           `json:"batchID"`
	Version int64            `json:"version"`
	Ready   bool             `json:"ready"`
	Checks  []ReadinessCheck `json:"checks"`
}

func NewService(ledger *store.Ledger, clock Clock) *Service {
	if clock == nil {
		clock = time.Now
	}
	return &Service{store: ledger, now: clock}
}

func (s *Service) CreateBatch(command CreateBatchCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, true); err != nil {
		return CommandResult{}, err
	}
	unlock := s.lock(command.BatchID)
	defer unlock()
	if result, ok := s.replay(command.BatchID, command.Meta.IdempotencyKey); ok {
		return result, nil
	}
	now := s.now().UTC()
	batch, err := domain.NewSamplingBatch(domain.NewBatchInput{
		BatchID: command.BatchID, RiverName: command.RiverName, SamplingDate: command.SamplingDate,
		Collector: command.Collector, Sites: command.Sites, Samples: command.Samples,
	}, now)
	if err != nil {
		return CommandResult{}, err
	}
	receipt, err := s.store.Commit("batch.created", command.Meta.IdempotencyKey, 0, batch, now)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Batch: batch.Clone(), Receipt: receipt}, nil
}

func (s *Service) RegisterResult(command RegisterResultCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, false); err != nil {
		return CommandResult{}, err
	}
	return s.change(command.BatchID, command.Meta, "sequencing_result.registered", func(batch *domain.SamplingBatch, now time.Time) error {
		_, err := batch.RegisterResult(domain.ResultInput{
			ResultID: command.ResultID, SampleID: command.SampleID, ExtractionLot: command.ExtractionLot,
			RunID: command.RunID, ReadCount: command.ReadCount, Coverage: command.Coverage,
			NegativeControlRate: command.NegativeControlRate, CandidateTaxa: command.CandidateTaxa,
			SupersedesResultID: command.SupersedesResultID,
		}, now)
		return err
	})
}

func (s *Service) RegisterResultContext(ctx context.Context, command RegisterResultCommand) (CommandResult, error) {
	type outcome struct {
		result CommandResult
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		result, err := s.RegisterResult(command)
		completed <- outcome{result: result, err: err}
	}()
	select {
	case <-ctx.Done():
		return CommandResult{}, ctx.Err()
	case value := <-completed:
		return value.result, value.err
	}
}

func (s *Service) RunQualityCheck(command QualityCheckCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, false); err != nil {
		return CommandResult{}, err
	}
	return s.change(command.BatchID, command.Meta, "quality.checked", func(batch *domain.SamplingBatch, now time.Time) error {
		_, err := batch.RunQualityReview(command.ReviewID, now)
		return err
	})
}

func (s *Service) RequestRetest(command RetestCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, false); err != nil {
		return CommandResult{}, err
	}
	return s.change(command.BatchID, command.Meta, "retest.requested", func(batch *domain.SamplingBatch, now time.Time) error {
		return batch.RequestRetest(domain.RetestRequest{
			RequestID: command.RequestID, SampleID: command.SampleID, OriginalResultID: command.OriginalResultID,
			Reason: command.Reason, RequestedBy: command.RequestedBy,
		}, now)
	})
}

func (s *Service) SubmitExpertReview(command ExpertReviewCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, false); err != nil {
		return CommandResult{}, err
	}
	eventType := "expert.reviewed"
	if command.Decision == domain.DecisionApproved {
		eventType = "expert.approved"
	}
	return s.change(command.BatchID, command.Meta, eventType, func(batch *domain.SamplingBatch, now time.Time) error {
		return batch.SubmitExpertReview(command.Expert, command.Decision, command.Remarks, now)
	})
}

func (s *Service) Freeze(command FreezeCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, false); err != nil {
		return CommandResult{}, err
	}
	return s.change(command.BatchID, command.Meta, "release.frozen", func(batch *domain.SamplingBatch, now time.Time) error {
		_, err := batch.Freeze(command.FrozenBy, now)
		return err
	})
}

func (s *Service) IssueCredential(command IssueCredentialCommand) (CommandResult, error) {
	if err := validateMeta(command.Meta, false); err != nil {
		return CommandResult{}, err
	}
	return s.change(command.BatchID, command.Meta, "credential.issued", func(batch *domain.SamplingBatch, now time.Time) error {
		_, err := batch.IssueCredential(command.CredentialID, command.IssuedBy, now)
		return err
	})
}

func (s *Service) VerifyCredential(request VerificationRequest) (VerificationResult, error) {
	request.CredentialID = strings.TrimSpace(request.CredentialID)
	if request.CredentialID == "" {
		return VerificationResult{}, errors.New("凭据编号不能为空")
	}
	credential, err := s.store.FindCredential(request.CredentialID)
	if errors.Is(err, store.ErrNotFound) {
		return VerificationResult{Valid: false, Message: "未找到该科研发布凭据"}, nil
	}
	if err != nil {
		return VerificationResult{}, err
	}
	valid, message := domain.VerifyCredential(*credential, strings.TrimSpace(request.SnapshotDigest), strings.TrimSpace(request.VerificationCode))
	return VerificationResult{Valid: valid, Message: message, Credential: credential}, nil
}

func (s *Service) GetBatch(batchID string) (*domain.SamplingBatch, error) {
	if strings.TrimSpace(batchID) == "" {
		return nil, errors.New("批次编号不能为空")
	}
	return s.store.GetBatch(batchID)
}

func (s *Service) ListBatches() []BatchSummary {
	batches := s.store.ListBatches()
	summaries := make([]BatchSummary, 0, len(batches))
	for _, batch := range batches {
		summary := BatchSummary{
			BatchID: batch.BatchID, RiverName: batch.RiverName, SamplingDate: batch.SamplingDate,
			Status: batch.Status, Version: batch.Version, SampleCount: len(batch.Samples),
			ResultCount: len(batch.Results), UpdatedAt: batch.UpdatedAt,
		}
		if batch.Review != nil {
			summary.FailedCount = len(batch.Review.FailedItems)
			for _, request := range batch.Review.RetestRequests {
				if request.ResolvedAt.IsZero() {
					summary.RetestPending++
				}
			}
		}
		summaries = append(summaries, summary)
	}
	return summaries
}

func (s *Service) AuditEvents(batchID string) ([]store.Event, error) {
	if batchID != "" {
		if _, err := s.store.GetBatch(batchID); err != nil {
			return nil, err
		}
	}
	return s.store.Events(batchID)
}

func (s *Service) CheckReleaseReadiness(batchID string) (ReleaseReadiness, error) {
	batch, err := s.GetBatch(batchID)
	if err != nil {
		return ReleaseReadiness{}, err
	}
	checks := make([]ReadinessCheck, 0, 5)
	activeSamples := make(map[string]bool, len(batch.Samples))
	for _, result := range batch.Results {
		if result.Status == domain.ResultActive {
			activeSamples[result.SampleID] = true
		}
	}
	allSamplesReady := len(activeSamples) == len(batch.Samples)
	checks = append(checks, ReadinessCheck{Code: "active_results", Label: "有效测序结果", Ready: allSamplesReady, Message: fmt.Sprintf("%d/%d 个样本具有有效结果", len(activeSamples), len(batch.Samples))})
	qualityReady := batch.Review != nil && len(batch.Review.FailedItems) == 0
	checks = append(checks, ReadinessCheck{Code: "quality_passed", Label: "自动质量检查", Ready: qualityReady, Message: readinessMessage(qualityReady, "全部自动检查通过", "尚未检查或仍有失败项")})
	closedRetests := batch.Review != nil
	if batch.Review != nil {
		for _, request := range batch.Review.RetestRequests {
			if request.ResolvedAt.IsZero() {
				closedRetests = false
				break
			}
		}
	}
	checks = append(checks, ReadinessCheck{Code: "retests_closed", Label: "异常重测处置", Ready: closedRetests, Message: readinessMessage(closedRetests, "所有重测请求均有替代结果", "存在未完成重测或尚无复核记录")})
	expertReady := batch.Review != nil && batch.Review.Decision == domain.DecisionApproved && batch.Review.Expert != ""
	checks = append(checks, ReadinessCheck{Code: "expert_approved", Label: "专家鉴定复核", Ready: expertReady, Message: readinessMessage(expertReady, "专家已经提交通过结论", "专家尚未通过")})
	mutable := batch.Status != domain.BatchFrozen && batch.Status != domain.BatchReleased
	checks = append(checks, ReadinessCheck{Code: "not_already_frozen", Label: "冻结状态", Ready: mutable, Message: readinessMessage(mutable, "批次尚未冻结", "批次已经冻结或发布")})
	readiness := ReleaseReadiness{BatchID: batch.BatchID, Version: batch.Version, Ready: true, Checks: checks}
	for _, check := range checks {
		if !check.Ready {
			readiness.Ready = false
			break
		}
	}
	return readiness, nil
}

func (s *Service) Health() map[string]any {
	sequence, digest, batches := s.store.Stats()
	return map[string]any{
		"status": "ok", "schemaVersion": 1, "eventSequence": sequence,
		"lastDigest": digest, "batchCount": batches, "checkedAt": s.now().UTC(),
	}
}

func (s *Service) change(batchID string, meta CommandMeta, eventType string, mutation func(*domain.SamplingBatch, time.Time) error) (CommandResult, error) {
	if strings.TrimSpace(batchID) == "" {
		return CommandResult{}, errors.New("批次编号不能为空")
	}
	unlock := s.lock(batchID)
	defer unlock()
	if result, ok := s.replay(batchID, meta.IdempotencyKey); ok {
		return result, nil
	}
	batch, err := s.store.GetBatch(batchID)
	if err != nil {
		return CommandResult{}, err
	}
	if batch.Version != meta.ExpectedVersion {
		return CommandResult{}, &store.ConflictError{BatchID: batchID, Expected: meta.ExpectedVersion, Actual: batch.Version}
	}
	now := s.now().UTC()
	if err := mutation(batch, now); err != nil {
		return CommandResult{}, err
	}
	receipt, err := s.store.Commit(eventType, meta.IdempotencyKey, meta.ExpectedVersion, batch, now)
	if err != nil {
		return CommandResult{}, err
	}
	return CommandResult{Batch: batch.Clone(), Receipt: receipt}, nil
}

func (s *Service) replay(batchID, key string) (CommandResult, bool) {
	batch, receipt, ok := s.store.IdempotentResult(batchID, key)
	if !ok {
		return CommandResult{}, false
	}
	return CommandResult{Batch: batch, Receipt: receipt}, true
}

func (s *Service) lock(batchID string) func() {
	value, _ := s.locks.LoadOrStore(batchID, &sync.Mutex{})
	mutex := value.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func validateMeta(meta CommandMeta, create bool) error {
	if meta.ExpectedVersion < 0 {
		return errors.New("expectedVersion 不能为负数")
	}
	if create && meta.ExpectedVersion != 0 {
		return errors.New("创建批次的 expectedVersion 必须为 0")
	}
	meta.IdempotencyKey = strings.TrimSpace(meta.IdempotencyKey)
	if meta.IdempotencyKey == "" {
		return errors.New("idempotencyKey 不能为空")
	}
	if len(meta.IdempotencyKey) > 128 {
		return errors.New("idempotencyKey 不能超过 128 个字符")
	}
	return nil
}

func IsConflict(err error) (*store.ConflictError, bool) {
	var conflict *store.ConflictError
	if errors.As(err, &conflict) {
		return conflict, true
	}
	return nil, false
}

func IsNotFound(err error) bool {
	return errors.Is(err, store.ErrNotFound)
}

func ExplainState(batch *domain.SamplingBatch) string {
	if batch == nil {
		return "批次不存在"
	}
	switch batch.Status {
	case domain.BatchDraft:
		return "等待为全部样本登记测序结果"
	case domain.BatchResultsEntered:
		return "测序结果已登记，可以执行自动质量检查"
	case domain.BatchQualityFailed:
		return "质量检查未通过，需要发起重测或补充合格结果"
	case domain.BatchAwaitingExpert:
		return "自动质量检查已通过，等待物种鉴定专家复核"
	case domain.BatchApproved:
		return "专家复核已通过，可以冻结物种清单"
	case domain.BatchFrozen:
		return "清单已冻结，可以签发科研发布凭据"
	case domain.BatchReleased:
		return "科研发布凭据已签发，可公开验证"
	default:
		return fmt.Sprintf("未知状态: %s", batch.Status)
	}
}

func readinessMessage(ready bool, success, pending string) string {
	if ready {
		return success
	}
	return pending
}
