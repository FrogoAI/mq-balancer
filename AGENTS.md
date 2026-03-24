# AGENTS.md — mq-balancer

AI/LLM agent guide for contributing to this project.

## What This Library Is

A Go vendor library (no `main()`) that provides a message queue subscriber with automatic worker pool scaling. Consumers import it via `go get github.com/FrogoAI/mq-balancer`. The only supported driver is NATS.

## Architecture

```
Consumer App
  |
  v
subscriber.NewSubscriber(driver.NewNATSSubscriber(conn))
  |
  v
[subscriber.go]         Orchestrator: manages subscriptions via SafeMap
  |
  v
[subscription.go]       Per-subject worker: reader goroutine → channel → worker pool
  |           |
  v           v
[meter.go]  [response.go]   Metrics (OTel) / error-response middleware
  |
  v
[mq/]                   Interface definitions (Client, Msg, Subscription, Config, Logger, Meter)
  |
  v
[driver/]               NATS implementation of mq interfaces
  |
  v
[driver/client/]        NATS connection, config (env-parsed), options
```

### Dependency Rules

```
subscriber  →  mq (interfaces only)
driver      →  subscriber (for ErrConnectionClosed), mq, driver/client
driver/client → nats.go, nkeys, env
mq          →  (no internal deps, only otel/metric)
```

**Forbidden**: `subscriber` must never import `driver` or `driver/client`.

### Key Design Decisions

- **Interfaces in `mq/` package**: defines `Client`, `Msg`, `Subscription`, `Config`, `Logger`, `Meter`. The driver implements them; the subscriber consumes them.
- **Worker pool auto-scaling**: persistent workers + temporal workers that spin up under load and idle-timeout after 30s.
- **Error wrapping at the driver boundary**: NATS-specific errors are wrapped with `subscriber.ErrConnectionClosed` in `NATSSubscription.NextMsg()`, so the subscriber package never imports NATS directly.
- **Deep copy on `Msg.Copy()`**: header map and data slice are fully copied to prevent mutation of the original.

## Package Responsibilities

| File | Responsibility |
|------|----------------|
| `subscriber/subscriber.go` | `Subscriber` type: subscribe, close, get, wait |
| `subscriber/subscription.go` | `Subscription` type: reader goroutine, auto-scaler, worker pool |
| `subscriber/meter.go` | OTel observable gauges for pending/dropped/delivered counts |
| `subscriber/response.go` | `WithResponseOnError` middleware: sends error headers on reply |
| `subscriber/errors.go` | Sentinel errors (`ErrConnectionClosed`) |
| `subscriber/mq/*.go` | All interface definitions |
| `subscriber/driver/nats-subscriber.go` | NATS adapter types: `NATSMsg`, `NATSSubscription`, `NATSSubscriber`, `NATSConfig` |
| `subscriber/driver/client/client.go` | NATS connection wrapper with meter mutex |
| `subscriber/driver/client/config.go` | Config struct with env parsing, NATS options builder |
| `subscriber/driver/client/logger.go` | Logger interface, StubLogger, NATS error handler |

## Commands

```bash
# Run unit tests
go test -race -count=1 ./...

# Run integration tests (uses embedded NATS, no external server needed)
go test -tags=integration -race -count=1 -v ./...

# Coverage
go test -coverprofile=coverage.out -cover -race ./...
go-test-coverage --config=./.testcoverage.yml

# Regenerate mocks
go generate ./subscriber/mq/...

# Lint
go vet ./...
```

## Code Conventions

### Go Standards
- **Formatting**: `gofmt` only. Tabs, no line limit.
- **Naming**: `MixedCaps`, no `snake_case`. Acronyms all-caps (`HTTPClient`, `userID`). No `Get` prefix on getters. No stuttering (`mq.MQClient` → `mq.Client`).
- **Errors**: lowercase, no punctuation. Wrap with `fmt.Errorf("context: %w", err)`. Sentinel errors in `errors.go`. Check with `errors.Is`/`errors.As`.
- **Interfaces**: small (1-3 methods ideal). Define at the consumer. Accept interfaces, return structs.
- **Imports**: stdlib → third-party → org packages (blank line separated).

### Project-Specific Rules
- **No `fmt.Print*` / `log.Print*`** in production code. Use `slog` or the `Logger` interface.
- **Table-driven tests** with `t.Run` for all test cases.
- **Integration tests** use `//go:build integration` tag and embedded NATS server.
- **Mocks** are generated with `mockgen` via `//go:generate` directives in `mq/` package files.
- **Config** supports two paths: env vars (production) and struct literals (tests).
- **Never import `driver/` from `subscriber/`** — this maintains the abstraction boundary.

### Error Handling
- Return `error` as last value.
- Check immediately after call.
- Wrap with context at each layer: `fmt.Errorf("save user %d: %w", id, err)`.
- Driver wraps transport errors with subscriber sentinel errors for abstraction.

### Concurrency
- Every goroutine must have a cancellation path via `context.Context`.
- Use channels for communication, not shared memory.
- Protect shared state with `sync.RWMutex` or `sync/atomic`.
- Always check both `ctx.Done()` and `s.ctx.Done()` in select loops.

## Testing

| Type | Tag | Backend | What it tests |
|------|-----|---------|---------------|
| Unit | None | Mocks from `mq/mock/` | Core logic, edge cases, error paths |
| Integration | `//go:build integration` | Embedded NATS server | Full subscribe/publish flow, driver adapters |

### Coverage Target
- **Total**: 90%+
- **Excluded**: generated mocks (`/mock/`), protobuf, examples
