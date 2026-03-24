//go:generate mockgen -package mock -source=logger.go -destination=mock/logger.go
package mq

type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}
