# roostlabs/protocol

The wire contract between a Roost **Runner** — the process on the developer's own
VPS — and the Roost **Cloud** API behind the dashboard.

[`PROTOCOL.md`](PROTOCOL.md) is the normative spec. This Go package implements
it: the envelope, every message type, and the payload structs. Both sides depend
on this package so neither can drift from the spec silently.

```go
import "github.com/roostlabs/protocol"

env, err := protocol.New(uuid.NewString(), time.Now().UnixMilli(),
    protocol.TypeTaskEvent, protocol.TaskEvent{
        Event:   protocol.EventCmdExit,
        Payload: payload,
    })
env = env.ForTask("T-42", seq)
```

Receiving:

```go
var env protocol.Envelope
if err := json.Unmarshal(frame, &env); err != nil { /* ... */ }
if err := env.Validate(); err != nil { /* ... */ }

switch env.Type {
case protocol.TypeTaskRun:
    var run protocol.TaskRun
    if err := env.Decode(&run); err != nil { /* ... */ }
    // ...
default:
    // Unknown types are dropped, never rejected.
}
```

## What the shape is protecting

Three properties drive most of the design, and a change that breaks one of them
is a change to the product:

**The Runner dials out.** Cloud never connects inward, so the VPS needs no open
ports. Everything Cloud wants — metrics, history, control — travels through the
connection the Runner opened.

**Secrets do not travel up.** `CredStatus` carries booleans, never values.
`CmdOutputPayload` passes the Runner's redaction filter before it is sent.
`CredSet` moves a value *down* in Managed mode only, relay-only, and its `String`
and `LogValue` methods redact it so no log can capture it by accident.

**The VPS owns the history.** Every `TaskEvent` lands in the Runner's local
SQLite event-store before it is streamed. A dropped connection therefore costs no
data: Cloud reports `LastSeq` per active task in `HelloOK`, and the Runner
replays from there. Tasks keep running while the channel is down — it is
observation and control, not life support.

## Compatibility

Unknown `Type` values and unknown fields inside `Data` are ignored, so a newer
sender can talk to an older receiver. Adding a type or a field is therefore not a
breaking change; only bumping `Version` is. Cloud is obliged to keep accepting
`Version - 1`, because self-hosted Runners upgrade on their owners' schedule.

Consumed as a versioned Go module — depend on a tag, not on `main`.

## Status

Draft v0.1. Nothing consumes it yet; `runner` is next. Open questions are listed
at the end of `PROTOCOL.md`.
