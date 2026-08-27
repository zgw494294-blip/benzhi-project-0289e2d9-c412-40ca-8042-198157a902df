package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"edna-workbench/internal/domain"
	"edna-workbench/internal/workflow"
)

func performSelfcheck(service *workflow.Service, address string, output io.Writer) error {
	now := time.Now().UTC()
	batchID := "SELF-CHECK-001"
	created, err := service.CreateBatch(workflow.CreateBatchCommand{
		Meta:    workflow.CommandMeta{ExpectedVersion: 0, IdempotencyKey: "self-create"},
		BatchID: batchID, RiverName: "自检河", SamplingDate: now.Format("2006-01-02"), Collector: "自检采样员",
		Sites:   []domain.SamplingSite{{SiteID: "SITE-001", Name: "自检断面", Latitude: 31.2, Longitude: 121.4}},
		Samples: []domain.Sample{{SampleID: "SAMPLE-001", Barcode: "SELF-0001", SiteID: "SITE-001", Matrix: "water", Collected: now.Format(time.RFC3339)}},
	})
	if err != nil {
		return fmt.Errorf("自检创建批次: %w", err)
	}
	failed, err := service.RegisterResult(workflow.RegisterResultCommand{
		Meta: workflow.CommandMeta{ExpectedVersion: created.Batch.Version, IdempotencyKey: "self-result-original"}, BatchID: batchID,
		ResultID: "RESULT-001", SampleID: "SAMPLE-001", ExtractionLot: "EXT-001", RunID: "RUN-001",
		ReadCount: 800, Coverage: .65, NegativeControlRate: .04,
		CandidateTaxa: []domain.CandidateTaxon{{ScientificName: "Cyprinus carpio", CommonName: "鲤", Confidence: .91, ReadCount: 500, Marker: "12S"}},
	})
	if err != nil {
		return fmt.Errorf("自检登记原始结果: %w", err)
	}
	checked, err := service.RunQualityCheck(workflow.QualityCheckCommand{Meta: workflow.CommandMeta{ExpectedVersion: failed.Batch.Version, IdempotencyKey: "self-quality-1"}, BatchID: batchID, ReviewID: "REVIEW-001"})
	if err != nil || checked.Batch.Status != domain.BatchQualityFailed {
		return fmt.Errorf("自检识别异常结果: %w", err)
	}
	retest, err := service.RequestRetest(workflow.RetestCommand{
		Meta: workflow.CommandMeta{ExpectedVersion: checked.Batch.Version, IdempotencyKey: "self-retest"}, BatchID: batchID,
		RequestID: "RETEST-001", SampleID: "SAMPLE-001", OriginalResultID: "RESULT-001", Reason: "覆盖度不足且阴性对照污染", RequestedBy: "自检实验员",
	})
	if err != nil {
		return fmt.Errorf("自检发起重测: %w", err)
	}
	replacement, err := service.RegisterResult(workflow.RegisterResultCommand{
		Meta: workflow.CommandMeta{ExpectedVersion: retest.Batch.Version, IdempotencyKey: "self-result-replacement"}, BatchID: batchID,
		ResultID: "RESULT-002", SampleID: "SAMPLE-001", ExtractionLot: "EXT-002", RunID: "RUN-002",
		ReadCount: 18000, Coverage: .96, NegativeControlRate: .003, SupersedesResultID: "RESULT-001",
		CandidateTaxa: []domain.CandidateTaxon{{ScientificName: "Cyprinus carpio", CommonName: "鲤", Confidence: .98, ReadCount: 9200, Marker: "12S"}},
	})
	if err != nil {
		return fmt.Errorf("自检登记替代结果: %w", err)
	}
	passed, err := service.RunQualityCheck(workflow.QualityCheckCommand{Meta: workflow.CommandMeta{ExpectedVersion: replacement.Batch.Version, IdempotencyKey: "self-quality-2"}, BatchID: batchID, ReviewID: "REVIEW-002"})
	if err != nil || passed.Batch.Status != domain.BatchAwaitingExpert {
		return fmt.Errorf("自检复查质量: %w", err)
	}
	approved, err := service.SubmitExpertReview(workflow.ExpertReviewCommand{Meta: workflow.CommandMeta{ExpectedVersion: passed.Batch.Version, IdempotencyKey: "self-expert"}, BatchID: batchID, Expert: "自检专家", Decision: domain.DecisionApproved, Remarks: "替代证据满足物种鉴定要求"})
	if err != nil {
		return fmt.Errorf("自检专家复核: %w", err)
	}
	frozen, err := service.Freeze(workflow.FreezeCommand{Meta: workflow.CommandMeta{ExpectedVersion: approved.Batch.Version, IdempotencyKey: "self-freeze"}, BatchID: batchID, FrozenBy: "自检发布人"})
	if err != nil {
		return fmt.Errorf("自检冻结: %w", err)
	}
	issued, err := service.IssueCredential(workflow.IssueCredentialCommand{Meta: workflow.CommandMeta{ExpectedVersion: frozen.Batch.Version, IdempotencyKey: "self-issue"}, BatchID: batchID, CredentialID: "CREDENTIAL-001", IssuedBy: "自检发布人"})
	if err != nil {
		return fmt.Errorf("自检签发凭据: %w", err)
	}
	verification, err := service.VerifyCredential(workflow.VerificationRequest{CredentialID: issued.Batch.Credential.CredentialID, SnapshotDigest: issued.Batch.Snapshot.Digest, VerificationCode: issued.Batch.Credential.VerificationCode})
	if err != nil || !verification.Valid {
		return fmt.Errorf("自检验证凭据失败: %w", err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	for _, path := range []string{"/api/health", "/", "/api/batches/" + batchID} {
		response, err := client.Get("http://" + address + path)
		if err != nil {
			return fmt.Errorf("自检 HTTP %s: %w", path, err)
		}
		_, readErr := io.Copy(io.Discard, response.Body)
		closeErr := response.Body.Close()
		if readErr != nil || closeErr != nil || response.StatusCode != http.StatusOK {
			return fmt.Errorf("自检 HTTP %s 返回异常状态 %d", path, response.StatusCode)
		}
	}
	fmt.Fprintf(output, "selfcheck: 批次 %s 已推进至 %s，凭据 %s 验证有效\n", batchID, issued.Batch.Status, issued.Batch.Credential.CredentialID)
	return nil
}
