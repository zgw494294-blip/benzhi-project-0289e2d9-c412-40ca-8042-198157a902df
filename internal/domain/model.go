package domain

import (
	"errors"
	"time"
)

// SamplingBatch 是从采样建档推进到发布的聚合根。
type SamplingBatch struct {
	BatchID      string             `json:"batchID"`
	RiverName    string             `json:"riverName"`
	SamplingDate string             `json:"samplingDate"`
	Collector    string             `json:"collector"`
	Sites        []SamplingSite     `json:"sites"`
	Samples      []Sample           `json:"samples"`
	Status       BatchStatus        `json:"status"`
	Version      int64              `json:"version"`
	Results      []SequencingResult `json:"results"`
	Review       *QualityReview     `json:"review,omitempty"`
	Snapshot     *FrozenSnapshot    `json:"snapshot,omitempty"`
	Credential   *ReleaseCredential `json:"credential,omitempty"`
	CreatedAt    time.Time          `json:"createdAt"`
	UpdatedAt    time.Time          `json:"updatedAt"`
}

func (b *SamplingBatch) Clone() *SamplingBatch {
	if b == nil {
		return nil
	}
	copied := *b
	return &copied
}

func (b *SamplingBatch) FindSample(id string) (*Sample, bool) {
	for i := range b.Samples {
		if b.Samples[i].SampleID == id {
			return &b.Samples[i], true
		}
	}
	return nil, false
}

func (b *SamplingBatch) FindResult(id string) (*SequencingResult, bool) {
	for i := range b.Results {
		if b.Results[i].ResultID == id {
			return &b.Results[i], true
		}
	}
	return nil, false
}

func (b *SamplingBatch) ActiveResultForSample(sampleID string) (*SequencingResult, bool) {
	for i := len(b.Results) - 1; i >= 0; i-- {
		if b.Results[i].SampleID == sampleID && b.Results[i].Status != ResultSuperseded {
			return &b.Results[i], true
		}
	}
	return nil, false
}

func (b *SamplingBatch) EnsureMutable() error {
	if b.Status == BatchFrozen || b.Status == BatchReleased {
		return errors.New("批次已经冻结，不能再修改样本或证据")
	}
	return nil
}

func (b *SamplingBatch) Touch(now time.Time) {
	b.Version++
	b.UpdatedAt = now.UTC()
}

func cloneSlice[T any](input []T) []T {
	return append([]T(nil), input...)
}
