package subscriber

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
	"github.com/FrogoAI/mq-balancer/subscriber/mq/mock"
	"github.com/FrogoAI/testutils"
	"go.uber.org/mock/gomock"
)

// errTimeout mimics what the driver produces for an idle poll.
func errTimeout() error {
	return fmt.Errorf("%w: nats: timeout", ErrTimeout)
}

func errConnectionClosed() error {
	return fmt.Errorf("%w: nats: connection closed", ErrConnectionClosed)
}

// waitFor polls until cond holds, failing the test if it never does.
func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()

	deadline := time.After(5 * time.Second)

	for !cond() {
		select {
		case <-deadline:
			t.Fatal(msg)
		case <-time.After(10 * time.Millisecond):
		}
	}
}

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
	testutils.Equal(t, s.Alive(), false)
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

// TestProcess_SurvivesRepeatedTimeouts is the regression test for the reported
// production failure: a subject with no traffic returns a read timeout on every
// poll, and those must never accumulate into a reason to stop reading.
func TestProcess_SurvivesRepeatedTimeouts(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	var polls atomic.Int64

	sub.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		polls.Add(1)
		time.Sleep(time.Millisecond)

		return nil, errTimeout()
	}).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			return nil
		})
	}()

	// Well past the 100-poll threshold that used to retire the reader.
	waitFor(t, func() bool { return polls.Load() > 200 }, "reader stopped polling while idle")

	select {
	case err := <-done:
		t.Fatalf("Process exited after %d idle polls: %v", polls.Load(), err)
	default:
	}

	testutils.Equal(t, s.Alive(), true)

	cancel()

	select {
	case err := <-done:
		testutils.Equal(t, err, nil)
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after cancel")
	}

	testutils.Equal(t, s.Alive(), false)
}

// TestProcess_SurvivesRepeatedSlowConsumer covers the other recoverable error
// that used to be fatal: a slow consumer drops messages but leaves the
// subscription readable.
func TestProcess_SurvivesRepeatedSlowConsumer(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)
	msgMock := mock.NewMockMsg(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	const slowConsumerRuns = 150

	var calls atomic.Int64

	sub.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		if calls.Add(1) <= slowConsumerRuns {
			return nil, fmt.Errorf("%w: nats: slow consumer, messages dropped", ErrSlowConsumer)
		}

		time.Sleep(time.Millisecond)

		return msgMock, nil
	}).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	var handled atomic.Int64

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			handled.Add(1)

			return nil
		})
	}()

	waitFor(t, func() bool { return handled.Load() > 0 },
		"no messages delivered after slow-consumer errors — reader stopped")

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after cancel")
	}
}

// TestProcess_RecoversAfterTransientErrors proves an unclassified error backs
// off rather than retiring the reader, and that recovery resumes delivery.
func TestProcess_RecoversAfterTransientErrors(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)
	msgMock := mock.NewMockMsg(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	var calls atomic.Int64

	sub.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		// Two failures cost initialBackoff + 2*initialBackoff before recovery.
		if calls.Add(1) <= 2 {
			return nil, errors.New("some transient error")
		}

		time.Sleep(time.Millisecond)

		return msgMock, nil
	}).AnyTimes()
	sub.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	var handled atomic.Int64

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			handled.Add(1)

			return nil
		})
	}()

	waitFor(t, func() bool { return handled.Load() >= 2 },
		"no messages delivered after transient errors — reader did not recover")

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after cancel")
	}
}

func TestProcess_ResubscribesOnConnectionClosed(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	dead := mock.NewMockSubscription(ctrl)
	fresh := mock.NewMockSubscription(ctrl)
	msgMock := mock.NewMockMsg(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().Meter().Return(nil).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	gomock.InOrder(
		cl.EXPECT().QueueSubscribeSync("subj", "q").Return(dead, nil),
		cl.EXPECT().QueueSubscribeSync("subj", "q").Return(fresh, nil),
	)

	dead.EXPECT().NextMsg(gomock.Any()).Return(nil, errConnectionClosed()).AnyTimes()
	dead.EXPECT().Drain().Return(nil).AnyTimes()

	fresh.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		time.Sleep(time.Millisecond)

		return msgMock, nil
	}).AnyTimes()
	fresh.EXPECT().Drain().Return(nil).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	var handled atomic.Int64

	done := make(chan error, 1)
	go func() {
		done <- s.Process(ctx, 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
			handled.Add(1)

			return nil
		})
	}()

	waitFor(t, func() bool { return handled.Load() > 0 },
		"no messages delivered — subscription was not re-established")

	testutils.Equal(t, s.Sub(), fresh)
	testutils.Equal(t, s.Alive(), true)

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after cancel")
	}
}

// TestProcess_FailsWhenConnectionGoneForGood is the one case that legitimately
// ends the reader: the connection itself is closed, so no retry can recover.
// It must surface as an error rather than the silent nil it used to return.
func TestProcess_FailsWhenConnectionGoneForGood(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().Meter().Return(nil).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	gomock.InOrder(
		cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil),
		cl.EXPECT().QueueSubscribeSync("subj", "q").Return(nil, errConnectionClosed()).AnyTimes(),
	)

	sub.EXPECT().NextMsg(gomock.Any()).Return(nil, errConnectionClosed()).AnyTimes()
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
		testutils.Equal(t, errors.Is(err, ErrSubscriptionFailed), true)
		testutils.Equal(t, s.Alive(), false)
	case <-time.After(5 * time.Second):
		t.Fatal("Process did not exit after the connection was closed for good")
	}
}

func TestProcess_TerminalErrorsEndCleanly(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "draining",
			err:  fmt.Errorf("%w: nats: connection draining", ErrDraining),
		},
		{
			name: "max messages",
			err:  fmt.Errorf("%w: nats: maximum messages delivered", ErrMaxMessages),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cl := mock.NewMockClient(ctrl)
			cfg := mock.NewMockConfig(ctrl)
			sub := mock.NewMockSubscription(ctrl)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			cl.EXPECT().Context().Return(ctx).AnyTimes()
			cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
			cl.EXPECT().Meter().Return(nil).AnyTimes()
			cl.EXPECT().Config().Return(cfg).AnyTimes()
			cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
			cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

			sub.EXPECT().NextMsg(gomock.Any()).Return(nil, tc.err).AnyTimes()
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
				testutils.Equal(t, err, nil)
			case <-time.After(5 * time.Second):
				t.Fatal("Process did not exit on a terminal error")
			}
		})
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
	cl.EXPECT().Meter().Return(nil).AnyTimes()
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

func TestProcess_MetricsErrorDoesNotStopProcessing(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(&recordingMetrics{err: errors.New("record failed")}).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	sub.EXPECT().Pending().Return(int64(5), int64(100), nil)
	sub.EXPECT().Dropped().Return(int64(1), nil)
	sub.EXPECT().Delivered().Return(int64(50), nil)
	sub.EXPECT().Subject().Return("test.subject")
	sub.EXPECT().NextMsg(gomock.Any()).Return(nil,
		fmt.Errorf("%w: nats: connection draining", ErrDraining)).AnyTimes()

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	err = s.Process(context.Background(), 1, 50*time.Millisecond, func(_ context.Context, _ mq.Msg) error {
		return nil
	})
	testutils.Equal(t, err, nil)
}

func TestProcess_StopDuringProcess(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Meter().Return(nil).AnyTimes()
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()

	sub.EXPECT().NextMsg(gomock.Any()).DoAndReturn(func(_ time.Duration) (mq.Msg, error) {
		time.Sleep(10 * time.Millisecond)

		return nil, errTimeout()
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

// TestStop_PreventsResubscribe guards the shutdown path: draining makes the
// subscription unreadable, which must not be mistaken for a fault to recover
// from. QueueSubscribeSync is expected exactly once (the initial subscribe), so
// any resubscribe attempt fails this test.
func TestStop_PreventsResubscribe(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	sub := mock.NewMockSubscription(ctrl)

	cl.EXPECT().Context().Return(context.Background()).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(sub, nil)
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	sub.EXPECT().Drain().Return(nil)

	s, err := NewSubscription(cl, "subj", "q")
	testutils.Equal(t, err, nil)

	err = s.Stop()
	testutils.Equal(t, err, nil)

	testutils.Equal(t, s.resubscribe(context.Background()), nil)
	testutils.Equal(t, s.Sub(), sub)
}

func TestNextBackoff(t *testing.T) {
	cases := []struct {
		name string
		in   time.Duration
		want time.Duration
	}{
		{
			name: "doubles",
			in:   initialBackoff,
			want: 2 * initialBackoff,
		},
		{
			name: "caps at maxBackoff",
			in:   maxBackoff,
			want: maxBackoff,
		},
		{
			name: "does not overshoot the cap",
			in:   maxBackoff - time.Second,
			want: maxBackoff,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutils.Equal(t, nextBackoff(tc.in), tc.want)
		})
	}
}
