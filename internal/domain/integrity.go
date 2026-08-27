package domain

import (
	"errors"
	"fmt"
	"strings"
)

// ValidateIntegrity verifies invariants that must also hold for aggregates restored
// from the event ledger. It deliberately does not mutate or normalize the batch.
func (b *SamplingBatch) ValidateIntegrity() error {
	if b == nil {
		return errors.New("批次聚合为空")
	}
	if !identifierPattern.MatchString(b.BatchID) || b.Version < 1 {
		return errors.New("批次编号或版本无效")
	}
	if b.CreatedAt.IsZero() || b.UpdatedAt.IsZero() || b.UpdatedAt.Before(b.CreatedAt) {
		return errors.New("批次创建或更新时间无效")
	}
	if len(b.Sites) == 0 || len(b.Samples) == 0 {
		return errors.New("批次缺少采样点或样本")
	}
	if err := validateSitesAndSamples(cloneSlice(b.Sites), cloneSlice(b.Samples)); err != nil {
		return fmt.Errorf("采样元数据完整性: %w", err)
	}
	sampleIDs := make(map[string]struct{}, len(b.Samples))
	for _, sample := range b.Samples {
		sampleIDs[sample.SampleID] = struct{}{}
	}
	resultIDs := make(map[string]*SequencingResult, len(b.Results))
	activeBySample := make(map[string]string)
	for index := range b.Results {
		result := &b.Results[index]
		if !identifierPattern.MatchString(result.ResultID) || resultIDs[result.ResultID] != nil {
			return fmt.Errorf("测序结果编号无效或重复: %s", result.ResultID)
		}
		if _, ok := sampleIDs[result.SampleID]; !ok {
			return fmt.Errorf("测序结果 %s 引用了不存在的样本", result.ResultID)
		}
		if result.Status != ResultActive && result.Status != ResultFailed && result.Status != ResultSuperseded {
			return fmt.Errorf("测序结果 %s 状态无效", result.ResultID)
		}
		digest, err := EvidenceDigest(*result)
		if err != nil || digest != result.EvidenceDigest {
			return fmt.Errorf("测序结果 %s 证据摘要不一致", result.ResultID)
		}
		if result.Status != ResultSuperseded {
			if existing := activeBySample[result.SampleID]; existing != "" {
				return fmt.Errorf("样本 %s 同时存在多个未替代结果", result.SampleID)
			}
			activeBySample[result.SampleID] = result.ResultID
		}
		resultIDs[result.ResultID] = result
	}
	for _, result := range b.Results {
		if result.SupersedesResultID == "" {
			continue
		}
		original := resultIDs[result.SupersedesResultID]
		if original == nil || original.SampleID != result.SampleID || original.Status != ResultSuperseded {
			return fmt.Errorf("替代结果 %s 的原结果关系无效", result.ResultID)
		}
	}
	if b.Review != nil {
		if err := b.validateReviewIntegrity(resultIDs, sampleIDs); err != nil {
			return err
		}
	}
	if b.Snapshot != nil {
		if err := b.validateSnapshotIntegrity(); err != nil {
			return err
		}
	}
	if b.Credential != nil {
		if err := b.validateCredentialIntegrity(); err != nil {
			return err
		}
	}
	return b.validateStatusIntegrity()
}

func (b *SamplingBatch) validateReviewIntegrity(results map[string]*SequencingResult, samples map[string]struct{}) error {
	if b.Review.BatchID != b.BatchID || !identifierPattern.MatchString(b.Review.ReviewID) {
		return errors.New("质量复核与批次不匹配或编号无效")
	}
	requestIDs := make(map[string]struct{}, len(b.Review.RetestRequests))
	for _, request := range b.Review.RetestRequests {
		if !identifierPattern.MatchString(request.RequestID) {
			return errors.New("重测请求编号无效")
		}
		if _, duplicate := requestIDs[request.RequestID]; duplicate {
			return fmt.Errorf("重测请求编号重复: %s", request.RequestID)
		}
		requestIDs[request.RequestID] = struct{}{}
		if _, exists := samples[request.SampleID]; !exists {
			return fmt.Errorf("重测请求 %s 引用了不存在的样本", request.RequestID)
		}
		original := results[request.OriginalResultID]
		if original == nil || original.SampleID != request.SampleID {
			return fmt.Errorf("重测请求 %s 的原结果关系无效", request.RequestID)
		}
		if request.ResolvedAt.IsZero() != (request.ReplacementResultID == "") {
			return fmt.Errorf("重测请求 %s 的完成状态不一致", request.RequestID)
		}
		if request.ReplacementResultID != "" {
			replacement := results[request.ReplacementResultID]
			if replacement == nil || replacement.SupersedesResultID != request.OriginalResultID {
				return fmt.Errorf("重测请求 %s 的替代结果关系无效", request.RequestID)
			}
		}
	}
	if b.Review.Decision == DecisionApproved && (b.Review.Expert == "" || len(b.Review.FailedItems) != 0) {
		return errors.New("专家通过结论缺少专家信息或仍有失败项")
	}
	return nil
}

func (b *SamplingBatch) validateSnapshotIntegrity() error {
	if b.Snapshot.BatchID != b.BatchID || b.Snapshot.SchemaVersion != 1 || b.Snapshot.BatchVersion > b.Version {
		return errors.New("冻结快照的批次、模式版本或批次版本无效")
	}
	if len(b.Snapshot.Taxa) == 0 || b.Snapshot.FrozenAt.IsZero() || strings.TrimSpace(b.Snapshot.FrozenBy) == "" {
		return errors.New("冻结快照缺少物种、时间或负责人")
	}
	digest, err := SnapshotDigest(*b.Snapshot)
	if err != nil || digest != b.Snapshot.Digest {
		return errors.New("冻结快照摘要校验失败")
	}
	previousName := ""
	for _, taxon := range b.Snapshot.Taxa {
		if taxon.ScientificName == "" || len(taxon.SampleIDs) == 0 || len(taxon.EvidenceDigests) == 0 || taxon.MaxConfidence < .80 {
			return fmt.Errorf("冻结物种 %s 的证据不完整", taxon.ScientificName)
		}
		if previousName != "" && taxon.ScientificName <= previousName {
			return errors.New("冻结物种清单未按学名稳定排序或包含重复项")
		}
		previousName = taxon.ScientificName
	}
	return nil
}

func (b *SamplingBatch) validateCredentialIntegrity() error {
	credential := b.Credential
	if b.Snapshot == nil || credential.BatchID != b.BatchID || credential.SnapshotDigest != b.Snapshot.Digest {
		return errors.New("发布凭据未关联当前冻结快照")
	}
	if credential.TaxaCount != len(b.Snapshot.Taxa) || credential.IssuedAt.IsZero() || strings.TrimSpace(credential.IssuedBy) == "" {
		return errors.New("发布凭据的物种数或签发信息无效")
	}
	if credential.VerificationCode != VerificationCode(*credential) {
		return errors.New("发布凭据验证码校验失败")
	}
	return nil
}

func (b *SamplingBatch) validateStatusIntegrity() error {
	switch b.Status {
	case BatchDraft:
		if len(b.Results) != 0 || b.Review != nil || b.Snapshot != nil || b.Credential != nil {
			return errors.New("建档状态包含了不应存在的后续业务数据")
		}
	case BatchResultsEntered:
		if len(b.Results) == 0 {
			return errors.New("结果已登记状态缺少测序结果")
		}
	case BatchQualityFailed, BatchAwaitingExpert:
		if b.Review == nil {
			return errors.New("质量处置状态缺少质量复核记录")
		}
	case BatchApproved:
		if b.Review == nil || b.Review.Decision != DecisionApproved {
			return errors.New("专家通过状态缺少通过结论")
		}
	case BatchFrozen:
		if b.Snapshot == nil || b.Credential != nil {
			return errors.New("冻结状态的快照或凭据组合无效")
		}
	case BatchReleased:
		if b.Snapshot == nil || b.Credential == nil || b.Credential.State != CredentialValid {
			return errors.New("发布状态缺少有效快照或凭据")
		}
	default:
		return fmt.Errorf("未知批次状态: %s", b.Status)
	}
	return nil
}
