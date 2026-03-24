package client

import (
	"errors"
	"log/slog"

	"github.com/nats-io/nats.go"
)

type Logger interface {
	Error(msg string, args ...any)
	Info(msg string, args ...any)
	Debug(msg string, args ...any)
}

type StubLogger struct{}

func (l StubLogger) Error(_ string, _ ...any) {}
func (l StubLogger) Info(_ string, _ ...any)  {}
func (l StubLogger) Debug(_ string, _ ...any) {}

func logNATSError(nc *nats.Conn, sub *nats.Subscription, err error) {
	cid, cerr := nc.GetClientID()
	if cerr != nil {
		err = errors.Join(cerr, err)
	}

	if sub != nil {
		slog.Error("Error on connection",
			"err", err, "cid", cid, "subject", sub.Subject)
	} else {
		slog.Error("Error on connection",
			"err", err, "cid", cid)
	}
}
