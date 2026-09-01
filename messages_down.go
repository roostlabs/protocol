package protocol

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// HelloOK accepts a connection and settles the protocol version.
type HelloOK struct {
	Proto    int    `json:"proto"`
	ServerTS int64  `json:"serverTs"` // unix milliseconds
	RunnerID string `json:"runnerId"`

	// LastSeq is the highest Seq Cloud has stored for each still-active task,
	// keyed by task id. After a reconnect the Runner replays everything newer
	// from its event-store, which is how a dropped channel costs no history.
	LastSeq map[string]uint64 `json:"lastSeq,omitempty"`
}

// HelloErr rejects a connection; Cloud closes it immediately after. Code is
// ErrAuthFailed or ErrUnsupportedVersion.
type HelloErr struct {
	Code ErrorCode `json:"code"`
	Msg  string    `json:"msg,omitempty"`
}

// Ticket identifies the work item a task came from.
//
// Title and Body carry the ticket's text, because the Runner has no connector
// to the task manager: it holds the repository credential, not the Jira or
// Linear one. Cloud reads the ticket and sends what the agent has to act on.
type Ticket struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
	Body     string `json:"body,omitempty"`
}

// TaskRun starts a task. With concurrency at 1, a TaskRun arriving while a
// sandbox is busy is answered with ErrBusy and queued.
//
// BudgetUSD caps spend for this task; the Runner stops the agent on breach and
// reports ErrBudgetExceeded. Zero means no task-level cap, leaving only whatever
// daily budget is configured on the VPS.
type TaskRun struct {
	TaskID    string  `json:"taskId"`
	Ticket    Ticket  `json:"ticket"`
	Repo      string  `json:"repo"`
	BudgetUSD float64 `json:"budgetUsd,omitempty"`
	TimeoutMs int64   `json:"timeoutMs,omitempty"`
}

// TaskCancel kills a task's sandbox.
type TaskCancel struct {
	TaskID string `json:"taskId"`
	Reason string `json:"reason,omitempty"`
}

// TaskApprove answers a task parked in TaskAwaitingApproval. StepID is the one
// from the AgentStepPayload that asked.
type TaskApprove struct {
	TaskID   string `json:"taskId"`
	StepID   string `json:"stepId"`
	Approved bool   `json:"approved"`
}

// RepoPrepare clones a repo if the VPS does not have it yet, and otherwise
// fetches. Repos persist between tasks along with their dependency caches.
type RepoPrepare struct {
	URL    string `json:"url"`
	Branch string `json:"branch,omitempty"`
}

// CredKey names a credential slot in the Runner's local config.
type CredKey string

const (
	CredGit         CredKey = "git"
	CredTaskManager CredKey = "taskManager"
	CredLLM         CredKey = "llm"
)

// CredSet writes a credential into the Runner's local config file.
//
// Managed mode only. In the default Local mode the developer sets values on the
// VPS themselves and this message is never sent at all. Even in Managed mode
// Value is relay-only: Cloud passes it down and must neither persist nor log it.
//
// An empty Value clears the slot, which is how a revoked token is taken out of
// use without shell access to the VPS.
//
// There is no acknowledgement message: a Runner that took the value answers
// with a fresh CredStatus, and one that refused it answers with an Error. The
// flags are the only thing Cloud is entitled to know either way.
type CredSet struct {
	Key   CredKey `json:"key"`
	Value string  `json:"value"`
}

// String redacts Value so a stray %v or %s cannot print a credential.
func (c CredSet) String() string {
	return fmt.Sprintf("CredSet{Key:%s Value:%s}", c.Key, redacted)
}

// LogValue redacts Value for slog, covering the path String does not.
func (c CredSet) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("key", string(c.Key)),
		slog.String("value", redacted),
	)
}

// QueryKind names what a Query asks for.
type QueryKind string

const (
	QueryTaskHistory QueryKind = "task_history"
	QueryTaskTrace   QueryKind = "task_trace"
	QueryDiskUsage   QueryKind = "disk_usage"
	QueryConfig      QueryKind = "config"
)

// Query is a one-shot request answered by exactly one QueryResult carrying the
// same QueryID.
//
// Finished tasks are read this way rather than pushed: live events stream up,
// history is pulled on demand from the VPS event-store.
type Query struct {
	QueryID string          `json:"queryId"`
	What    QueryKind       `json:"what"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Stream names a continuous push the Runner can be asked to start or stop.
type Stream string

// StreamMetrics is the host and sandbox telemetry stream.
const StreamMetrics Stream = "metrics"

// Subscribe turns a stream on. Metrics are not sent to nobody.
type Subscribe struct {
	Stream Stream `json:"stream"`
}

// Unsubscribe turns a stream off.
type Unsubscribe struct {
	Stream Stream `json:"stream"`
}

// RunnerUpdate asks the Runner to replace its own binary. Version is the release
// to move to.
type RunnerUpdate struct {
	Version string `json:"version"`
}
