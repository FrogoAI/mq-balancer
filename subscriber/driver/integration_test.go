//go:build integration

package driver

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/FrogoAI/mq-balancer/subscriber"
	"github.com/FrogoAI/mq-balancer/subscriber/driver/client"
	"github.com/FrogoAI/mq-balancer/subscriber/mq"
	"github.com/FrogoAI/testutils"
)

func TestIntegration_SubscribeAndPublish(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)

	s := subscriber.NewSubscriber(NewNATSSubscriber(conn))

	subject := "test.subscribe"
	received := make(chan string, 10)

	s.Subscribe(subject, "grp", func(_ context.Context, msg mq.Msg) error {
		received <- string(msg.Data())
		return nil
	})

	// Give subscription time to register
	time.Sleep(50 * time.Millisecond)

	err := conn.Conn().Publish(subject, []byte("hello"))
	testutils.Equal(t, err, nil)
	err = conn.Conn().Flush()
	testutils.Equal(t, err, nil)

	select {
	case data := <-received:
		testutils.Equal(t, data, "hello")
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	err = s.Close()
	testutils.Equal(t, err, nil)

	err = s.Wait()
	testutils.Equal(t, err, nil)

	testutils.Equal(t, len(s.Subs().GetMap()), 0)
}

func TestIntegration_RequestReply(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)

	s := subscriber.NewSubscriber(NewNATSSubscriber(conn))

	subject := "test.request"

	s.Subscribe(subject, "grp", func(_ context.Context, msg mq.Msg) error {
		return msg.Respond([]byte("reply:" + string(msg.Data())))
	})

	time.Sleep(50 * time.Millisecond)

	resp, err := conn.Conn().Request(subject, []byte("ping"), 2*time.Second)
	testutils.Equal(t, err, nil)
	testutils.Equal(t, string(resp.Data), "reply:ping")

	err = s.Close()
	testutils.Equal(t, err, nil)

	err = s.Wait()
	testutils.Equal(t, err, nil)
}

func TestIntegration_WithResponseOnError(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)

	s := subscriber.NewSubscriber(NewNATSSubscriber(conn))

	subject := "test.error"

	s.Subscribe(subject, "grp", subscriber.WithResponseOnError(slog.Default(), func(_ context.Context, msg mq.Msg) error {
		return errors.New("handler failed")
	}))

	time.Sleep(50 * time.Millisecond)

	resp, err := conn.Conn().Request(subject, []byte("data"), 2*time.Second)
	testutils.Equal(t, err, nil)
	testutils.Equal(t, resp.Header.Get(subscriber.HeaderConsumerError), "handler failed")

	err = s.Close()
	testutils.Equal(t, err, nil)

	err = s.Wait()
	testutils.Equal(t, err, nil)
}

func TestIntegration_MultipleSubjects(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)

	s := subscriber.NewSubscriber(NewNATSSubscriber(conn))

	var count1, count2 atomic.Int32

	s.Subscribe("subj.a", "grp", func(_ context.Context, msg mq.Msg) error {
		count1.Add(1)
		return nil
	})

	s.Subscribe("subj.b", "grp", func(_ context.Context, msg mq.Msg) error {
		count2.Add(1)
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	for i := 0; i < 5; i++ {
		conn.Conn().Publish("subj.a", []byte(strconv.Itoa(i)))
		conn.Conn().Publish("subj.b", []byte(strconv.Itoa(i)))
	}
	conn.Conn().Flush()

	time.Sleep(200 * time.Millisecond)

	testutils.Equal(t, count1.Load(), int32(5))
	testutils.Equal(t, count2.Load(), int32(5))

	sub := s.Get("subj.a", "grp")
	testutils.Equal(t, sub != nil, true)

	err := s.Close()
	testutils.Equal(t, err, nil)

	err = s.Wait()
	testutils.Equal(t, err, nil)
}

func TestIntegration_SubscribeWithParameters(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)

	s := subscriber.NewSubscriber(NewNATSSubscriber(conn))

	received := make(chan struct{}, 1)

	s.SubscribeWithParameters(2, 100*time.Millisecond, "test.params", "grp", func(_ context.Context, msg mq.Msg) error {
		received <- struct{}{}
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	conn.Conn().Publish("test.params", []byte("data"))
	conn.Conn().Flush()

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	err := s.Close()
	testutils.Equal(t, err, nil)

	err = s.Wait()
	testutils.Equal(t, err, nil)
}

func TestIntegration_ConnectionClose(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)

	s := subscriber.NewSubscriber(NewNATSSubscriber(conn))

	s.Subscribe("test.close", "grp", func(_ context.Context, msg mq.Msg) error {
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	// Close connection while subscriptions are active
	err := conn.Close()
	testutils.Equal(t, err, nil)

	// ForceClose should not panic
	s.ForceClose()
}

func TestIntegration_NATSSubscription_Subject(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	testutils.Equal(t, err, nil)
	defer nc.Close()

	natsSub, err := nc.QueueSubscribeSync("test.sub", "grp")
	testutils.Equal(t, err, nil)

	sub := &NATSSubscription{Subscription: natsSub}
	testutils.Equal(t, sub.Subject(), "test.sub")

	err = sub.Drain()
	testutils.Equal(t, err, nil)
}

func TestIntegration_NATSSubscription_Metrics(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	nc, err := nats.Connect(srv.ClientURL())
	testutils.Equal(t, err, nil)
	defer nc.Close()

	natsSub, err := nc.QueueSubscribeSync("test.metrics", "grp")
	testutils.Equal(t, err, nil)

	// Publish a message
	err = nc.Publish("test.metrics", []byte("data"))
	testutils.Equal(t, err, nil)
	err = nc.Flush()
	testutils.Equal(t, err, nil)

	// Consume it
	msg, err := natsSub.NextMsg(time.Second)
	testutils.Equal(t, err, nil)
	testutils.Equal(t, string(msg.Data), "data")

	sub := &NATSSubscription{Subscription: natsSub}

	// Pending
	pMsg, pBytes, err := sub.Pending()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, pMsg >= 0, true)
	testutils.Equal(t, pBytes >= 0, true)

	// Dropped
	dropped, err := sub.Dropped()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, dropped >= 0, true)

	// Delivered
	delivered, err := sub.Delivered()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, delivered >= 1, true)
}

func TestIntegration_WithMeter(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)
	natsSubscriber := NewNATSSubscriber(conn)

	provider := sdkmetric.NewMeterProvider()
	meter := provider.Meter("test")

	natsSubscriber.WithMeter(meter)
	testutils.Equal(t, natsSubscriber.Meter() != nil, true)

	s := subscriber.NewSubscriber(natsSubscriber)

	received := make(chan struct{}, 1)

	s.Subscribe("test.meter", "grp", func(_ context.Context, msg mq.Msg) error {
		received <- struct{}{}
		return nil
	})

	time.Sleep(50 * time.Millisecond)

	conn.Conn().Publish("test.meter", []byte("data"))
	conn.Conn().Flush()

	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for message")
	}

	// Check that Sub() accessor works on the subscription
	sub := s.Get("test.meter", "grp")
	testutils.Equal(t, sub != nil, true)
	testutils.Equal(t, sub.Sub() != nil, true)

	err := s.Close()
	testutils.Equal(t, err, nil)

	err = s.Wait()
	testutils.Equal(t, err, nil)
}

func TestIntegration_NATSSubscriber_Close(t *testing.T) {
	srv := startTestServer(t)
	defer srv.Shutdown()

	conn := connectClient(t, srv)
	ns := NewNATSSubscriber(conn)

	err := ns.Close()
	testutils.Equal(t, err, nil)
}

func connectClient(t *testing.T, srv interface{ ClientURL() string }) *client.Client {
	t.Helper()

	cfg := &client.Config{
		Addr:                 srv.ClientURL(),
		MaxReconnects:        1,
		ReconnectWait:        100 * time.Millisecond,
		RetryOnFailedConnect: false,
		ConcurrentSizeVal:    2,
		MaxConcurrentSize:    10,
		ReadTimeoutVal:       100 * time.Millisecond,
	}

	nc, err := nats.Connect(cfg.Addr)
	testutils.Equal(t, err, nil)

	c, err := client.NewClient(context.Background(), nc, cfg, slog.Default())
	testutils.Equal(t, err, nil)

	return c
}
