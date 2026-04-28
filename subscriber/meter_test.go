package subscriber

import (
	"errors"
	"testing"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
	"github.com/FrogoAI/mq-balancer/subscriber/mq/mock"
	"github.com/FrogoAI/testutils"
	"go.uber.org/mock/gomock"
)

type recordedMetric struct {
	name  string
	value float64
	tags  []string
}

type recordingMetrics struct {
	err    error
	gauges []recordedMetric
}

func (m *recordingMetrics) Count(string, int64, []string) error {
	return m.err
}

func (m *recordingMetrics) Gauge(name string, value float64, tags []string) error {
	m.gauges = append(m.gauges, recordedMetric{name: name, value: value, tags: tags})

	return m.err
}

func (m *recordingMetrics) Distribution(string, float64, []string) error {
	return m.err
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

func TestRecordSubscriptionMetrics_MeterError(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)
	meter := &recordingMetrics{err: errors.New("record failed")}

	sub.EXPECT().Pending().Return(int64(5), int64(100), nil)
	sub.EXPECT().Dropped().Return(int64(1), nil)
	sub.EXPECT().Delivered().Return(int64(50), nil)
	sub.EXPECT().Subject().Return("test.subject")

	err := recordSubscriptionMetrics(meter, sub)
	testutils.Equal(t, err != nil, true)
}

func TestRecordSubscriptionMetrics_GetDetailsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)
	meter := &recordingMetrics{}

	sub.EXPECT().Pending().Return(int64(0), int64(0), errTest)

	err := recordSubscriptionMetrics(meter, sub)
	testutils.Equal(t, err != nil, true)
}

func TestRecordSubscriptionMetrics_WithMeter(t *testing.T) {
	ctrl := gomock.NewController(t)
	sub := mock.NewMockSubscription(ctrl)
	meter := &recordingMetrics{}

	sub.EXPECT().Pending().Return(int64(5), int64(100), nil)
	sub.EXPECT().Dropped().Return(int64(1), nil)
	sub.EXPECT().Delivered().Return(int64(50), nil)
	sub.EXPECT().Subject().Return("test.subject")

	err := recordSubscriptionMetrics(meter, sub)
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(meter.gauges), 4)
	testutils.Equal(t, meter.gauges[0].name, SubscriptionsPendingCount)
	testutils.Equal(t, meter.gauges[0].value, float64(5))
	testutils.Equal(t, meter.gauges[0].tags[0], "subject:test.subject")
}

func TestSetupMetrics_WithMeter(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)
	meter := &recordingMetrics{}

	cl.EXPECT().Meter().Return(meter)

	sub.EXPECT().Subject().Return("test.subject").AnyTimes()
	sub.EXPECT().Pending().Return(int64(5), int64(100), nil)
	sub.EXPECT().Dropped().Return(int64(1), nil)
	sub.EXPECT().Delivered().Return(int64(50), nil)

	s := &Subscription{Client: cl, sub: sub}

	err := s.setupMetrics()
	testutils.Equal(t, err, nil)
	testutils.Equal(t, len(meter.gauges), 4)
}

var _ mq.Metrics = (*recordingMetrics)(nil)
