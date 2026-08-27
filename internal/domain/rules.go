package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	MinimumCoverage                  = 0.80
	MaximumNegativeControlRate       = 0.02
	MinimumReadCount           int64 = 1000
)

type ResultInput struct {
	ResultID            string
	SampleID            string
	ExtractionLot       string
	RunID               string
	ReadCount           int64
	Coverage            float64
	NegativeControlRate float64
	CandidateTaxa       []CandidateTaxon
	SupersedesResultID  string
}

func (b *SamplingBatch) RegisterResult(input ResultInput, now time.Time) (*SequencingResult, error) {
	if err := b.EnsureMutable(); err != nil {
		return nil, err
	}
	if !identifierPattern.MatchString(input.ResultID) || !identifierPattern.MatchString(input.SampleID) {
		return nil, errors.New("结果编号或样本编号格式无效")
	}
	if _, found := b.FindResult(input.ResultID); found {
		return nil, fmt.Errorf("测序结果已存在: %s", input.ResultID)
	}
	if _, found := b.FindSample(input.SampleID); !found {
		return nil, fmt.Errorf("样本不存在: %s", input.SampleID)
	}
	if strings.TrimSpace(input.ExtractionLot) == "" || strings.TrimSpace(input.RunID) == "" {
		return nil, errors.New("DNA提取批次和测序运行编号不能为空")
	}
	if input.ReadCount < 0 || input.Coverage < 0 || input.Coverage > 1 || input.NegativeControlRate < 0 || input.NegativeControlRate > 1 {
		return nil, errors.New("测序读数、覆盖度或阴性对照污染率超出允许范围")
	}
	if len(input.CandidateTaxa) == 0 {
		return nil, errors.New("至少需要一个物种候选结果")
	}
	seenTaxa := make(map[string]struct{})
	for i := range input.CandidateTaxa {
		candidate := &input.CandidateTaxa[i]
		candidate.ScientificName = strings.TrimSpace(candidate.ScientificName)
		candidate.Marker = strings.TrimSpace(candidate.Marker)
		if candidate.ScientificName == "" || candidate.Marker == "" || candidate.Confidence < 0 || candidate.Confidence > 1 || candidate.ReadCount < 0 {
			return nil, fmt.Errorf("第 %d 个物种候选证据无效", i+1)
		}
		key := strings.ToLower(candidate.ScientificName) + "\x00" + strings.ToLower(candidate.Marker)
		if _, duplicate := seenTaxa[key]; duplicate {
			return nil, fmt.Errorf("物种候选重复: %s/%s", candidate.ScientificName, candidate.Marker)
		}
		seenTaxa[key] = struct{}{}
	}
	if _, found := b.ActiveResultForSample(input.SampleID); found && input.SupersedesResultID == "" {
		return nil, fmt.Errorf("样本 %s 已有有效结果，替代结果必须声明 supersedesResultID", input.SampleID)
	} else if !found && input.SupersedesResultID != "" {
		return nil, errors.New("没有可被替代的有效结果")
	}
	if input.SupersedesResultID != "" {
		old, found := b.FindResult(input.SupersedesResultID)
		if !found || old.SampleID != input.SampleID || old.Status == ResultSuperseded {
			return nil, errors.New("被替代结果不存在、样本不一致或已经被替代")
		}
		if b.Review == nil || !hasOpenRetest(b.Review, input.SampleID, input.SupersedesResultID) {
			return nil, errors.New("替代测序结果必须对应一个未完成的重测请求")
		}
	}
	result := SequencingResult{
		ResultID: input.ResultID, SampleID: input.SampleID,
		ExtractionLot: strings.TrimSpace(input.ExtractionLot), RunID: strings.TrimSpace(input.RunID),
		ReadCount: input.ReadCount, Coverage: input.Coverage, NegativeControlRate: input.NegativeControlRate,
		CandidateTaxa: cloneSlice(input.CandidateTaxa), SupersedesResultID: input.SupersedesResultID,
		Status: ResultActive, RecordedAt: now.UTC(),
	}
	digest, err := EvidenceDigest(result)
	if err != nil {
		return nil, fmt.Errorf("计算证据摘要: %w", err)
	}
	result.EvidenceDigest = digest
	if input.SupersedesResultID != "" {
		old, _ := b.FindResult(input.SupersedesResultID)
		old.Status = ResultSuperseded
		resolveRetest(b.Review, input.SampleID, input.SupersedesResultID, result.ResultID, now)
		b.Review.Decision = DecisionPending
		b.Review.Expert = ""
		b.Review.Remarks = ""
		b.Review.ReviewedAt = time.Time{}
	}
	b.Results = append(b.Results, result)
	if len(b.Results) > 0 {
		b.Status = BatchResultsEntered
	}
	b.Touch(now)
	return &b.Results[len(b.Results)-1], nil
}
