package protocol

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestEnvelopeRoundTrip(t *testing.T) {
	sent, err := New("msg-1", 1754400000000, TypeTaskResult, TaskResult{
		PRURL:      "https://github.com/acme/app/pull/7",
		CostUSD:    0.42,
		Tokens:     Tokens{In: 1200, Out: 350},
		DurationMs: 61000,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sent = sent.ForTask("T-42", 17)

	wire, err := json.Marshal(sent)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var got Envelope
	if err := json.Unmarshal(wire, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.V != Version || got.Type != TypeTaskResult {
		t.Errorf("envelope header = v%d %s, want v%d %s", got.V, got.Type, Version, TypeTaskResult)
	}
	if got.TaskID != "T-42" || got.Seq != 17 {
		t.Errorf("task binding = %s/%d, want T-42/17", got.TaskID, got.Seq)
	}

	var result TaskResult
	if err := got.Decode(&result); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if result.CostUSD != 0.42 || result.Tokens.Out != 350 {
		t.Errorf("payload = %+v, want cost 0.42 and 350 out tokens", result)
	}
}

// A newer sender may add fields a receiver has never heard of. Decoding must
// keep the fields it knows and drop the rest rather than failing.
func TestDecodeIgnoresUnknownFields(t *testing.T) {
	wire := []byte(`{"v":1,"id":"msg-2","ts":1754400000000,"type":"task.state",
		"data":{"state":"running","reason":"","escalationTier":3}}`)

	var e Envelope
	if err := json.Unmarshal(wire, &e); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	var state TaskState
	if err := e.Decode(&state); err != nil {
		t.Fatalf("Decode with unknown field: %v", err)
	}
	if state.State != TaskRunning {
		t.Errorf("state = %q, want %q", state.State, TaskRunning)
	}
}

func TestDecodeWithoutData(t *testing.T) {
	e := Envelope{V: Version, ID: "msg-3", Type: TypeStatus}
	var status Status
	if err := e.Decode(&status); err == nil {
		t.Error("Decode on an empty payload succeeded, want error")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		env     Envelope
		wantErr bool
	}{
		{"complete", Envelope{V: 1, ID: "a", Type: TypeStatus}, false},
		{"task bound", Envelope{V: 1, ID: "a", Type: TypeTaskEvent, TaskID: "T-1", Seq: 4}, false},
		{"no version", Envelope{ID: "a", Type: TypeStatus}, true},
		{"no id", Envelope{V: 1, Type: TypeStatus}, true},
		{"no type", Envelope{V: 1, ID: "a"}, true},
		{"seq without task", Envelope{V: 1, ID: "a", Type: TypeTaskEvent, Seq: 4}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.env.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// An unknown type is ignorable, not invalid — that is what forward compatibility
// rests on, so Validate must not reject it.
func TestValidateAcceptsUnknownType(t *testing.T) {
	e := Envelope{V: 1, ID: "a", Type: "task.teleport"}
	if err := e.Validate(); err != nil {
		t.Errorf("Validate() on unknown type = %v, want nil", err)
	}
}

func TestNegotiate(t *testing.T) {
	tests := []struct {
		name   string
		runner []int
		cloud  []int
		want   int
		wantOK bool
	}{
		{"single match", []int{1}, []int{1}, 1, true},
		{"picks highest common", []int{1, 2}, []int{1, 2, 3}, 2, true},
		{"old runner, new cloud", []int{1}, []int{1, 2}, 1, true},
		{"nothing in common", []int{1}, []int{2, 3}, 0, false},
		{"runner speaks nothing", nil, []int{1}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Negotiate(tt.runner, tt.cloud)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Negotiate(%v, %v) = %d, %v; want %d, %v",
					tt.runner, tt.cloud, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestSupportedVersionsIncludesCurrent(t *testing.T) {
	got, ok := Negotiate(SupportedVersions(), SupportedVersions())
	if !ok || got != Version {
		t.Errorf("Negotiate against self = %d, %v; want %d, true", got, ok, Version)
	}
}

// Credentials must survive no logging path intact. Both the fmt path and the
// slog path are checked because they resolve through different interfaces.
func TestCredSetNeverLogsValue(t *testing.T) {
	const secret = "ghp_realtokenvalue"
	cred := CredSet{Key: CredGit, Value: secret}

	if s := cred.String(); strings.Contains(s, secret) {
		t.Errorf("String() leaked the value: %s", s)
	}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("setting cred", "cred", cred)
	if out := buf.String(); strings.Contains(out, secret) {
		t.Errorf("slog leaked the value: %s", out)
	} else if !strings.Contains(out, string(CredGit)) {
		t.Errorf("slog dropped the key too: %s", out)
	}

	// Redaction is for logs only — the wire form has to carry the real value,
	// or Managed mode could not work.
	wire, err := json.Marshal(cred)
	if err != nil {
		t.Fatalf("marshal CredSet: %v", err)
	}
	if !bytes.Contains(wire, []byte(secret)) {
		t.Errorf("JSON dropped the value: %s", wire)
	}
}

// The mode is additive, so a Runner built before it existed still decodes and
// simply reports no mode. Cloud reads that as Local, which is the safe default:
// it offers no input rather than pushing a secret at a Runner that would refuse
// it.
func TestCredStatusModeIsOptional(t *testing.T) {
	var old CredStatus
	if err := json.Unmarshal([]byte(`{"git":true,"taskManager":false,"llm":true}`), &old); err != nil {
		t.Fatalf("unmarshal an older cred.status: %v", err)
	}
	if old.Mode != "" {
		t.Errorf("mode = %q, want empty", old.Mode)
	}

	wire, err := json.Marshal(CredStatus{Git: true, Mode: CredModeManaged})
	if err != nil {
		t.Fatalf("marshal CredStatus: %v", err)
	}
	if !bytes.Contains(wire, []byte(`"mode":"managed"`)) {
		t.Errorf("JSON dropped the mode: %s", wire)
	}
}

func TestHelloNeverLogsToken(t *testing.T) {
	const token = "rt_secrettoken"
	hello := Hello{
		Token:         token,
		RunnerVersion: "0.3.1",
		ProtoVersions: SupportedVersions(),
		Host:          HostInfo{OS: "linux", Arch: "amd64", Docker: "27.0"},
	}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("handshake", "hello", hello)
	if out := buf.String(); strings.Contains(out, token) {
		t.Errorf("slog leaked the token: %s", out)
	}
}

// The trace payloads are decoded off a raw TaskEvent, so check one end to end.
func TestTaskEventPayload(t *testing.T) {
	payload, err := json.Marshal(LLMCallPayload{
		Model:      "claude-opus-5",
		Tokens:     Tokens{In: 900, Out: 120},
		CostUSD:    0.11,
		DurationMs: 2400,
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	env, err := New("msg-4", 1754400000000, TypeTaskEvent, TaskEvent{
		Event:   EventLLMCall,
		Payload: payload,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	var event TaskEvent
	if err := env.Decode(&event); err != nil {
		t.Fatalf("Decode event: %v", err)
	}
	if event.Event != EventLLMCall {
		t.Fatalf("event = %q, want %q", event.Event, EventLLMCall)
	}

	var call LLMCallPayload
	if err := json.Unmarshal(event.Payload, &call); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if call.Model != "claude-opus-5" || call.Tokens.In != 900 {
		t.Errorf("payload = %+v, want model claude-opus-5 and 900 in tokens", call)
	}
}

// repo.status is a bare array on the wire, not an object wrapping one.
func TestRepoStatusIsArray(t *testing.T) {
	env, err := New("msg-5", 1754400000000, TypeRepoStatus, RepoStatus{
		{Repo: "acme/app", Branch: "main", LastFetch: 1754400000000, Dirty: false},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !bytes.HasPrefix(env.Data, []byte("[")) {
		t.Errorf("repo.status data = %s, want a JSON array", env.Data)
	}
}

func TestErrorImplementsError(t *testing.T) {
	var err error = Error{Code: ErrBudgetExceeded, Ref: "msg-1", Msg: "cap $2.00 reached"}
	if got, want := err.Error(), "BUDGET_EXCEEDED: cap $2.00 reached"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if got, want := (Error{Code: ErrInternal}).Error(), "INTERNAL"; got != want {
		t.Errorf("Error() without message = %q, want %q", got, want)
	}
}
