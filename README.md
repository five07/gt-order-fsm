# Order State Machine

A small Go service that models a checkout lifecycle of an Order. using a simple finite state machine (FSM). The example demonstrates stage-dependent failure recovery, enforces valid transitions, records timestamped history, and surfaces partial failures instead of hiding them.


---

## What I built and why

Based on the requirements below, I started with a simple plan. I wanted to treat the state machine and all side effects as separate concerns. The machine is a pure transition table that only answers "is this move legal?", which makes it easy to test. The service calls payment and fulfillment “services”, maps each outcome to an event to keep all the state-dependent recovery information in one readable place. 

Two rules guided the rest: an **order's state changes only after** the real outcome is known, so it never shows cancelled unless the void actually succeeded, and **partial failure is a real state** (needs_attention) that records both errors instead of disappearing into a log line. I

 Where I simplified for a prototype (in-memory store, one void attempt, no intermediate voiding state), I kept those pieces behind interfaces and wrote down additional improvements toward a production path, such as a durable outbox and worker pattern. See [With more time]( #with-more-time)

### Requirements

1. Model the state machine. Define states, enforce valid transitions, and record state history with timestamps.
2. Handle failure differently depending on stage. A payment decline just rejects. A completion failure after authorization triggers a void. Same concept (something failed), different recovery.
3. Handle partial failures. If the void fails, surface it—don't pretend the order is cleanly cancelled.
4. Expose a small API for creating an order, advancing its state, and querying its current state history.

### State Mapping

| Stage               | Failure                             | Money state              | Recovery                               |
|---------------------|-------------------------------------|--------------------------|----------------------------------------|
| Authorizing payment | Declined                            | Nothing held             | Reject. No cleanup.                    |
| Completing order    | Fulfillment failed                  | Funds held               | Void payment, then cancel.             |
| Completing order    | Fulfillment and payment void failed | Funds held, unknown fate | `needs_attention`. Record both errors. |

The design:

- **The state machine is pure.** It only answers "is this transition legal?" It has no I/O, no goroutines, and no hooks/callbacks. That makes it trivial to test exhaustively.
- **The service owns side effects.** `service.Advance` looks at the current state, calls the right service (payment or fulfillment), and maps the outcome to an event. All state-dependent recovery lives in one readable place.
- **State changes only after the outcome is known.** An order never shows `cancelled` unless the void actually succeeded.
- **Partial failure is a first-class state, not a log line.** `needs_attention` carries the completion error and the void error in its history entry, and it's logged at `ERROR`.

I deliberately did not use a generic or configurable FSM engine. There is one machine, owned by the business, and it doesn't change at runtime. 

---

## State model

### States

| State | Terminal | Meaning |
|---|---|---|
| `initialized` | no | Order created, no payment attempted |
| `payment_authorized` | no | Funds held, fulfillment not yet attempted |
| `complete` | yes | Tickets delivered, payment authorized |
| `rejected` | yes | Payment declined, nothing held |
| `cancelled` | yes | Fulfillment failed, authorization voided |
| `needs_attention` | yes (for automation) | Fulfillment failed and void failed. Requires manual resolution |

### Events

Events are verbs naming what the order does. The service decides which one applies from the collaborator's outcome; the cause is recorded in the history entry's error.

| Event | Fired when |
|---|---|
| `authorize` | Payment authorization succeeded |
| `reject` | Payment was declined |
| `fulfill` | Fulfillment succeeded |
| `cancel` | Fulfillment failed and the void succeeded |
| `escalate` | Fulfillment failed and the void failed |

### Transitions

| From | Event | To |
|---|---|---|
| `initialized` | `authorize` | `payment_authorized` |
| `initialized` | `reject` | `rejected` |
| `payment_authorized` | `fulfill` | `complete` |
| `payment_authorized` | `cancel` | `cancelled` |
| `payment_authorized` | `escalate` | `needs_attention` |

Any other `(state, event)` pair returns `ErrInvalidTransition` and leaves the order unchanged. `needs_attention` is terminal for automation but still awaits a human.

### History

Each order keeps an append-only history. Every entry records:

- `from` and `to` states
- `event`
- `at`: the UTC timestamp, from an injected, fake clock so tests are deterministic
- `note`: what the service did in that step, in plain words. For example, `fulfillment failed; authorization 01a1… voided`. It makes a successful void visible, since the void happens in the same step as the failed fulfillment
- `errors`: the cause when the transition was driven by a failure. For `needs_attention` it holds both the completion and the void error (stored as one `errors.Join` error, so `errors.Is` matches either)

Creation is recorded as the first entry (`"" → initialized`).

---

## Project Structure

```
cmd/service/           main: wiring, simple HTTP server
internal/order/
  interfaces.go        interfaces for services
  machine.go           FSM config (transition table), Next, IsTerminal
  order.go             Order, HistoryEntry, Clone, apply
  service.go           Create, Advance, Get: side effects and recovery
  types.go             states, events and errors
internal/payment/      Money, Authorization, payment Stub (for testing)
internal/fulfillment/  fulfillment Stub (for testing)
internal/store/        in-memory Store as a database stand-in
internal/api/          handlers, DTOs, error to status mapping
internal/util/         UUIDv7 IDs
```

### Concurrency

Each order has its own mutex. `Advance` uses `TryLock`. A concurrent advance on the same order gets `409 Conflict` instead of queueing. This prevents a double authorize or a double void from two racing requests. The lock is held across the external call, which is acceptable for an in-memory prototype. See "With more time" for the production version.

---
## API

| Method | Path | Description |
|---|---|---|
| `POST` | `/orders` | Create an order in `initialized` |
| `POST` | `/orders/{id}/advance` | Run the next step for the order's current stage |
| `GET` | `/orders/{id}` | Current state and full history |

### Status mapping

| Condition | Status |
|---|---|
| Created | `201` |
| Advanced (including into `rejected`, `cancelled`, `needs_attention`) | `200` |
| Order not found | `404` |
| Terminal state, invalid transition, or advance already in progress | `409` |
| Transient gateway error, no state change | `502` |
| Malformed request (bad JSON, unknown field, amount ≤ 0, non-ISO currency, missing card) | `400` |
| Anything else | `500` |

Failure outcomes return `200` because the request succeeded at advancing the order. The resulting state tells the client what happened. `502` and `500` bodies are generic; details go to the log.

### Request and response

```json
POST /orders
{ "amount_cents": 12500, "currency": "USD", "card_number": "4242" }
```

```json
{
  "id": "01a18272-b0ea-7c2d-8284-f16b1ebc368d",
  "state": "initialized",
  "amount_cents": 12345,
  "currency": "USD",
  "card_last4": "4242",
  "created_at": "2026-10-28T12:18:36.906799Z",
  "updated_at": "2026-10-28T12:18:36.906799Z",
  "history": [
    {
      "to": "initialized",
      "at": "2026-10-28T12:18:36.906799Z"
    }
  ]
}
```

`card_number` is required . Responses only ever include `card_last4`; the full number is never returned.

Errors are `{ "error": "..." }`. A history entry driven by a failure has an `errors` array, one string per cause.

### Simulating failures with test cards

Like Stripe's test cards, specific `card_number` values make the stubs fail, so every outcome can be reached with curl:

| `card_number` | Effect | Final state |
|---|---|---|
| `9999` | Payment declined | `rejected` |
| `9998` | Fulfillment fails, void succeeds | `cancelled` |
| `9997` | Fulfillment fails, void fails | `needs_attention` |
| anything else | Succeeds | `complete` |

The values are constants in `internal/payment/testcards.go`. Only the stubs read them; a real gateway adapter would never see them. The payment stub remembers which card each authorization came from, so `Void` can fail for `9997`.
### 
---

## How to run

```
go run ./cmd/service   # listens on :8080; set ADDR to change
go test -race ./...

# coverage across packages
go test -coverpkg=./internal/... -coverprofile=cover.out ./...
go tool cover -func=cover.out
```

Requires Go 1.27

### Try it with curl

**NOTE**: *These examples were written by ClaudeCode in the interest of time.*

With the service running (`go run ./cmd/service`), these commands walk through each outcome. They use `jq` to pull out the order ID and trim the output.

**Happy path:** create, authorize, fulfill, then inspect the history.

```sh
BASE=http://localhost:8080

ID=$(curl -s -X POST $BASE/orders \
  -H 'Content-Type: application/json' \
  -d '{"amount_cents": 12500, "currency": "USD", "card_number": "4242"}' | jq -r .id)

curl -s -X POST $BASE/orders/$ID/advance | jq .state   # "payment_authorized"
curl -s -X POST $BASE/orders/$ID/advance | jq .state   # "complete"
curl -s $BASE/orders/$ID | jq .history
```

**Advancing a terminal order** returns `409`:

```sh
curl -s -w '\n%{http_code}\n' -X POST $BASE/orders/$ID/advance
# {"error":"order is in a terminal state"}
# 409
```

**Payment declined** (`9999`): one advance ends in `rejected`.

```sh
ID=$(curl -s -X POST $BASE/orders \
  -d '{"amount_cents": 5000, "currency": "EUR", "card_number": "9999"}' | jq -r .id)

curl -s -X POST $BASE/orders/$ID/advance | jq '{state, last: .history[-1]}'
# {
#   "state": "rejected",
#   "last": {
#     "from": "initialized", to": "rejected", "event": "reject",
#     "note": "payment declined; nothing held",
#     "errors": ["payment declined: simulated by test card 9999"]
#   }
# }
```

**Fulfillment fails, void succeeds** (`9998`): the second advance ends in `cancelled`.

```sh
ID=$(curl -s -X POST $BASE/orders \
  -d '{"amount_cents": 5000, "currency": "EUR", "card_number": "9998"}' | jq -r .id)

curl -s -X POST $BASE/orders/$ID/advance > /dev/null
curl -s -X POST $BASE/orders/$ID/advance | jq '{state, last: .history[-1]}'
# {
#   "state": "cancelled",
#   "last": {
#     "from": "payment_authorized", "to": "cancelled", "event": "cancel",
#     "note": "fulfillment failed; authorization 01a1… voided",
#     "errors": ["fulfillment failed: simulated by test card 9998"], ...
#   }
# }
```

**Fulfillment and void both fail** (`9997`): the second advance ends in `needs_attention` with both errors.

```sh
ID=$(curl -s -X POST $BASE/orders \
  -d '{"amount_cents": 5000, "currency": "EUR", "card_number": "9997"}' | jq -r .id)

curl -s -X POST $BASE/orders/$ID/advance > /dev/null
curl -s -X POST $BASE/orders/$ID/advance | jq '.history[-1].errors'
# [
#   "fulfillment failed: simulated by test card 9997",
#   "void failed: simulated by test card 9997"
# ]
```

**Errors:** a malformed body returns `400`, and an unknown ID returns `404`.

```sh
curl -s -w '\n%{http_code}\n' -X POST $BASE/orders \
  -d '{"amount_cents": 0, "currency": "USD", "card_number": "4242"}'
# {"error":"amount_cents must be greater than zero"}
# 400

curl -s -w '\n%{http_code}\n' $BASE/orders/does-not-exist
# {"error":"order not found: does-not-exist"}
# 404
```

---

## Testing

**NOTE**: *Testing, stubs and this README section was created using ClaudeCode so that I could focus on the core functionality.*

Coverage is 99.5% of statements across `internal/` (measured with `-coverpkg`). The one uncovered branch is the defensive `default` in `Advance`, reachable only if a non-terminal state is added without a step.

**Required scenarios**, each covered at the service level and through the HTTP API with `httptest`:

1. Happy path: `initialized → payment_authorized → complete`
2. Payment decline: `initialized → rejected`, no fulfillment or void attempted
3. Completion failure, void succeeds: `→ cancelled`, history carries the completion error
4. Completion failure, void fails: `→ needs_attention`, history carries both errors

**Additional coverage:**

- A table-driven test of every `(state, event)` pair (6 × 5) against an independently written expectation, plus `IsTerminal` for every state
- Advancing a terminal state returns `ErrTerminalState`, and history is unchanged
- A transient authorize error leaves the order in `initialized` and is retryable
- Concurrent `Advance` on one order: the second gets `ErrAdvanceInProgress` while the first is mid-call, and `Authorize` runs once (run under `-race`)
- History ordering and timestamps via a fake clock
- The stubs record calls, for example that a decline never calls `Void` and that the void receives the order's own authorization
- A failed `Save` on create or advance is returned and leaves the stored order unchanged
- A corrupt record (authorized with no authorization) is refused before fulfillment
- `apply` with an illegal event leaves the order untouched; `Clone` and the store share no memory with callers
- Test cards drive each outcome over HTTP with the default stubs, and responses mask the card to its last four digits
- API: `400` for seven malformed bodies, `404`, `409`, `502` without leaking the upstream error, and a generic `500`
- `Money.String` for USD, JPY (zero-decimal) and EUR

---

## Tradeoffs

- **In-memory store.** It sits behind an interface, so swapping in Postgres doesn't touch the service.
- **A single `advance` endpoint** instead of client-sent events. The client can't choose an illegal path, and the server owns sequencing. The cost is less explicit control for callers.
- **No intermediate `voiding` state.** It's fine in memory, but a crash between a completion failure and the void outcome would lose that fact in a durable system. See the outbox/worker note -> [With more time]( #with-more-time)
- **Side effect before save.** If `Save` fails after a successful gateway call (for example, authorized but still stored as `initialized`), the external effect has already happened. The outbox below fixes this.
- **Decline vs. transient error.** A decline is final. A timeout on authorize is ambiguous: the charge may or may not exist. The prototype leaves the order in `initialized` and relies on the order ID as the gateway idempotency key so a retry is safe.
- **One void attempt.** The prompt routes a failed void straight to `needs_attention`. In production, I'd retry with backoff first.

---

## With more time

- **Durable store with an outbox.** Persist the state change and the intent ("void this auth") in one transaction. A worker executes it, so a crash mid-recovery resumes instead of losing track.
- **Event-Driven Design** with loose coupling to downstream services such as fulfillment.
- **Intermediate states** (`authorizing`, `completing`, `voiding`) for crash recovery and accurate visibility while an external call is in flight.
- **Void retry with backoff** before escalating to `needs_attention`, plus a reconciliation job that checks the gateway for orphaned authorizations. Requires an outbox and worker.
- **A resolution endpoint** for fetching all`needs_attention` (`resolve` as voided or refunded) with an operator ID and reason in the audit history.
- **Idempotency keys** on `POST /orders` and `advance`.
- **Alerting:** a metric and page on any entry into `needs_attention`, and on its age.
- **Authorize vs. capture:** business rules may specify authorize first and capture after delivery.
- **AuthZ,** and rate limiting.
