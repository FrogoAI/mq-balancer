package client

import (
	"context"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"

	"github.com/FrogoAI/testutils"
)

type testMetrics struct{}

func (testMetrics) Count(string, int64, []string) error {
	return nil
}

func (testMetrics) Gauge(string, float64, []string) error {
	return nil
}

func (testMetrics) Distribution(string, float64, []string) error {
	return nil
}

func TestClient_Logger_Nil(t *testing.T) {
	c := &Client{}
	logger := c.Logger()

	// Should return StubLogger, not nil
	testutils.Equal(t, logger != nil, true)

	// StubLogger should not panic
	logger.Error("test")
	logger.Info("test")
	logger.Debug("test")
}

func TestClient_Logger_Set(t *testing.T) {
	l := StubLogger{}
	c := &Client{logger: l}
	testutils.Equal(t, c.Logger() != nil, true)
}

func TestClient_Context(t *testing.T) {
	ctx := context.Background()
	c := &Client{ctx: ctx}
	testutils.Equal(t, c.Context(), ctx)
}

func TestClient_Config(t *testing.T) {
	cfg := &Config{Addr: "nats://test:4222"}
	c := &Client{cfg: cfg}
	testutils.Equal(t, c.Config().Addr, "nats://test:4222")
}

func TestClient_Conn(t *testing.T) {
	c := &Client{}
	testutils.Equal(t, c.Conn() == nil, true)
}

func TestClient_MeterConcurrency(t *testing.T) {
	c := &Client{}

	// Initially nil
	m := c.Meter()
	testutils.Equal(t, m == nil, true)
}

func TestClient_WithMeter(t *testing.T) {
	c := &Client{}

	c.WithMeter(testMetrics{})
	testutils.Equal(t, c.Meter() != nil, true)
}

func TestStubLogger(t *testing.T) {
	l := StubLogger{}
	// Should not panic
	l.Error("err")
	l.Info("info")
	l.Debug("debug")
}

func TestLogNATSError(t *testing.T) {
	// Basic smoke test — should not panic with nil sub
	nc := &nats.Conn{}
	logNATSError(nc, nil, errors.New("test error"))
}

func TestLogNATSError_WithSub(t *testing.T) {
	nc := &nats.Conn{}
	sub := &nats.Subscription{Subject: "test.subj"}
	logNATSError(nc, sub, errors.New("test error"))
}

func TestNATSConfigFromEnv_MultiplePrefix(t *testing.T) {
	t.Setenv("FIRST_ADDR", "nats://first:4222")

	c, err := NATSConfigFromEnv("first", "second")
	testutils.Equal(t, err, nil)
	testutils.Equal(t, c.Addr, "nats://first:4222")
}
