package protocol

// ErrorCode is the machine-readable half of an Error.
type ErrorCode string

const (
	// ErrAuthFailed means the Hello token was not recognized.
	ErrAuthFailed ErrorCode = "AUTH_FAILED"
	// ErrUnsupportedVersion means the two sides share no protocol version.
	ErrUnsupportedVersion ErrorCode = "UNSUPPORTED_VERSION"
	// ErrTaskNotFound means the referenced task is unknown to the receiver.
	ErrTaskNotFound ErrorCode = "TASK_NOT_FOUND"
	// ErrBusy means concurrency is exhausted and the task was queued instead of
	// started. It is a delay, not a failure.
	ErrBusy ErrorCode = "BUSY"
	// ErrBudgetExceeded means a task or daily spend cap was hit and the agent
	// was stopped.
	ErrBudgetExceeded ErrorCode = "BUDGET_EXCEEDED"
	// ErrCredMissing means a credential the task needs is not configured on the
	// VPS.
	ErrCredMissing ErrorCode = "CRED_MISSING"
	// ErrInternal is an unexpected failure on the sending side.
	ErrInternal ErrorCode = "INTERNAL"
)

// Error is the failure frame, valid in both directions.
//
// Ref carries the Envelope ID of the message that provoked it, which is what
// lets a sender match a failure to its request. It is empty for failures that
// were nobody's request.
type Error struct {
	Code ErrorCode `json:"code"`
	Ref  string    `json:"ref,omitempty"`
	Msg  string    `json:"msg,omitempty"`
}

// Error implements the error interface, so a decoded Error can be returned as
// one directly.
func (e Error) Error() string {
	if e.Msg == "" {
		return string(e.Code)
	}
	return string(e.Code) + ": " + e.Msg
}
