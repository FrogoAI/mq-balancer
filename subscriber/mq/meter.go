//go:generate mockgen -package mock -source=meter.go -destination=mock/meter.go
package mq

import "go.opentelemetry.io/otel/metric"

type Meter interface {
	Meter() metric.Meter
	WithMeter(metric.Meter)
}
