package subscriber

import (
	"context"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
)

const (
	HeaderConsumerError = "Error"
)

// WithResponseOnError wraps handler where if handler returns a non-nil error,
// the msg is then responded to with said error's string in the
// HeaderConsumerError header. Such message then causes nats publisher to return
// the error when reading response.
//
// Note: this wrapper should NOT be used for observer-like handlers that do not
// send success responses. If subscribed to the same subject with an actual
// responder, the latter's response can potentially get snubbed.
func WithResponseOnError(logger mq.Logger, handler mq.MsgHandler) mq.MsgHandler {
	if logger == nil {
		logger = stubLogger{}
	}

	return func(ctx context.Context, msg mq.Msg) error {
		err := handler(ctx, msg)
		if err != nil && msg.IsReply() {
			responseMsg := msg.Copy(msg.ReplyTo())
			responseMsg.SetHeader(HeaderConsumerError, err.Error())

			if respErr := msg.RespondMsg(responseMsg); respErr != nil {
				logger.Error("Message error response failed", "respErr", respErr, "originalErr", err)
			}
		}

		return err
	}
}

type stubLogger struct{}

func (l stubLogger) Error(_ string, _ ...any) {}
func (l stubLogger) Info(_ string, _ ...any)  {}
func (l stubLogger) Debug(_ string, _ ...any) {}
