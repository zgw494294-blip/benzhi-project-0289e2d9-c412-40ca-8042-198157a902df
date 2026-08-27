package domain

import (
	"errors"
	"strings"
	"time"
)

func (b *SamplingBatch) RunQualityReview(reviewID string, now time.Time) (*QualityReview, error) {
	if err := b.EnsureMutable(); err != nil {
		return nil, err
	}
	if !identifierPattern.MatchString(reviewID) {
		return nil, errors.New("质检编号格式无效")
	}
	active := make(map[string]*SequencingResult)
	for i := range b.Results {
		result := &b.Results[i]
		if result.Status != ResultSuperseded {
			active[result.SampleID] = result
		}
	}
	items := make([]QualityCheckItem, 0, len(b.Samples)*4)
	failed := make([]string, 0)
	add := func(item QualityCheckItem) {
		items = append(items, item)
		if !item.Passed {
			failed = append(failed, item.Code+":"+item.SampleID)
		}
	}
	metadataOK := b.RiverName != "" && b.SamplingDate != "" && b.Collector != "" && len(b.Sites) > 0
	add(QualityCheckItem{Code: "metadata_complete", Label: "批次元数据完整性", Passed: metadataOK, Message: passMessage(metadataOK, "批次元数据完整", "河流、日期、采样员或采样点信息缺失")})
	for _, sample := range b.Samples {
		result, linked := active[sample.SampleID]
		add(QualityCheckItem{Code: "sample_linked", Label: "样本关联", SampleID: sample.SampleID, Passed: linked, Message: passMessage(linked, "样本已有有效测序结果", "样本缺少有效测序结果")})
		if !linked {
			continue
		}
		coverageOK := result.Coverage >= MinimumCoverage
		add(QualityCheckItem{Code: "coverage", Label: "测序覆盖度", SampleID: sample.SampleID, Passed: coverageOK, Observed: result.Coverage, Threshold: MinimumCoverage, Message: passMessage(coverageOK, "覆盖度达到阈值", "覆盖度低于阈值")})
		controlOK := result.NegativeControlRate <= MaximumNegativeControlRate
		add(QualityCheckItem{Code: "negative_control", Label: "阴性对照污染", SampleID: sample.SampleID, Passed: controlOK, Observed: result.NegativeControlRate, Threshold: MaximumNegativeControlRate, Message: passMessage(controlOK, "阴性对照污染率合格", "阴性对照污染率超出阈值")})
		readsOK := result.ReadCount >= MinimumReadCount
		add(QualityCheckItem{Code: "read_count", Label: "有效读数", SampleID: sample.SampleID, Passed: readsOK, Observed: float64(result.ReadCount), Threshold: float64(MinimumReadCount), Message: passMessage(readsOK, "有效读数达到阈值", "有效读数低于阈值")})
		if !coverageOK || !controlOK || !readsOK {
			result.Status = ResultFailed
		} else {
			result.Status = ResultActive
		}
	}
	retests := []RetestRequest(nil)
	if b.Review != nil {
		retests = cloneSlice(b.Review.RetestRequests)
	}
	b.Review = &QualityReview{ReviewID: reviewID, BatchID: b.BatchID, CheckItems: items, FailedItems: failed, RetestRequests: retests, Decision: DecisionPending, CheckedAt: now.UTC()}
	if len(failed) > 0 {
		b.Status = BatchQualityFailed
	} else {
		b.Status = BatchAwaitingExpert
	}
	b.Touch(now)
	return b.Review, nil
}

func (b *SamplingBatch) RequestRetest(request RetestRequest, now time.Time) error {
	if b.Status != BatchQualityFailed || b.Review == nil {
		return errors.New("只有质检失败的批次可以发起重测")
	}
	if !identifierPattern.MatchString(request.RequestID) || strings.TrimSpace(request.Reason) == "" || strings.TrimSpace(request.RequestedBy) == "" {
		return errors.New("重测请求编号、原因和申请人不能为空")
	}
	result, found := b.FindResult(request.OriginalResultID)
	if !found || result.SampleID != request.SampleID || result.Status != ResultFailed {
		return errors.New("原始失败结果不存在或与样本不匹配")
	}
	for _, existing := range b.Review.RetestRequests {
		if existing.RequestID == request.RequestID {
			return errors.New("重测请求编号已存在")
		}
		if existing.SampleID == request.SampleID && existing.ResolvedAt.IsZero() {
			return errors.New("该样本已有未完成的重测请求")
		}
	}
	request.RequestedAt = now.UTC()
	request.ResolvedAt = time.Time{}
	request.ReplacementResultID = ""
	b.Review.RetestRequests = append(b.Review.RetestRequests, request)
	b.Touch(now)
	return nil
}

func (b *SamplingBatch) SubmitExpertReview(expert string, decision ReviewDecision, remarks string, now time.Time) error {
	if b.Review == nil || (b.Status != BatchAwaitingExpert && b.Status != BatchQualityFailed) {
		return errors.New("当前批次尚未进入专家复核阶段")
	}
	expert = strings.TrimSpace(expert)
	remarks = strings.TrimSpace(remarks)
	if expert == "" {
		return errors.New("专家姓名不能为空")
	}
	if decision != DecisionApproved && decision != DecisionChanges && decision != DecisionRejected {
		return errors.New("专家复核结论无效")
	}
	if decision != DecisionApproved && remarks == "" {
		return errors.New("非通过结论必须填写整改或驳回意见")
	}
	if decision == DecisionApproved {
		if len(b.Review.FailedItems) > 0 {
			return errors.New("当前质量检查仍有失败项，请录入替代结果后重新执行质检")
		}
		for _, request := range b.Review.RetestRequests {
			if request.ResolvedAt.IsZero() {
				return errors.New("仍有未完成的重测请求")
			}
		}
		b.Status = BatchApproved
	} else if decision == DecisionChanges {
		b.Status = BatchAwaitingExpert
	} else {
		b.Status = BatchQualityFailed
	}
	b.Review.Expert = expert
	b.Review.Decision = decision
	b.Review.Remarks = remarks
	b.Review.ReviewedAt = now.UTC()
	b.Touch(now)
	return nil
}

func hasOpenRetest(review *QualityReview, sampleID, resultID string) bool {
	for _, request := range review.RetestRequests {
		if request.SampleID == sampleID && request.OriginalResultID == resultID && request.ResolvedAt.IsZero() {
			return true
		}
	}
	return false
}

func resolveRetest(review *QualityReview, sampleID, originalID, replacementID string, now time.Time) {
	for i := range review.RetestRequests {
		request := &review.RetestRequests[i]
		if request.SampleID == sampleID && request.OriginalResultID == originalID && request.ResolvedAt.IsZero() {
			request.ReplacementResultID = replacementID
			request.ResolvedAt = now.UTC()
			return
		}
	}
}

func passMessage(passed bool, success, failure string) string {
	if passed {
		return success
	}
	return failure
}
