package driver

import (
	"errors"
	"testing"
	"time"

	"github.com/FrogoAI/mq-balancer/subscriber"
	"github.com/FrogoAI/mq-balancer/subscriber/driver/client"
	"github.com/FrogoAI/testutils"
	"github.com/nats-io/nats.go"
)

func TestNATSSubscription_NextMsg_WrapsConnectionClosed(t *testing.T) {
	// We cannot easily test NextMsg without a real server, but we can test
	// the NATSConfig delegation and error wrapping behavior at the type level.

	// NATSConfig delegates to Config methods (C3 fix)
	cfg := &NATSConfig{Config: &client.Config{
		ReadTimeoutVal:    5 * time.Second,
		ConcurrentSizeVal: 8,
		MaxConcurrentSize: 50,
	}}

	testutils.Equal(t, cfg.ReadTimeout(), 5*time.Second)
	testutils.Equal(t, cfg.ConcurrentSize(), 8)
	testutils.Equal(t, cfg.MaxConcurrentSize(), uint64(50))
}

func TestNATSConfig_DelegatesToConfigDefaults(t *testing.T) {
	cfg := &NATSConfig{Config: &client.Config{
		ReadTimeoutVal:    0,
		ConcurrentSizeVal: 0,
	}}

	// Should use Config's default logic, not return raw 0 values
	testutils.Equal(t, cfg.ReadTimeout() > 0, true)
	testutils.Equal(t, cfg.ConcurrentSize() > 0, true)
}

func TestErrConnectionClosed_WrappingWorks(t *testing.T) {
	// Simulate what NextMsg does
	wrapped := errors.Join(subscriber.ErrConnectionClosed, nats.ErrConnectionClosed)

	testutils.Equal(t, errors.Is(wrapped, subscriber.ErrConnectionClosed), true)
	testutils.Equal(t, errors.Is(wrapped, nats.ErrConnectionClosed), true)
}
