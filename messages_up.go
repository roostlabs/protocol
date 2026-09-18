package protocol

import (
	"encoding/json"
	"log/slog"
)

// Hello is the first frame on every connection: the Runner authenticates with
// its dashboard-issued token and declares which protocol versions it speaks.
// Cloud answers HelloOK, or HelloErr and then closes.
type Hello struct {
	Token         string   `json:"token"`
	RunnerVersion string   `json:"runnerVersion"`
	ProtoVersions []int    `json:"protoVersions"`
	Host          HostInfo `json:"host"`
}

// LogValue redacts Token, so logging a Hello cannot leak the Runner's identity.
func (h Hello) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("token", redacted),
		slog.String("runnerVersion", h.RunnerVersion),
		slog.Any("protoVersions", h.ProtoVersions),
		slog.Any("host", h.Host),
	)
}

// HostInfo describes the VPS well enough for Cloud to warn about unmet
// prerequisites. Docker is empty when the Runner could not detect a daemon.
type HostInfo struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	Docker string `json:"docker"`
}

// RunnerState is the Runner's availability. Concurrency is 1 by default, so busy
// means one sandbox is occupied and further work queues.
type RunnerState string

const (
	RunnerIdle RunnerState = "idle"
	RunnerBusy RunnerState = "busy"
)

// Status reports Runner availability, sent periodically and on every change.
type Status struct {
	State       RunnerState `json:"state"`
	ActiveTasks []string    `json:"activeTasks"`
	QueuedTasks int         `json:"queuedTasks"`
}

// Metrics is host and sandbox telemetry, sent roughly every 5s but only while
// Cloud holds a Subscribe on the metrics stream — an unwatched dashboard costs
// the VPS nothing.
type Metrics struct {
	Host      HostMetrics      `json:"host"`
	Sandboxes []SandboxMetrics `json:"sandboxes"`
}

// HostMetrics carries whole-VPS usage. CPU, Mem and Disk are percentages.
type HostMetrics struct {
	CPU  float64 `json:"cpu"`
	Mem  float64 `json:"mem"`
	Disk float64 `json:"disk"`
	Load float64 `json:"load"` // one-minute load average
}

// SandboxMetrics carries usage for one running container.
type SandboxMetrics struct {
	TaskID string  `json:"taskId"`
	CPUPct float64 `json:"cpuPct"`
	MemMB  float64 `json:"memMb"`
}

// EventKind names an entry in a task's trace.
type EventKind string

const (
	EventStage     EventKind = "stage"
	EventAgentStep EventKind = "agent_step"
	EventCmdStart  EventKind = "cmd_start"
	EventCmdOutput EventKind = "cmd_output"
	EventCmdExit   EventKind = "cmd_exit"
	EventLLMCall   EventKind = "llm_call"
	EventPR        EventKind = "pr"
	EventError     EventKind = "error"
	EventTicket    EventKind = "ticket"
)

// TaskEvent is one entry of the append-only task trace.
//
// The Runner writes each event to its local SQLite event-store first and streams
// it up second. That order is what makes the VPS the source of truth and lets
// events replay after a dropped connection instead of being lost.
//
// Payload shape follows Event, so it stays raw here; decode it into the matching
// payload type below.
type TaskEvent struct {
	Event   EventKind       `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// TicketPayload records which work item the task is for, as the Runner knows it
// once the ticket has been read. It is the first event of a task that has a
// ticket, and it is how Cloud learns about a task the Runner started on its own
// — one picked up by polling the tracker — which no task.run ever described.
//
// No Body: the description can be long and is the agent's input, not the
// dashboard's. The url is what a reader clicks to see it.
type TicketPayload struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
	URL      string `json:"url,omitempty"`
	Title    string `json:"title,omitempty"`
}

// StagePayload marks entry into a coarse phase of the task, such as preparing
// the worktree or opening the PR.
type StagePayload struct {
	Name string `json:"name"`
}

// AgentStepPayload records one reasoning step the agent took. StepID is what
// TaskApprove refers back to when a step needs a human.
type AgentStepPayload struct {
	StepID string `json:"stepId"`
	Text   string `json:"text"`
}

// CmdStartPayload records a command the agent launched inside the sandbox. Argv
// is unjoined so a reader never has to guess at quoting.
type CmdStartPayload struct {
	CmdID string   `json:"cmdId"`
	Argv  []string `json:"argv"`
	Dir   string   `json:"dir,omitempty"`
}

// CmdOutputPayload is one chunk of command output.
//
// Chunk must pass the Runner's redaction filter before it is sent: every
// credential value injected into the sandbox env is masked first. Nothing else
// on this channel is as likely to carry a secret by accident.
type CmdOutputPayload struct {
	CmdID  string `json:"cmdId"`
	Stream string `json:"stream"` // stdout or stderr
	Chunk  string `json:"chunk"`
}

// CmdExitPayload closes out a command.
type CmdExitPayload struct {
	CmdID      string `json:"cmdId"`
	Code       int    `json:"code"`
	DurationMs int64  `json:"durationMs"`
}

// LLMCallPayload is the unit cost accounting is built from: budgets and the
// per-task dollar figure are both derived by summing these.
type LLMCallPayload struct {
	Model      string  `json:"model"`
	Tokens     Tokens  `json:"tokens"`
	CostUSD    float64 `json:"costUsd"`
	DurationMs int64   `json:"durationMs"`
}

// PRPayload records the pull request the agent opened. It is opened by a service
// account with no merge rights, so a human always reviews.
type PRPayload struct {
	URL    string `json:"url"`
	Branch string `json:"branch"`
}

// EventErrorPayload records a failure inside the trace. A failed task also sends
// TaskState with TaskFailed.
type EventErrorPayload struct {
	Msg string `json:"msg"`
}

// Tokens counts LLM tokens in one call or over one task.
type Tokens struct {
	In  int64 `json:"in"`
	Out int64 `json:"out"`
}

// TaskStatus is a task's lifecycle state.
type TaskStatus string

const (
	TaskQueued           TaskStatus = "queued"
	TaskPreparing        TaskStatus = "preparing"
	TaskRunning          TaskStatus = "running"
	TaskAwaitingApproval TaskStatus = "awaiting_approval"
	TaskDone             TaskStatus = "done"
	TaskFailed           TaskStatus = "failed"
	TaskCancelled        TaskStatus = "cancelled"
)

// TaskState announces a lifecycle transition. Reason is set for the states where
// "why" is not obvious, chiefly TaskFailed and TaskCancelled.
type TaskState struct {
	State  TaskStatus `json:"state"`
	Reason string     `json:"reason,omitempty"`
}

// TaskResult closes a task out. PRURL is empty when the task failed before
// opening one.
type TaskResult struct {
	PRURL      string  `json:"prUrl,omitempty"`
	CostUSD    float64 `json:"costUsd"`
	Tokens     Tokens  `json:"tokens"`
	DurationMs int64   `json:"durationMs"`
}

// CredMode says where credential values come from.
//
// Local is the default and the recommendation: the developer sets values on the
// VPS and Cloud only ever learns whether a slot is filled. Managed is opt-in and
// means the developer accepts typing values into the dashboard, from where they
// are relayed down the channel — a transit Local never performs at all.
type CredMode string

const (
	CredModeLocal   CredMode = "local"
	CredModeManaged CredMode = "managed"
)

// CredStatus reports which credentials are configured — flags only. Values live
// on the VPS and never travel up this channel, which is the whole point of the
// design.
//
// Mode is here because the dashboard cannot infer it: whether it may offer an
// input for a credential is the Runner's decision, not Cloud's.
type CredStatus struct {
	Git         bool     `json:"git"`
	TaskManager bool     `json:"taskManager"`
	LLM         bool     `json:"llm"`
	Mode        CredMode `json:"mode,omitempty"`
}

// RepoStatus lists the persistent working copies on the VPS. Repos are cloned
// once and kept; each task gets a clean baseline from a git worktree rather than
// a fresh clone.
type RepoStatus []RepoInfo

// RepoInfo describes one prepared repo. Dirty means uncommitted changes are
// present, which a task's baseline reset would discard.
type RepoInfo struct {
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	LastFetch int64  `json:"lastFetch"` // unix milliseconds
	Dirty     bool   `json:"dirty"`
}

// Chat carries a message between the agent and the developer. It is the one type
// both sides send.
type Chat struct {
	TaskID string `json:"taskId"`
	Text   string `json:"text"`
}

// TaskHistoryEntry is one task as the Runner's journal knows it: the answer to
// a task_history query is a list of these, most recently active first.
//
// This is how Cloud learns about tasks it never saw finish — or never saw at
// all, when the Runner picked a ticket up while Cloud was unreachable. Replay
// after a reconnect covers only tasks Cloud already holds; history covers the
// rest, on demand.
type TaskHistoryEntry struct {
	TaskID string     `json:"taskId"`
	State  TaskStatus `json:"state,omitempty"`
	Reason string     `json:"reason,omitempty"`
	// Result is set once the task has finished.
	Result *TaskResult `json:"result,omitempty"`
	// Ticket is what the task was for, when its trace recorded one.
	Ticket *TicketPayload `json:"ticket,omitempty"`
	// LastSeq is the highest event seq in the journal. Cloud compares it with
	// what it holds to know whether there is anything left to pull.
	LastSeq   uint64 `json:"lastSeq"`
	StartedAt int64  `json:"startedAt"` // unix milliseconds, first event
	UpdatedAt int64  `json:"updatedAt"` // unix milliseconds, latest change
}

// TaskHistoryParams is the optional Params of a task_history query. Limit
// bounds the list; zero leaves it to the Runner.
type TaskHistoryParams struct {
	Limit int `json:"limit,omitempty"`
}

// TaskTraceParams is the Params of a task_trace query: one task's events after
// AfterSeq, at most Limit of them (zero means all). Paging is by seq, which is
// what the journal is ordered by.
type TaskTraceParams struct {
	TaskID   string `json:"taskId"`
	AfterSeq uint64 `json:"afterSeq,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// TraceEvent is one journalled event as a task_trace query returns it: a
// TaskEvent with the seq and time the journal gave it.
type TraceEvent struct {
	Seq     uint64          `json:"seq"`
	Event   EventKind       `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
	TS      int64           `json:"ts"` // unix milliseconds
}

// QueryResult answers a Query. Data holds the payload shaped by the Query's
// What; Error explains an unsuccessful one.
type QueryResult struct {
	QueryID string          `json:"queryId"`
	OK      bool            `json:"ok"`
	Data    json.RawMessage `json:"data,omitempty"`
	Error   string          `json:"error,omitempty"`
}
