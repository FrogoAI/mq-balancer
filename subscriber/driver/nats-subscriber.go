package driver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"

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
	return m.Reply != ""
}

func (m *NATSMsg) ReplyTo() string {
	return m.Reply
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
			Reply:   m.Reply,
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
	if !s.IsValid() {
		return nil, fmt.Errorf("%w: subscription invalid", subscriber.ErrConnectionClosed)
	}

	msg, err := s.Subscription.NextMsg(timeout)
	if err != nil {
		return nil, classifyNextMsgErr(err)
	}

	return &NATSMsg{Msg: msg}, nil
}

// classifyNextMsgErr maps a NATS read error onto a subscriber sentinel so the
// subscriber package can decide how to react without importing nats.
//
// The distinction that matters most is ErrTimeout: NextMsg returns it whenever
// no message arrived within the poll window, which is the normal idle path and
// must never be treated as a failure. ErrSlowConsumer is likewise recoverable —
// nats clears the flag as it is read and the subscription stays valid.
func classifyNextMsgErr(err error) error {
	switch {
	case errors.Is(err, nats.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return fmt.Errorf("%w: %w", subscriber.ErrTimeout, err)

	case errors.Is(err, nats.ErrSlowConsumer):
		return fmt.Errorf("%w: %w", subscriber.ErrSlowConsumer, err)

	case errors.Is(err, nats.ErrMaxMessages):
		return fmt.Errorf("%w: %w", subscriber.ErrMaxMessages, err)

	case errors.Is(err, nats.ErrConnectionDraining):
		return fmt.Errorf("%w: %w", subscriber.ErrDraining, err)

	case errors.Is(err, nats.ErrConnectionClosed),
		errors.Is(err, nats.ErrBadSubscription),
		errors.Is(err, nats.ErrSyncSubRequired),
		errors.Is(err, nats.ErrInvalidConnection):
		return fmt.Errorf("%w: %w", subscriber.ErrConnectionClosed, err)
	}

	return err
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

func (n *NATSSubscriber) WithMeter(m mq.Metrics) {
	n.Conn.WithMeter(m)
}

func (n *NATSSubscriber) Meter() mq.Metrics {
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
		return nil, classifySubscribeErr(err)
	}

	return &NATSSubscription{Subscription: sub}, nil
}

// classifySubscribeErr marks the failures no retry can recover from. A closed
// or invalid connection can never be subscribed to again; everything else is
// transient and worth retrying.
func classifySubscribeErr(err error) error {
	if errors.Is(err, nats.ErrConnectionClosed) || errors.Is(err, nats.ErrInvalidConnection) {
		return fmt.Errorf("%w: %w", subscriber.ErrConnectionClosed, err)
	}

	return err
}
