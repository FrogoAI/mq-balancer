# mq-balancer

> A backend-agnostic, high-concurrency message queue load balancer for Go, powering scalable worker pools.

`mq-balancer` is a lightweight library designed to decouple message processing logic from the underlying transport layer. It provides a robust worker pool implementation that consumes messages from any source (NATS, Kafka, RabbitMQ) and processes them concurrently using goroutines.

Key features include **dynamic worker scaling**, **OpenTelemetry metrics**, and a **strict interface-driven design**.

## Features

- **Protocol Agnostic:** Define your handler once; run it on NATS, JetStream, or Kafka by swapping the driver.
- **Dynamic Scaling:** Automatically scales worker goroutines up and down based on queue pressure (pending messages).
- **Resiliency:** Built-in graceful shutdown (`Drain`) and error handling middleware.
- **Observability:** Native OpenTelemetry integration for tracking pending messages, dropped counts, and processing throughput.