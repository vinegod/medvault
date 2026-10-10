package models

// Error codes sent in the API envelope { "error": { "code": "...", "message": "..." } }.
// Must stay in sync with the table in docs/API.md.
const (
	CodeValidation   = "VALIDATION_ERROR"
	CodeUnauthorized = "UNAUTHORIZED"
	CodeForbidden    = "FORBIDDEN"
	CodeNotFound     = "NOT_FOUND"
	CodeConflict     = "CONFLICT"
	CodeRateLimited  = "RATE_LIMITED"
	CodeInternal     = "INTERNAL"

	// CodeOwnerTransferRequired is returned when an owner tries to leave a
	// household that still has other members. The client must call the
	// transfer endpoint to promote another member to owner first.
	CodeOwnerTransferRequired = "OWNER_TRANSFER_REQUIRED"

	// CodeRegistrationClosed: register under REGISTRATION_MODE=closed.
	CodeRegistrationClosed = "REGISTRATION_CLOSED"

	// CodeInviteRequired: register under REGISTRATION_MODE=invite without
	// an invite_code.
	CodeInviteRequired = "INVITE_REQUIRED"
)
