package subscriber

import (
	"context"
	"errors"
	"testing"

	"github.com/FrogoAI/mq-balancer/subscriber/mq"
	"github.com/FrogoAI/mq-balancer/subscriber/mq/mock"
	"github.com/FrogoAI/testutils"
	"go.uber.org/mock/gomock"
)

func TestWithResponseOnError(t *testing.T) {
	cases := []struct {
		name        string
		handlerErr  error
		isReply     bool
		respondErr  error
		wantErr     error
		expectReply bool
	}{
		{
			name:       "handler success no response sent",
			handlerErr: nil,
			isReply:    true,
			wantErr:    nil,
		},
		{
			name:        "handler error with reply sends error response",
			handlerErr:  errors.New("processing failed"),
			isReply:     true,
			wantErr:     errors.New("processing failed"),
			expectReply: true,
		},
		{
			name:       "handler error without reply does not respond",
			handlerErr: errors.New("processing failed"),
			isReply:    false,
			wantErr:    errors.New("processing failed"),
		},
		{
			name:        "handler error with reply and respond failure",
			handlerErr:  errors.New("processing failed"),
			isReply:     true,
			respondErr:  errors.New("respond failed"),
			wantErr:     errors.New("processing failed"),
			expectReply: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)

			msg := mock.NewMockMsg(ctrl)
			copyMsg := mock.NewMockMsg(ctrl)

			handler := func(_ context.Context, _ mq.Msg) error {
				return tc.handlerErr
			}

			wrapped := WithResponseOnError(nil, handler)

			if tc.handlerErr != nil {
				msg.EXPECT().IsReply().Return(tc.isReply)
			}

			if tc.expectReply {
				msg.EXPECT().ReplyTo().Return("reply.subject")
				msg.EXPECT().Copy("reply.subject").Return(copyMsg)
				copyMsg.EXPECT().SetHeader(HeaderConsumerError, tc.handlerErr.Error())
				msg.EXPECT().RespondMsg(copyMsg).Return(tc.respondErr)
			}

			err := wrapped(context.Background(), msg)

			if tc.wantErr != nil {
				testutils.Equal(t, err.Error(), tc.wantErr.Error())
			} else {
				testutils.Equal(t, err, nil)
			}
		})
	}
}

func TestWithResponseOnError_NilLogger(t *testing.T) {
	ctrl := gomock.NewController(t)
	msg := mock.NewMockMsg(ctrl)

	handler := func(_ context.Context, _ mq.Msg) error {
		return nil
	}

	wrapped := WithResponseOnError(nil, handler)
	err := wrapped(context.Background(), msg)
	testutils.Equal(t, err, nil)
}

func TestStubLogger(t *testing.T) {
	l := stubLogger{}
	l.Error("test")
	l.Info("test")
	l.Debug("test")
}
