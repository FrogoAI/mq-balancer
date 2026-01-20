# mq-balancer

> A protocol-agnostic, high-concurrency message queue load balancer for Go.

`mq-balancer` is a library designed to decouple high-performance message processing from the underlying message broker. It provides a robust, auto-scaling worker pool that consumes messages from any source (NATS, Kafka, etc.) and processes them concurrently.

It is built with **dynamic scaling** in mind: workers are spawned on-demand to handle traffic bursts and gracefully spin down when idle, ensuring optimal resource usage.

## Features

* **Protocol Agnostic:** Defined strictly by interfaces (`Client`, `Subscription`). Switch drivers (e.g., from NATS to Kafka) without changing your business logic.
* **Dynamic Burst Scaling:** Automatically monitors queue depth and spawns "Temporal Workers" to handle pressure, scaling up to a configurable maximum and down to a minimum buffer.
* **Resilient Worker Pools:** Isolates panic/crash failures and handles graceful shutdowns via context propagation.
* **Middleware Support:** Includes built-in middleware like `WithResponseOnError` for RPC-style error reporting.
* **Observability:** Built-in OpenTelemetry metrics for queue depth (`pending`), throughput (`delivered`), and dropped messages.

## Installation

```bash
go get github.com/frogoai/mq-balancer

```

## Quick Start

### 1. Basic NATS Subscription

This example shows how to use the included NATS driver to process messages concurrently.

```go
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/frogoai/mq-balancer/subscriber"
	"github.com/frogoai/mq-balancer/subscriber/driver"
	"github.com/frogoai/mq-balancer/subscriber/interfaces"
)

func main() {
	// 1. Setup NATS connection
	nc, _ := nats.Connect(nats.DefaultURL)
	
	// 2. Initialize the NATS Driver
	// This wrapper satisfies the mq-balancer Client interface
	natsClient := driver.NewNATSSubscriber(driver.NewClient(nc))

	// 3. Create the Subscriber
	sub := subscriber.NewSubscriber(natsClient)

	// 4. Subscribe to a subject
	// "orders" = Subject, "workers" = Queue Group (Load Balancing)
	sub.Subscribe("orders.created", "workers", func(ctx context.Context, msg interfaces.Msg) error {
		slog.Info("Processing order", "data", string(msg.GetData()))
		
		// Simulate work
		time.Sleep(100 * time.Millisecond)
		return nil
	})

	// 5. Wait for graceful shutdown (blocks until context is cancelled)
	sub.Wait()
}

```

## Architecture & Configuration

### Dynamic Scaling

The balancer uses a `Ticker` loop to monitor the local channel buffer.

* **Min Workers (Buffer):** Always active. Defined by `GetConcurrentSize()`.
* **Max Workers:** The hard limit for scaling. Defined by `GetMaxConcurrentSize()`.
* **Scale Up:** If the buffer has pending messages and active workers < Max, new **Temporal Workers** are spawned.
* **Scale Down:** Temporal workers automatically exit after 30 seconds of inactivity (`idleTimeout`).

### RPC / Request-Reply

If you are using NATS Request-Reply patterns, use the `WithResponseOnError` middleware. If your handler returns an error, this middleware captures it and sends it back to the caller in the `Error` header.

```go
sub.Subscribe("rpc.service", "group", 
    subscriber.WithResponseOnError(logger, func(ctx context.Context, msg interfaces.Msg) error {
        if invalid(msg) {
            return errors.New("invalid input") // Caller receives this error string
        }
        return msg.Respond([]byte("OK"))
    }),
)

```

## Interfaces

To support a new backend (e.g., RabbitMQ), implement the `Client` interface:

```go
type Client interface {
	Meter
	Context() context.Context
	Logger() Logger
	Config() Config
	QueueSubscribeSync(subject, queue string) (Subscription, error)
	Close() error
}

```

And the `Config` interface to control scaling:

```go
type Config interface {
	GetReadTimeout() time.Duration
	GetMaxConcurrentSize() uint64 // Burst limit
	GetConcurrentSize() int       // Min/Base workers
}

```

## Metrics

The library uses `go.opentelemetry.io/otel/metric`. Available metrics include:

* `queue.subscriptions.pending.msgs`: Current channel buffer depth.
* `queue.subscriptions.dropped.count`: Messages dropped if buffer is full.
* `queue.subscriptions.send.count`: Successfully processed messages.

## License

[MIT](https://www.google.com/search?q=LICENSE)