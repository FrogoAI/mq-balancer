package subscriber

import (
	"errors"
	"time"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
)

const (
	SubscriptionsPendingCount = "queue.subscriptions.pending.msgs"
	SubscriptionsPendingBytes = "queue.subscriptions.pending.bytes"
	SubscriptionsDroppedMsgs  = "queue.subscriptions.dropped.count"
	SubscriptionCountMsgs     = "queue.subscriptions.send.count"

	subscriptionMetricsInterval = 15 * time.Second
)

type SubscriptionDetails struct {
	Pending      int64
	PendingBytes int64
	Dropped      int64
	Delivered    int64
}

func getSubscriptionMetrics(sub mq.Subscription) (*SubscriptionDetails, error) {
	pMsg, pBytes, err := sub.Pending()
	if err != nil {
		return nil, err
	}

	dropped, err := sub.Dropped()
	if err != nil {
		return nil, err
	}

	count, err := sub.Delivered()
	if err != nil {
		return nil, err
	}

	return &SubscriptionDetails{
		Pending:      pMsg,
		PendingBytes: pBytes,
		Dropped:      dropped,
		Delivered:    count,
	}, nil
}

func (s *Subscription) setupMetrics() error {
	meter := s.Meter()
	if meter == nil {
		return nil
	}

	s.recordSubscriptionMetrics(meter)

	if s.ctx == nil {
		return nil
	}

	go s.recordSubscriptionMetricsUntilDone(meter)

	return nil
}

func (s *Subscription) recordSubscriptionMetricsUntilDone(meter mq.Metrics) {
	ticker := time.NewTicker(subscriptionMetricsInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.recordSubscriptionMetrics(meter)
		}
	}
}

func (s *Subscription) recordSubscriptionMetrics(meter mq.Metrics) {
	if err := recordSubscriptionMetrics(meter, s.sub); err != nil {
		s.Client.Logger().Debug("Record subscription metrics failed", "err", err, "subject", s.subject, "queue", s.queue)
	}
}

func recordSubscriptionMetrics(meter mq.Metrics, sub mq.Subscription) error {
	if meter == nil || sub == nil {
		return nil
	}

	details, err := getSubscriptionMetrics(sub)
	if err != nil {
		return err
	}

	tags := []string{"subject:" + sub.Subject()}

	return errors.Join(
		meter.Gauge(SubscriptionsPendingCount, float64(details.Pending), tags),
		meter.Gauge(SubscriptionsPendingBytes, float64(details.PendingBytes), tags),
		meter.Gauge(SubscriptionsDroppedMsgs, float64(details.Dropped), tags),
		meter.Gauge(SubscriptionCountMsgs, float64(details.Delivered), tags),
	)
}
