// Package protocol defines the wire contract between a Roost Runner, which runs
// on the developer's own VPS, and the Roost Cloud API behind the dashboard.
//
// The Runner always dials out over wss:// and Cloud never connects inward, so a
// VPS needs no open ports. One Runner holds one connection and multiplexes every
// stream — tasks, metrics, chat, commands — through it.
//
// Both sides must silently ignore unknown Type values and unknown fields inside
// Data. That is what lets a newer sender talk to an older receiver, which
// matters here because open-source Runners update on their owners' schedule.
//
// PROTOCOL.md in this repo is the normative spec; this package follows it.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// Version is the protocol major version this package implements. A breaking
// change bumps it. Adding a Type, or a field inside Data, does not.
const Version = 1

// redacted stands in for any credential in String and slog output.
const redacted = "[REDACTED]"

// Keepalive and reconnect parameters, per PROTOCOL.md §1 and §6.
const (
	// PingInterval is how often each side sends a WebSocket ping.
	PingInterval = 30 * time.Second
	// MissedPongsDead is how many unanswered pings mark the peer dead.
	MissedPongsDead = 2

	// ReconnectMinBackoff and ReconnectMaxBackoff bound the Runner's
	// exponential backoff. Add jitter on top.
	ReconnectMinBackoff = 1 * time.Second
	ReconnectMaxBackoff = 60 * time.Second

	// QueuedCommandTTL is how long Cloud may hold a command marked queued for
	// an offline Runner. Unmarked commands are refused, never queued.
	QueuedCommandTTL = 10 * time.Minute
)

// Type identifies a message as namespace.action. A receiver that does not know
// a Type drops the message without erroring.
type Type string

// Messages the Runner sends up to Cloud.
//
// Credential values, file contents from the repo, and secrets of any kind are
// forbidden upward. CmdOutput payloads pass the Runner's redaction filter first.
const (
	TypeHello       Type = "hello"
	TypeStatus      Type = "status"
	TypeMetrics     Type = "metrics"
	TypeTaskEvent   Type = "task.event"
	TypeTaskState   Type = "task.state"
	TypeTaskResult  Type = "task.result"
	TypeCredStatus  Type = "cred.status"
	TypeRepoStatus  Type = "repo.status"
	TypeQueryResult Type = "query.result"
)

// Messages Cloud sends down to the Runner.
const (
	TypeHelloOK      Type = "hello.ok"
	TypeHelloErr     Type = "hello.err"
	TypeTaskRun      Type = "task.run"
	TypeTaskCancel   Type = "task.cancel"
	TypeTaskApprove  Type = "task.approve"
	TypeRepoPrepare  Type = "repo.prepare"
	TypeCredSet      Type = "cred.set"
	TypeQuery        Type = "query"
	TypeSubscribe    Type = "subscribe"
	TypeUnsubscribe  Type = "unsubscribe"
	TypeRunnerUpdate Type = "runner.update"
)

// Messages either side may send.
const (
	TypeChat  Type = "chat"
	TypeError Type = "error"
)

// Envelope wraps every message in both directions. One Envelope is one
// WebSocket text frame of UTF-8 JSON.
type Envelope struct {
	V      int             `json:"v"`
	ID     string          `json:"id"`
	TS     int64           `json:"ts"` // unix milliseconds
	Type   Type            `json:"type"`
	TaskID string          `json:"taskId,omitempty"`
	Seq    uint64          `json:"seq,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// New builds an Envelope carrying data, marshalling the payload immediately so
// a bad payload fails at the call site rather than at write time.
//
// The caller supplies id and ts (unix milliseconds). That keeps this package
// dependency-free and makes envelopes reproducible in tests.
func New(id string, ts int64, t Type, data any) (Envelope, error) {
	e := Envelope{V: Version, ID: id, TS: ts, Type: t}
	if data == nil {
		return e, nil
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("protocol: marshal %s payload: %w", t, err)
	}
	e.Data = raw
	return e, nil
}

// ForTask returns a copy of e bound to a task. Seq is monotonic per task, which
// gives receivers a total order without trusting either clock, and lets a
// reconnecting Runner resume from the LastSeq that Cloud reports in HelloOK.
func (e Envelope) ForTask(taskID string, seq uint64) Envelope {
	e.TaskID = taskID
	e.Seq = seq
	return e
}

// Decode unmarshals the payload into v. Fields in the payload that v does not
// declare are ignored, by design.
func (e Envelope) Decode(v any) error {
	if len(e.Data) == 0 {
		return fmt.Errorf("protocol: %s carries no data", e.Type)
	}
	if err := json.Unmarshal(e.Data, v); err != nil {
		return fmt.Errorf("protocol: decode %s payload: %w", e.Type, err)
	}
	return nil
}

// Validate checks the invariants a receiver can enforce without knowing the
// Type.
//
// An unrecognized Type is deliberately not an error: callers skip those
// messages, they do not reject the sender.
func (e Envelope) Validate() error {
	switch {
	case e.V <= 0:
		return fmt.Errorf("protocol: envelope %q has no version", e.ID)
	case e.ID == "":
		return fmt.Errorf("protocol: envelope has no id")
	case e.Type == "":
		return fmt.Errorf("protocol: envelope %q has no type", e.ID)
	case e.Seq != 0 && e.TaskID == "":
		return fmt.Errorf("protocol: envelope %q has seq %d but no taskId", e.ID, e.Seq)
	}
	return nil
}

// SupportedVersions reports the protocol versions this package speaks, for the
// ProtoVersions field of Hello.
func SupportedVersions() []int {
	return []int{Version}
}

// Negotiate picks the highest version both sides speak, reporting false when
// they have none in common.
//
// Cloud is obliged to keep accepting Version-1 alongside Version, because
// self-hosted Runners upgrade slowly.
func Negotiate(runner, cloud []int) (int, bool) {
	best, ok := 0, false
	for _, r := range runner {
		for _, c := range cloud {
			if r == c && r > best {
				best, ok = r, true
			}
		}
	}
	return best, ok
}
