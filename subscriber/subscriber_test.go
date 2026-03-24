package subscriber

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
	"github.com/FrogoAI/mq-balancer/subscriber/mq/mock"
	"github.com/FrogoAI/testutils"
	"go.uber.org/mock/gomock"
)

func TestConcurrentSize(t *testing.T) {
	cases := []struct {
		name string
		size int
		want int
	}{
		{
			name: "positive value returned as-is",
			size: 10,
			want: 10,
		},
		{
			name: "zero falls back to NumCPU",
			size: 0,
			want: runtime.NumCPU(),
		},
		{
			name: "negative falls back to NumCPU",
			size: -1,
			want: runtime.NumCPU(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			cfg := mock.NewMockConfig(ctrl)
			cfg.EXPECT().ConcurrentSize().Return(tc.size)

			got := ConcurrentSize(cfg)
			testutils.Equal(t, got, tc.want)
		})
	}
}

func TestNewSubscriber(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cl.EXPECT().Context().Return(context.Background())

	s := NewSubscriber(cl)

	testutils.Equal(t, s.Client != nil, true)
	testutils.Equal(t, s.Pool != nil, true)
	testutils.Equal(t, s.Subs() != nil, true)
	testutils.Equal(t, len(s.Subs().GetMap()), 0)
}

func TestSubscribeWithParameters_NilHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cl.EXPECT().Context().Return(context.Background())

	s := NewSubscriber(cl)
	s.SubscribeWithParameters(1, time.Second, "subj", "queue", nil)

	testutils.Equal(t, len(s.Subs().GetMap()), 0)
}

func TestSubscribeWithParameters_AfterClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cl.EXPECT().Context().Return(context.Background())

	s := NewSubscriber(cl)

	err := s.Close()
	testutils.Equal(t, err, nil)

	handler := func(_ context.Context, _ mq.Msg) error { return nil }
	s.SubscribeWithParameters(1, time.Second, "subj", "queue", handler)

	testutils.Equal(t, len(s.Subs().GetMap()), 0)
}

func TestGet_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cl.EXPECT().Context().Return(context.Background())

	s := NewSubscriber(cl)
	sub := s.Get("nonexistent", "queue")

	testutils.Equal(t, sub == nil, true)
}

func TestSubscribeWithParameters_ZeroBuffer(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)
	cfg := mock.NewMockConfig(ctrl)
	subMock := mock.NewMockSubscription(ctrl)

	ctx, cancel := context.WithCancel(context.Background())

	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(subMock, nil)
	cl.EXPECT().Meter().Return(nil)
	cl.EXPECT().Config().Return(cfg).AnyTimes()
	cl.EXPECT().Logger().Return(stubLogger{}).AnyTimes()
	cfg.EXPECT().MaxConcurrentSize().Return(uint64(10)).AnyTimes()
	subMock.EXPECT().NextMsg(gomock.Any()).Return(nil, errors.New("timeout")).AnyTimes()
	subMock.EXPECT().Drain().Return(nil).AnyTimes()

	s := NewSubscriber(cl)

	// buffer=0 should be corrected to 1
	s.SubscribeWithParameters(0, 50*time.Millisecond, "subj", "q", func(_ context.Context, _ mq.Msg) error {
		return nil
	})

	time.Sleep(100 * time.Millisecond)
	cancel()

	err := s.Close()
	testutils.Equal(t, err, nil)

	s.ForceClose()
}

func TestSubscribeWithParameters_SubscribeError(t *testing.T) {
	ctrl := gomock.NewController(t)
	cl := mock.NewMockClient(ctrl)

	ctx := context.Background()
	cl.EXPECT().Context().Return(ctx).AnyTimes()
	cl.EXPECT().QueueSubscribeSync("subj", "q").Return(nil, errors.New("subscribe failed"))

	s := NewSubscriber(cl)

	s.SubscribeWithParameters(1, 50*time.Millisecond, "subj", "q", func(_ context.Context, _ mq.Msg) error {
		return nil
	})

	err := s.Wait()
	testutils.Equal(t, err != nil, true)
}
