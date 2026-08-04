package subscriber

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/FrogoAI/multiproc/worker"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
)

const (
	maximumBufferSize = 1024

	poolSizeValidate = 100 * time.Millisecond
	idleTimeout      = 30 * time.Second

	initialBackoff      = 100 * time.Millisecond
	maxBackoff          = 30 * time.Second
	readerShutdownGrace = 5 * time.Second
)

type Subscription struct {
	mq.Client

	wpool          *worker.WorkersPool[mq.Msg]
	ctx            context.Context
	cancel         func()
	subject, queue string

	// mu guards sub, which resubscribe swaps out while the metrics goroutine
	// reads it.
	mu  sync.RWMutex
	sub mq.Subscription

	alive    atomic.Bool
	stopping atomic.Bool
}

func NewSubscription(c mq.Client, subject, queue string) (*Subscription, error) {
	sub, err := c.QueueSubscribeSync(subject, queue)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(c.Context())

	return &Subscription{
		ctx:     ctx,
		cancel:  cancel,
		Client:  c,
		sub:     sub,
		subject: subject,
		queue:   queue,
		wpool:   worker.NewWorkersPool[mq.Msg](c.Context()),
	}, nil
}

func (s *Subscription) Process(
	ctx context.Context,
	buffer int,
	timeout time.Duration,
	handler mq.MsgHandler,
) error {
	err := s.setupMetrics()
	if err != nil {
		return err
	}

	ch := make(chan mq.Msg, maximumBufferSize)

	readerErr := make(chan error, 1)

	go func() {
		defer close(ch)

		readerErr <- s.readLoop(ctx, timeout, ch)
	}()

	go s.scalePool(ctx, ch, handler)

	// Setup default buffer
	for i := 0; i < buffer; i++ {
		s.wpool.Execute(func(ctx context.Context) error {
			return s.wpool.PersistentWorker(ctx, ch, handler)
		})
	}

	err = s.wpool.Wait()

	// Ensure reader goroutine stops when worker pool exits
	s.cancel()

	return errors.Join(err, s.waitForReader(readerErr))
}

// waitForReader collects the reader's outcome. The reader can still be parked
// inside NextMsg for up to one read timeout after the cancel above, so the wait
// is bounded rather than indefinite.
func (s *Subscription) waitForReader(readerErr <-chan error) error {
	select {
	case err := <-readerErr:
		return err
	case <-time.After(readerShutdownGrace):
		return fmt.Errorf("%w: reader did not stop within %s", ErrSubscriptionFailed, readerShutdownGrace)
	}
}

// readLoop pulls messages off the subscription until one of the contexts is
// cancelled or the subscription ends for good.
//
// Only a genuinely unrecoverable fault ends the loop. Everything transient is
// retried, because returning here closes ch, which retires the worker pool and
// silently takes the whole subscription down with it.
func (s *Subscription) readLoop(ctx context.Context, timeout time.Duration, ch chan<- mq.Msg) error {
	s.alive.Store(true)
	defer s.alive.Store(false)

	backoff := initialBackoff

	for {
		if s.done(ctx) {
			return nil
		}

		msg, err := s.Sub().NextMsg(timeout)

		switch {
		case err == nil:
			backoff = initialBackoff

			select {
			case ch <- msg:
			case <-ctx.Done():
				return nil
			case <-s.ctx.Done():
				return nil
			}

		case errors.Is(err, ErrTimeout):
			// No message within the poll window. This is the idle path, not a
			// failure: keep polling, with no penalty and no delay.
			backoff = initialBackoff

		case errors.Is(err, ErrSlowConsumer):
			// Messages were dropped because the client buffer overflowed. The
			// subscription itself is still valid, so keep reading.
			s.countReaderError(reasonSlowConsumer)
			s.Client.Logger().Error("Slow consumer, messages dropped",
				"err", err,
				"subject", s.subject,
				"queue", s.queue,
			)

		case errors.Is(err, ErrDraining), errors.Is(err, ErrMaxMessages):
			s.Client.Logger().Info("Reader finished",
				"err", err,
				"subject", s.subject,
				"queue", s.queue,
			)

			return nil

		case errors.Is(err, ErrConnectionClosed):
			s.countReaderError(reasonConnectionClosed)

			if err := s.resubscribe(ctx); err != nil {
				return err
			}

			backoff = initialBackoff

		default:
			s.countReaderError(reasonUnknown)
			s.Client.Logger().Error("Next message failed, retrying",
				"err", err,
				"backoff", backoff,
				"subject", s.subject,
				"queue", s.queue,
			)

			if !s.sleep(ctx, backoff) {
				return nil
			}

			backoff = nextBackoff(backoff)
		}
	}
}

// resubscribe re-establishes the subscription after it became unreadable. It
// keeps retrying transient failures, and only gives up when the underlying
// connection is closed for good — nothing can recover from that.
func (s *Subscription) resubscribe(ctx context.Context) error {
	backoff := initialBackoff

	for {
		// A drain triggered by Stop surfaces as an unreadable subscription too.
		// Recovering from a deliberate shutdown would resurrect it.
		if s.stopping.Load() || s.done(ctx) {
			return nil
		}

		sub, err := s.Client.QueueSubscribeSync(s.subject, s.queue)
		if err == nil {
			s.setSub(sub)
			s.countResubscribe()
			s.Client.Logger().Info("Resubscribed", "subject", s.subject, "queue", s.queue)

			return nil
		}

		if errors.Is(err, ErrConnectionClosed) {
			return fmt.Errorf("%w: resubscribe %s/%s: %w", ErrSubscriptionFailed, s.subject, s.queue, err)
		}

		s.Client.Logger().Error("Resubscribe failed, retrying",
			"err", err,
			"backoff", backoff,
			"subject", s.subject,
			"queue", s.queue,
		)

		if !s.sleep(ctx, backoff) {
			return nil
		}

		backoff = nextBackoff(backoff)
	}
}

// scalePool grows the worker pool while messages are queueing up.
func (s *Subscription) scalePool(ctx context.Context, ch chan mq.Msg, handler mq.MsgHandler) {
	t := time.NewTicker(poolSizeValidate)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ctx.Done():
			return
		case <-t.C:
			if len(ch) > 0 && s.wpool.Size() < s.Config().MaxConcurrentSize() {
				s.Client.Logger().Debug("Increase pool size", "pending", len(ch), "wpool", s.wpool.Size())
				s.wpool.Execute(func(ctx context.Context) error {
					return s.wpool.TemporalWorker(ctx, idleTimeout, func() {
						s.Client.Logger().Debug("Decrease pool size", "wpool", s.wpool.Size()-1)
					}, ch, handler)
				})
			}
		}
	}
}

// done reports whether either the caller's context or the subscription's own
// context has been cancelled.
func (s *Subscription) done(ctx context.Context) bool {
	return ctx.Err() != nil || s.ctx.Err() != nil
}

// sleep waits for d, reporting false if it was cut short by a cancellation.
func (s *Subscription) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	case <-s.ctx.Done():
		return false
	}
}

func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > maxBackoff {
		return maxBackoff
	}

	return d
}

// Alive reports whether the reader goroutine is currently running. Callers can
// poll it to supervise a subscription without waiting for Process to return.
func (s *Subscription) Alive() bool {
	return s.alive.Load()
}

func (s *Subscription) Sub() mq.Subscription {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.sub
}

func (s *Subscription) setSub(sub mq.Subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sub = sub
}

func (s *Subscription) Stop() error {
	s.stopping.Store(true)

	err := s.Sub().Drain()

	s.cancel()

	s.wpool.Stop()

	return err
}
