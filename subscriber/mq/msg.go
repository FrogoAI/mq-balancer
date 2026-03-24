//go:generate mockgen -package mock -source=msg.go -destination=mock/msg.go
package mq

import "context"

type Msg interface {
	Subject() string
	IsReply() bool
	ReplyTo() string
	Copy(subject string) Msg
	SetHeader(key, value string)
	Respond([]byte) error
	Header() map[string][]string
	Data() []byte
	RespondMsg(Msg) error
}

type MsgHandler func(ctx context.Context, msg Msg) error
