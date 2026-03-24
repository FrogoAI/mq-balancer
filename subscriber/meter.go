package subscriber

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
)

const (
	SubscriptionsPendingCount = "queue.subscriptions.pending.msgs"
	SubscriptionsPendingBytes = "queue.subscriptions.pending.bytes"
	SubscriptionsDroppedMsgs  = "queue.subscriptions.dropped.count"
	SubscriptionCountMsgs     = "queue.subscriptions.send.count"

	Bytes string = "By"

	SubjectKey = attribute.Key("subject")
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

	_, err := meter.Int64ObservableGauge(SubscriptionsPendingCount,
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			details, err := getSubscriptionMetrics(s.sub)
			if err != nil {
				return err
			}

			observer.Observe(details.Pending, metric.WithAttributes(SubjectKey.String(s.sub.Subject())))

			return nil
		}))
	if err != nil {
		return err
	}

	_, err = meter.Int64ObservableGauge(SubscriptionsPendingBytes, metric.WithUnit(Bytes),
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			details, err := getSubscriptionMetrics(s.sub)
			if err != nil {
				return err
			}

			observer.Observe(details.PendingBytes, metric.WithAttributes(SubjectKey.String(s.sub.Subject())))

			return nil
		}))
	if err != nil {
		return err
	}

	_, err = meter.Int64ObservableGauge(SubscriptionsDroppedMsgs,
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			details, err := getSubscriptionMetrics(s.sub)
			if err != nil {
				return err
			}

			observer.Observe(details.Dropped, metric.WithAttributes(SubjectKey.String(s.sub.Subject())))

			return nil
		}))
	if err != nil {
		return err
	}

	_, err = meter.Int64ObservableGauge(SubscriptionCountMsgs,
		metric.WithInt64Callback(func(_ context.Context, observer metric.Int64Observer) error {
			details, err := getSubscriptionMetrics(s.sub)
			if err != nil {
				return err
			}

			observer.Observe(details.Delivered, metric.WithAttributes(SubjectKey.String(s.sub.Subject())))

			return nil
		}))
	if err != nil {
		return err
	}

	return nil
}
