package subscriber

import (
	"context"
	"errors"
	"time"

	"github.com/FrogoAI/multiproc/worker"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
)

const (
	maximumBufferSize    = 1024
	maxConsecutiveErrors = 100

	poolSizeValidate = 100 * time.Millisecond
	idleTimeout      = 30 * time.Second
)

type Subscription struct {
	mq.Client

	wpool          *worker.WorkersPool[mq.Msg]
	ctx            context.Context
	cancel         func()
	sub            mq.Subscription
	subject, queue string
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

	// Read messages
	go func() {
		defer close(ch)

		consecutiveErrors := 0

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.ctx.Done():
				return
			default:
				msg, err := s.sub.NextMsg(timeout)
				if err != nil {
					if errors.Is(err, ErrConnectionClosed) {
						s.Client.Logger().Debug("Connection closed",
							"err", err,
							"subject", s.subject,
							"queue", s.queue,
						)

						return
					}

					consecutiveErrors++
					if consecutiveErrors >= maxConsecutiveErrors {
						s.Client.Logger().Error("Too many consecutive errors, stopping reader",
							"err", err,
							"subject", s.subject,
							"queue", s.queue,
						)

						return
					}

					s.Client.Logger().Debug("Error getting next message with timeout",
						"err", err,
						"subject", s.subject,
						"queue", s.queue,
					)

					continue
				}

				consecutiveErrors = 0

				select {
				case ch <- msg:
				case <-ctx.Done():
					return
				case <-s.ctx.Done():
					return
				}
			}
		}
	}()

	go func() {
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
	}()

	// Setup default buffer
	for i := 0; i < buffer; i++ {
		s.wpool.Execute(func(ctx context.Context) error {
			return s.wpool.PersistentWorker(ctx, ch, handler)
		})
	}

	err = s.wpool.Wait()

	// Ensure reader goroutine stops when worker pool exits
	s.cancel()

	return err
}

func (s *Subscription) Sub() mq.Subscription {
	return s.sub
}

func (s *Subscription) Stop() error {
	err := s.sub.Drain()

	s.cancel()

	s.wpool.Stop()

	return err
}
