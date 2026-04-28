//go:generate mockgen -package mock -source=meter.go -destination=mock/meter.go
package mq

type Metrics interface {
	Count(name string, value int64, tags []string) error
	Gauge(name string, value float64, tags []string) error
	Distribution(name string, value float64, tags []string) error
}

type Meter interface {
	Meter() Metrics
	WithMeter(Metrics)
}
