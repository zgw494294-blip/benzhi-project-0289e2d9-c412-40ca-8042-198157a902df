package domain

import (
	"testing"
	"time"
)

func TestCompleteDomainFlowWithRetest(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	batch, err := NewSamplingBatch(NewBatchInput{
		BatchID: "BATCH-001", RiverName: "岷江", SamplingDate: "2026-08-19", Collector: "采样员甲",
		Sites:   []SamplingSite{{SiteID: "SITE-001", Name: "北岸", Latitude: 30.5, Longitude: 103.9}},
		Samples: []Sample{{SampleID: "SAMPLE-001", Barcode: "EDNA-0001", SiteID: "SITE-001", Matrix: "water"}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	original, err := batch.RegisterResult(ResultInput{
		ResultID: "RESULT-001", SampleID: "SAMPLE-001", ExtractionLot: "EXT-001", RunID: "RUN-001",
		ReadCount: 500, Coverage: .60, NegativeControlRate: .05,
		CandidateTaxa: []CandidateTaxon{{ScientificName: "Carassius auratus", CommonName: "鲫", Confidence: .90, ReadCount: 300, Marker: "12S"}},
	}, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	review, err := batch.RunQualityReview("REVIEW-001", now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != BatchQualityFailed || len(review.FailedItems) != 3 || original.Status != ResultFailed {
		t.Fatalf("异常结果未被正确识别: status=%s failures=%v result=%s", batch.Status, review.FailedItems, original.Status)
	}
	err = batch.RequestRetest(RetestRequest{RequestID: "RETEST-001", SampleID: "SAMPLE-001", OriginalResultID: "RESULT-001", Reason: "污染率和覆盖度不合格", RequestedBy: "实验员乙"}, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := batch.RegisterResult(ResultInput{
		ResultID: "RESULT-002", SampleID: "SAMPLE-001", ExtractionLot: "EXT-002", RunID: "RUN-002",
		ReadCount: 13000, Coverage: .94, NegativeControlRate: .004, SupersedesResultID: "RESULT-001",
		CandidateTaxa: []CandidateTaxon{{ScientificName: "Carassius auratus", CommonName: "鲫", Confidence: .98, ReadCount: 8000, Marker: "12S"}},
	}, now.Add(4*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if replacement.EvidenceDigest == "" || batch.Results[0].Status != ResultSuperseded || batch.Review.RetestRequests[0].ReplacementResultID != "RESULT-002" {
		t.Fatal("替代关系、证据摘要或重测处置未被保存")
	}
	review, err = batch.RunQualityReview("REVIEW-002", now.Add(5*time.Minute))
	if err != nil || len(review.FailedItems) != 0 || batch.Status != BatchAwaitingExpert {
		t.Fatalf("替代结果质检应通过: review=%+v err=%v", review, err)
	}
	if err := batch.SubmitExpertReview("专家丙", DecisionApproved, "证据链完整", now.Add(6*time.Minute)); err != nil {
		t.Fatal(err)
	}
	snapshot, err := batch.Freeze("发布人丁", now.Add(7*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Taxa) != 1 || snapshot.Digest == "" || snapshot.Taxa[0].ScientificName != "Carassius auratus" {
		t.Fatalf("冻结快照异常: %+v", snapshot)
	}
	credential, err := batch.IssueCredential("CRED-001", "发布人丁", now.Add(8*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	valid, message := VerifyCredential(*credential, snapshot.Digest, credential.VerificationCode)
	if !valid {
		t.Fatalf("凭据应验证有效: %s", message)
	}
	valid, _ = VerifyCredential(*credential, "tampered", credential.VerificationCode)
	if valid {
		t.Fatal("摘要被修改时不应验证通过")
	}
}

func TestValidationAndFrozenMutation(t *testing.T) {
	now := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	_, err := NewSamplingBatch(NewBatchInput{BatchID: "B-1", RiverName: "河流", SamplingDate: "2026-08-19", Collector: "甲", Sites: []SamplingSite{{SiteID: "SITE-1", Name: "点位"}}, Samples: []Sample{{SampleID: "SAMPLE-1", Barcode: "BAD", SiteID: "SITE-1"}}}, now)
	if err == nil {
		t.Fatal("过短条码应被拒绝")
	}
	batch := &SamplingBatch{BatchID: "BATCH-001", Status: BatchFrozen}
	_, err = batch.RegisterResult(ResultInput{}, now)
	if err == nil {
		t.Fatal("冻结批次应拒绝新结果")
	}
}
