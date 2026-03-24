//go:build integration

package client

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	natsserver "github.com/nats-io/nats-server/v2/server"

	"github.com/FrogoAI/testutils"
)

func startTestServer(t *testing.T) *natsserver.Server {
	t.Helper()

	opts := &natsserver.Options{
		Host: "127.0.0.1",
		Port: -1,
	}

	srv, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("failed to create test NATS server: %v", err)
	}

	go srv.Start()

	if !srv.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready for connections")
	}

	return srv
}

func TestIntegration_NewClient(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	testutils.Equal(t, err, nil)

	cfg := &Config{Addr: srv.ClientURL()}
	c, err := NewClient(context.Background(), nc, cfg, slog.Default())
	testutils.Equal(t, err, nil)
	testutils.Equal(t, c != nil, true)
	testutils.Equal(t, c.Conn() != nil, true)

	err = c.Close()
	testutils.Equal(t, err, nil)
}

func TestIntegration_QueueSubscribeSync(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	testutils.Equal(t, err, nil)

	cfg := &Config{Addr: srv.ClientURL()}
	c, err := NewClient(context.Background(), nc, cfg, slog.Default())
	testutils.Equal(t, err, nil)

	sub, err := c.QueueSubscribeSync("test.sub", "grp")
	testutils.Equal(t, err, nil)
	testutils.Equal(t, sub != nil, true)

	err = c.Close()
	testutils.Equal(t, err, nil)
}

func TestIntegration_NewClient_FlushError(t *testing.T) {
	srv := startTestServer(t)

	nc, err := nats.Connect(srv.ClientURL())
	testutils.Equal(t, err, nil)

	nc.Close()
	srv.Shutdown()

	cfg := &Config{Addr: "nats://invalid:4222"}
	c, err := NewClient(context.Background(), nc, cfg, slog.Default())
	testutils.Equal(t, c == nil, true)
	testutils.Equal(t, err != nil, true)
}

func TestIntegration_Default_ConnectError(t *testing.T) {
	t.Setenv("NATS_ADDR", "nats://invalid:4222")
	t.Setenv("NATS_RETRY_ON_FAILED_CONNECT", "false")

	_, err := Default(context.Background(), slog.Default())
	testutils.Equal(t, err != nil, true)
}

func TestIntegration_Default(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	t.Setenv("NATS_ADDR", srv.ClientURL())
	t.Setenv("NATS_RETRY_ON_FAILED_CONNECT", "false")

	c, err := Default(context.Background(), slog.Default())
	testutils.Equal(t, err, nil)
	testutils.Equal(t, c != nil, true)

	err = c.Close()
	testutils.Equal(t, err, nil)
}
