package subscriber

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
	"github.com/FrogoAI/mq-balancer/subscriber/mq/mock"
	"github.com/FrogoAI/testutils"
	"go.uber.org/mock/gomock"
)

func TestNewSubscription(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)
	testutils.Equal(t, s != nil, true)
	testutils.Equal(t, s.Sub(), sub)
}

func TestNewSubscription_Error(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(nil, errors.New("subscribe failed"))

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err != nil, true)
	testutils.Equal(t, s == nil, true)
}

func TestProcess_ReaderExitsOnConnectionClosed(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil)
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	sub.EXPECT().NextMsg(gomock.Any()).Return(nil,
		fmt.Errorf("%w: nats: connection closed", ErrConnectionClosed)).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			return nil
		})
	}()

	select {
	case err := <-done:
		// Process should return (workers exit because channel closes)
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after ErrConnectionClosed")
	}
}

func TestProcess_ReaderExitsOnConsecutiveErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil)
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	// Return a non-fatal error repeatedly to trigger consecutive error guard
	sub.EXPECT().NextMsg(gomock.Any()).Return(nil, errors.New("some transient error")).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			return nil
		})
	}()

	select {
	case err := <-done:
		// Reader should exit after maxConsecutiveErrors, closing channel, workers exit
		_ = err
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after consecutive errors")
	}
}

func TestProcess_ConsecutiveErrorsResetOnSuccess(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)
	msgMock := mock.NewMockMsg(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil)
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	// Alternate: 50 errors, 1 success, 50 errors, 1 success, ... never reaches 100 consecutive
	var callCount atomic.Int64
	sub.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		n := callCount.Add(1)
		if n%51 == 0 {
			return msgMock, nil
		}
		return nil, errors.New("transient")
	}).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	var handled atomic.Int64
	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			handled.Add(1)
			return nil
		})
	}()

	// Wait for at least 2 successful messages (proving counter resets)
	deadline := time.After(5 * time.Second)
	for handled.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("did not receive enough successful messages — counter may not be resetting")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after cancel")
	}
}

func TestProcess_NonBlockingSendExitsOnCancel(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)
	msgMock := mock.NewMockMsg(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil)
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	// Always return a message — the channel will fill up
	sub.EXPECT().NextMsg(gomock.Any()).Return(msgMock, nil).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	done := make(chan error, 1)
	go func() {
		// buffer=0 → corrected to 1 at Subscriber level, but Process uses it directly
		// Use buffer=1 with a handler that blocks forever to fill the channel
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(ctx context.Context, _ mq.Msg) error {
			// Block until context cancelled — simulates stuck workers
			<-ctx.Done()
			return ctx.Err()
		})
	}()

	// Give time for channel to fill
	time.Sleep(200 * time.Millisecond)

	// Cancel context — reader should unblock from the select on ch <- msg
	cancel()

	select {
	case <-done:
		// Process exited — reader was not stuck on ch <- msg
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit — reader likely blocked on ch <- msg")
	}
}

func TestProcess_SetupMetricsError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)

	// Return a failing meter so setupMetrics returns an error
	provider := sdkmetric.NewMeterProvider()
	realMeter := provider.Meter("test")
	fm := &failingMeter{Meter: realMeter, failAt: 1}
	cl.EXPECT().Meter().Return(fm)

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	err = s.Process(context.Background(), 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
		return nil
	})
	testutils.Equal(t, err != nil, true)
}

func TestProcess_StopDuringProcess(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil)
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	// Slow down NextMsg to avoid hitting maxConsecutiveErrors before Stop
	sub.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, errors.New("timeout")
	}).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	done := make(chan error, 1)
	go func() {
		// Use context.Background() so only s.ctx cancellation (via Stop) exits the reader
		done <- s.Process(context.Background(), 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			return nil
		})
	}()

	time.Sleep(200 * time.Millisecond)

	err = s.Stop()
	testutils.Equal(t, err, nil)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after Stop")
	}
}

func TestStop_CancelsContext(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	sub.EXPECT().Drain().Return(nil)

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	err = s.Stop()
	testutils.Equal(t, err, nil)

	// s.ctx should be cancelled after Stop
	select {
	case <-s.ctx.Done():
		// expected
	default:
		t.Fatal("s.ctx not cancelled after Stop")
	}
}
