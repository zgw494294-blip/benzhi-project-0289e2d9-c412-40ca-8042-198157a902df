package domain

import "time"

type FrozenTaxon struct {
	ScientificName  string   `json:"scientificName"`
	CommonName      string   `json:"commonName,omitempty"`
	Markers         []string `json:"markers"`
	SampleIDs       []string `json:"sampleIDs"`
	EvidenceDigests []string `json:"evidenceDigests"`
	MaxConfidence   float64  `json:"maxConfidence"`
}

type FrozenSnapshot struct {
	SchemaVersion   int           `json:"schemaVersion"`
	BatchID         string        `json:"batchID"`
	BatchVersion    int64         `json:"batchVersion"`
	RiverName       string        `json:"riverName"`
	SamplingDate    string        `json:"samplingDate"`
	Taxa            []FrozenTaxon `json:"taxa"`
	EvidenceSummary string        `json:"evidenceSummary"`
	FrozenBy        string        `json:"frozenBy"`
	FrozenAt        time.Time     `json:"frozenAt"`
	Digest          string        `json:"digest"`
}

type ReleaseCredential struct {
	CredentialID     string          `json:"credentialID"`
	BatchID          string          `json:"batchID"`
	SnapshotDigest   string          `json:"snapshotDigest"`
	TaxaCount        int             `json:"taxaCount"`
	IssuedBy         string          `json:"issuedBy"`
	IssuedAt         time.Time       `json:"issuedAt"`
	VerificationCode string          `json:"verificationCode"`
	State            CredentialState `json:"state"`
}
