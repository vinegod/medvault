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
)
