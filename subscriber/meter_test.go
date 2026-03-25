package subscriber

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/FrogoAI/mq-balancer/subscriber/mq/mock"
	"github.com/FrogoAI/testutils"
	"go.uber.org/mock/gomock"
)

// failingMeter wraps a real meter but fails Int64ObservableGauge at a specific call.
type failingMeter struct {
	metric.Meter
	failAt    int
	callCount int
}

func (m *failingMeter) Int64ObservableGauge(name string, options ...metric.Int64ObservableGaugeOption) (metric.Int64ObservableGauge, error) {
	m.callCount++
	if m.callCount >= m.failAt {
		return nil, errors.New("gauge creation failed")
	}
	return m.Meter.Int64ObservableGauge(name, options...)
}

func TestGetSubscriptionMetrics(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)

	sub.EXPECT().Pending().Return(int64(10), int64(200), nil)
	sub.EXPECT().Dropped().Return(int64(2), nil)
	sub.EXPECT().Delivered().Return(int64(100), nil)

	details, err := getSubscriptionMetrics(sub)
	testutils.Equal(t, err, nil)
	testutils.Equal(t, details.Pending, int64(10))
	testutils.Equal(t, details.PendingBytes, int64(200))
	testutils.Equal(t, details.Dropped, int64(2))
	testutils.Equal(t, details.Delivered, int64(100))
}

func TestGetSubscriptionMetrics_PendingError(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)

	sub.EXPECT().Pending().Return(int64(0), int64(0), errTest)

	_, err := getSubscriptionMetrics(sub)
	testutils.Equal(t, err != nil, true)
}

func TestGetSubscriptionMetrics_DroppedError(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)

	sub.EXPECT().Pending().Return(int64(0), int64(0), nil)
	sub.EXPECT().Dropped().Return(int64(0), errTest)

	_, err := getSubscriptionMetrics(sub)
	testutils.Equal(t, err != nil, true)
}

func TestGetSubscriptionMetrics_DeliveredError(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)

	sub.EXPECT().Pending().Return(int64(0), int64(0), nil)
	sub.EXPECT().Dropped().Return(int64(0), nil)
	sub.EXPECT().Delivered().Return(int64(0), errTest)

	_, err := getSubscriptionMetrics(sub)
	testutils.Equal(t, err != nil, true)
}

func TestSetupMetrics_NilMeter(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)

	cl.EXPECT().Meter().Return(nil)

	s := &Subscription{Client: cl}

	err := s.setupMetrics()
	testutils.Equal(t, err, nil)
}

func TestSetupMetrics_GaugeCreationError(t *testing.T) {
	cases := []struct {
		name   string
		failAt int
	}{
		{"first gauge fails", 1},
		{"second gauge fails", 2},
		{"third gauge fails", 3},
		{"fourth gauge fails", 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cl := mock.NewMockClient(ctrl)

			provider := sdkmetric.NewMeterProvider()
			realMeter := provider.Meter("test")
			fm := &failingMeter{Meter: realMeter, failAt: tc.failAt}

			cl.EXPECT().Meter().Return(fm)

			s := &Subscription{Client: cl}

			err := s.setupMetrics()
			testutils.Equal(t, err != nil, true)
		})
	}
}

func TestSetupMetrics_CallbackError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	cl.EXPECT().Meter().Return(meter)

	sub.EXPECT().Subject().Return("test.subject").AnyTimes()
	sub.EXPECT().Pending().Return(int64(0), int64(0), errTest).AnyTimes()

	s := &Subscription{Client: cl, sub: sub}

	err := s.setupMetrics()
	testutils.Equal(t, err, nil)

	// Trigger metric collection — callbacks should hit error paths
	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	testutils.Equal(t, err != nil, true)
}

func TestSetupMetrics_WithMeter(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")

	cl.EXPECT().Meter().Return(meter)

	sub.EXPECT().Subject().Return("test.subject").AnyTimes()
	sub.EXPECT().Pending().Return(int64(5), int64(100), nil).AnyTimes()
	sub.EXPECT().Dropped().Return(int64(1), nil).AnyTimes()
	sub.EXPECT().Delivered().Return(int64(50), nil).AnyTimes()

	s := &Subscription{Client: cl, sub: sub}

	err := s.setupMetrics()
	testutils.Equal(t, err, nil)

	// Trigger metric collection to exercise callbacks
	var rm metricdata.ResourceMetrics
	err = reader.Collect(context.Background(), &rm)
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(rm.ScopeMetrics) > 0, true)
}
