package driver

import (
	"context"
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

func TestNATSSubscription_NextMsg_InvalidSubscription(t *testing.T) {
	sub := &NATSSubscription{Subscription: &nats.Subscription{}}
	msg, err := sub.NextMsg(time.Second)
	testutils.Equal(t, msg == nil, true)
	testutils.Equal(t, errors.Is(err, subscriber.ErrConnectionClosed), true)
}

func TestNATSSubscription_Drain_InvalidSubscription(t *testing.T) {
	sub := &NATSSubscription{Subscription: &nats.Subscription{}}
	err := sub.Drain()
	testutils.Equal(t, err != nil, true)
}

func TestNATSSubscription_Subject(t *testing.T) {
	sub := &NATSSubscription{Subscription: &nats.Subscription{Subject: "test.sub"}}
	testutils.Equal(t, sub.Subject(), "test.sub")
}

func TestNATSSubscription_Pending_InvalidSubscription(t *testing.T) {
	sub := &NATSSubscription{Subscription: &nats.Subscription{}}
	_, _, err := sub.Pending()
	testutils.Equal(t, err != nil, true)
}

func TestNATSSubscription_Dropped_InvalidSubscription(t *testing.T) {
	sub := &NATSSubscription{Subscription: &nats.Subscription{}}
	_, err := sub.Dropped()
	testutils.Equal(t, err != nil, true)
}

func TestNATSSubscription_Delivered_InvalidSubscription(t *testing.T) {
	sub := &NATSSubscription{Subscription: &nats.Subscription{}}
	_, err := sub.Delivered()
	testutils.Equal(t, err != nil, true)
}

// TestClassifyNextMsgErr pins the mapping the reader loop depends on. Getting
// ErrTimeout wrong here is what silently killed idle subscriptions: it is the
// normal quiet path, not a fault, and must not be classified as one.
func TestClassifyNextMsgErr(t *testing.T) {
	cases := []struct {
		name      string
		natsErr   error
		want      error
		unwrapped bool
	}{
		{
			name:    "timeout is its own sentinel, not a failure",
			natsErr: nats.ErrTimeout,
			want:    subscriber.ErrTimeout,
		},
		{
			name:    "deadline exceeded is a timeout",
			natsErr: context.DeadlineExceeded,
			want:    subscriber.ErrTimeout,
		},
		{
			name:    "slow consumer is recoverable, not a closed connection",
			natsErr: nats.ErrSlowConsumer,
			want:    subscriber.ErrSlowConsumer,
		},
		{
			name:    "max messages is terminal but legitimate",
			natsErr: nats.ErrMaxMessages,
			want:    subscriber.ErrMaxMessages,
		},
		{
			name:    "draining is a clean shutdown",
			natsErr: nats.ErrConnectionDraining,
			want:    subscriber.ErrDraining,
		},
		{
			name:    "connection closed",
			natsErr: nats.ErrConnectionClosed,
			want:    subscriber.ErrConnectionClosed,
		},
		{
			name:    "bad subscription",
			natsErr: nats.ErrBadSubscription,
			want:    subscriber.ErrConnectionClosed,
		},
		{
			name:    "sync sub required",
			natsErr: nats.ErrSyncSubRequired,
			want:    subscriber.ErrConnectionClosed,
		},
		{
			name:    "invalid connection",
			natsErr: nats.ErrInvalidConnection,
			want:    subscriber.ErrConnectionClosed,
		},
		{
			name:      "unknown errors pass through untouched",
			natsErr:   errors.New("something else"),
			unwrapped: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyNextMsgErr(tc.natsErr)

			// The original error is always preserved for logging.
			testutils.Equal(t, errors.Is(got, tc.natsErr), true)

			if tc.unwrapped {
				testutils.Equal(t, got, tc.natsErr)

				return
			}

			testutils.Equal(t, errors.Is(got, tc.want), true)
		})
	}
}

// TestClassifyNextMsgErr_TimeoutIsNotConnectionClosed is called out separately
// because conflating the two is the exact defect this mapping fixes.
func TestClassifyNextMsgErr_TimeoutIsNotConnectionClosed(t *testing.T) {
	got := classifyNextMsgErr(nats.ErrTimeout)

	testutils.Equal(t, errors.Is(got, subscriber.ErrConnectionClosed), false)
	testutils.Equal(t, errors.Is(got, subscriber.ErrSlowConsumer), false)
}

func TestClassifySubscribeErr(t *testing.T) {
	cases := []struct {
		name    string
		natsErr error
		fatal   bool
	}{
		{
			name:    "closed connection is unrecoverable",
			natsErr: nats.ErrConnectionClosed,
			fatal:   true,
		},
		{
			name:    "invalid connection is unrecoverable",
			natsErr: nats.ErrInvalidConnection,
			fatal:   true,
		},
		{
			name:    "no servers is transient and worth retrying",
			natsErr: nats.ErrNoServers,
		},
		{
			name:    "unknown errors are transient",
			natsErr: errors.New("something else"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySubscribeErr(tc.natsErr)

			testutils.Equal(t, errors.Is(got, tc.natsErr), true)
			testutils.Equal(t, errors.Is(got, subscriber.ErrConnectionClosed), tc.fatal)
		})
	}
}
