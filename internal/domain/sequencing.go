package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

type CandidateTaxon struct {
	ScientificName string  `json:"scientificName"`
	CommonName     string  `json:"commonName,omitempty"`
	Confidence     float64 `json:"confidence"`
	ReadCount      int64   `json:"readCount"`
	Marker         string  `json:"marker"`
}

type SequencingResult struct {
	ResultID            string           `json:"resultID"`
	SampleID            string           `json:"sampleID"`
	ExtractionLot       string           `json:"extractionLot"`
	RunID               string           `json:"runID"`
	ReadCount           int64            `json:"readCount"`
	Coverage            float64          `json:"coverage"`
	NegativeControlRate float64          `json:"negativeControlRate"`
	CandidateTaxa       []CandidateTaxon `json:"candidateTaxa"`
	EvidenceDigest      string           `json:"evidenceDigest"`
	SupersedesResultID  string           `json:"supersedesResultID,omitempty"`
	Status              ResultStatus     `json:"status"`
	RecordedAt          time.Time        `json:"recordedAt"`
}

func EvidenceDigest(result SequencingResult) (string, error) {
	copyResult := result
	copyResult.EvidenceDigest = ""
	copyResult.RecordedAt = time.Time{}
	// 处置状态会随质检与重测改变，不属于实验原始证据摘要。
	copyResult.Status = ""
	taxa := cloneSlice(copyResult.CandidateTaxa)
	sort.Slice(taxa, func(i, j int) bool {
		if taxa[i].ScientificName == taxa[j].ScientificName {
			return taxa[i].Marker < taxa[j].Marker
		}
		return taxa[i].ScientificName < taxa[j].ScientificName
	})
	copyResult.CandidateTaxa = taxa
	raw, err := json.Marshal(copyResult)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
