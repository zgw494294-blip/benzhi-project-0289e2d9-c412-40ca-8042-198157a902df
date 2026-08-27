package domain

type BatchStatus string

const (
	BatchDraft          BatchStatus = "draft"
	BatchResultsEntered BatchStatus = "results_entered"
	BatchQualityFailed  BatchStatus = "quality_failed"
	BatchAwaitingExpert BatchStatus = "awaiting_expert"
	BatchApproved       BatchStatus = "approved"
	BatchFrozen         BatchStatus = "frozen"
	BatchReleased       BatchStatus = "released"
)

type ResultStatus string

const (
	ResultActive     ResultStatus = "active"
	ResultFailed     ResultStatus = "failed"
	ResultSuperseded ResultStatus = "superseded"
)

type ReviewDecision string

const (
	DecisionPending  ReviewDecision = "pending"
	DecisionChanges  ReviewDecision = "changes_required"
	DecisionApproved ReviewDecision = "approved"
	DecisionRejected ReviewDecision = "rejected"
)

type CredentialState string

const (
	CredentialValid   CredentialState = "valid"
	CredentialRevoked CredentialState = "revoked"
)
