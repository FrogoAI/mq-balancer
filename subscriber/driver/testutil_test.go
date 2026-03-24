//go:build integration

package driver

import (
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

func startTestServer(t *testing.T) *natsserver.Server {
	t.Helper()

	opts := &natsserver.Options{
		Host: "127.0.0.1",
		Port: -1, // random port
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
