//go:generate mockgen -package mock -source=client.go -destination=mock/client.go
package mq

import "context"

type Client interface {
	Meter
	Context() context.Context
	Logger() Logger
	Config() Config
	QueueSubscribeSync(subject, queue string) (Subscription, error)
	Close() error
}
