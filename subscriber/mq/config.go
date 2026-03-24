//go:generate mockgen -package mock -source=config.go -destination=mock/config.go
package mq

import "time"

type Config interface {
	ReadTimeout() time.Duration
	MaxConcurrentSize() uint64
	ConcurrentSize() int
}
