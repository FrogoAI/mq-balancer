package driver

import (
	"context"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel/metric"

	"github.com/FrogoAI/mq-balancer/subscriber"
	"github.com/FrogoAI/mq-balancer/subscriber/driver/client"
	"github.com/FrogoAI/mq-balancer/subscriber/mq"
)

type NATSMsg struct {
	*nats.Msg
}

func (m *NATSMsg) Subject() string {
	return m.Msg.Subject
}

func (m *NATSMsg) IsReply() bool {
	return m.Msg.Reply != ""
}

func (m *NATSMsg) ReplyTo() string {
	return m.Msg.Reply
}

func (m *NATSMsg) Copy(subject string) mq.Msg {
	var hdr nats.Header
	if m.Msg.Header != nil {
		hdr = make(nats.Header, len(m.Msg.Header))
		for k, v := range m.Msg.Header {
			hdr[k] = append([]string(nil), v...)
		}
	}

	data := make([]byte, len(m.Msg.Data))
	copy(data, m.Msg.Data)

	return &NATSMsg{
		Msg: &nats.Msg{
			Data:    data,
			Header:  hdr,
			Reply:   m.Msg.Reply,
			Subject: subject,
		},
	}
}

func (m *NATSMsg) SetHeader(key, value string) {
	if m.Msg.Header == nil {
		m.Msg.Header = nats.Header{}
	}

	m.Msg.Header.Set(key, value)
}

func (m *NATSMsg) Respond(data []byte) error {
	return m.Msg.Respond(data)
}

func (m *NATSMsg) Header() map[string][]string {
	return m.Msg.Header
}

func (m *NATSMsg) Data() []byte {
	return m.Msg.Data
}

func (m *NATSMsg) RespondMsg(msg mq.Msg) error {
	return m.Msg.RespondMsg(&nats.Msg{
		Data:    msg.Data(),
		Header:  msg.Header(),
		Reply:   msg.ReplyTo(),
		Subject: msg.Subject(),
	})
}

type NATSSubscription struct {
	*nats.Subscription
}

func (s *NATSSubscription) NextMsg(timeout time.Duration) (mq.Msg, error) {
	msg, err := s.Subscription.NextMsg(timeout)
	if err != nil {
		if err == nats.ErrConnectionClosed || err == nats.ErrConnectionDraining {
			return nil, fmt.Errorf("%w: %w", subscriber.ErrConnectionClosed, err)
		}

		return nil, err
	}

	return &NATSMsg{Msg: msg}, nil
}

func (s *NATSSubscription) Drain() error {
	return s.Subscription.Drain()
}

func (s *NATSSubscription) Subject() string {
	return s.Subscription.Subject
}

func (s *NATSSubscription) Pending() (int64, int64, error) {
	v1, v2, err := s.Subscription.Pending()
	return int64(v1), int64(v2), err
}

func (s *NATSSubscription) Dropped() (int64, error) {
	v, err := s.Subscription.Dropped()
	return int64(v), err
}

func (s *NATSSubscription) Delivered() (int64, error) {
	return s.Subscription.Delivered()
}

type NATSConfig struct {
	*client.Config
}

func (c *NATSConfig) ReadTimeout() time.Duration {
	return c.Config.ReadTimeout()
}

func (c *NATSConfig) MaxConcurrentSize() uint64 {
	return c.Config.MaxConcurrentSize
}

func (c *NATSConfig) ConcurrentSize() int {
	return c.Config.ConcurrentSize()
}

type NATSSubscriber struct {
	Conn *client.Client
}

func NewNATSSubscriber(conn *client.Client) *NATSSubscriber {
	return &NATSSubscriber{Conn: conn}
}

func (n *NATSSubscriber) WithMeter(m metric.Meter) {
	n.Conn.WithMeter(m)
}

func (n *NATSSubscriber) Meter() metric.Meter {
	return n.Conn.Meter()
}

func (n *NATSSubscriber) Context() context.Context {
	return n.Conn.Context()
}

func (n *NATSSubscriber) Logger() mq.Logger {
	return n.Conn.Logger()
}

func (n *NATSSubscriber) Config() mq.Config {
	return &NATSConfig{
		Config: n.Conn.Config(),
	}
}

func (n *NATSSubscriber) Close() error {
	return n.Conn.Close()
}

func (n *NATSSubscriber) QueueSubscribeSync(subject, queue string) (mq.Subscription, error) {
	sub, err := n.Conn.QueueSubscribeSync(subject, queue)
	if err != nil {
		return nil, err
	}

	return &NATSSubscription{Subscription: sub}, nil
}
