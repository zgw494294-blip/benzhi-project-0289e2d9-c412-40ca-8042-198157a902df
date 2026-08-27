package domain

import "time"

type QualityCheckItem struct {
	Code      string  `json:"code"`
	Label     string  `json:"label"`
	Passed    bool    `json:"passed"`
	Observed  float64 `json:"observed,omitempty"`
	Threshold float64 `json:"threshold,omitempty"`
	Message   string  `json:"message"`
	SampleID  string  `json:"sampleID,omitempty"`
}

type RetestRequest struct {
	RequestID           string    `json:"requestID"`
	SampleID            string    `json:"sampleID"`
	OriginalResultID    string    `json:"originalResultID"`
	Reason              string    `json:"reason"`
	RequestedBy         string    `json:"requestedBy"`
	RequestedAt         time.Time `json:"requestedAt"`
	ReplacementResultID string    `json:"replacementResultID,omitempty"`
	ResolvedAt          time.Time `json:"resolvedAt,omitempty"`
}

type QualityReview struct {
	ReviewID       string             `json:"reviewID"`
	BatchID        string             `json:"batchID"`
	CheckItems     []QualityCheckItem `json:"checkItems"`
	FailedItems    []string           `json:"failedItems"`
	RetestRequests []RetestRequest    `json:"retestRequests"`
	Expert         string             `json:"expert,omitempty"`
	Decision       ReviewDecision     `json:"decision"`
	Remarks        string             `json:"remarks,omitempty"`
	ReviewedAt     time.Time          `json:"reviewedAt,omitempty"`
	CheckedAt      time.Time          `json:"checkedAt"`
}
