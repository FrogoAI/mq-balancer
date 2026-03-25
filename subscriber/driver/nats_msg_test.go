package driver

import (
	"testing"

	"github.com/FrogoAI/testutils"
	"github.com/nats-io/nats.go"
)

func TestNATSMsg_Subject(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{Subject: "test.subject"}}
	testutils.Equal(t, msg.Subject(), "test.subject")
}

func TestNATSMsg_IsReply(t *testing.T) {
	cases := []struct {
		name  string
		reply string
		want  bool
	}{
		{name: "with reply", reply: "_INBOX.123", want: true},
		{name: "without reply", reply: "", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msg := &NATSMsg{Msg: &nats.Msg{Reply: tc.reply}}
			testutils.Equal(t, msg.IsReply(), tc.want)
		})
	}
}

func TestNATSMsg_ReplyTo(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{Reply: "_INBOX.456"}}
	testutils.Equal(t, msg.ReplyTo(), "_INBOX.456")
}

func TestNATSMsg_Data(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{Data: []byte("hello")}}
	testutils.Equal(t, string(msg.Data()), "hello")
}

func TestNATSMsg_Header(t *testing.T) {
	hdr := nats.Header{"Key": {"val1", "val2"}}
	msg := &NATSMsg{Msg: &nats.Msg{Header: hdr}}
	testutils.Equal(t, len(msg.Header()), 1)
	testutils.Equal(t, msg.Header()["Key"][0], "val1")
}

func TestNATSMsg_Header_Nil(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{}}
	testutils.Equal(t, msg.Header() == nil, true)
}

func TestNATSMsg_SetHeader(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{}}
	msg.SetHeader("Error", "something went wrong")
	testutils.Equal(t, msg.Msg.Header.Get("Error"), "something went wrong")
}

func TestNATSMsg_SetHeader_Existing(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{Header: nats.Header{"Existing": {"val"}}}}
	msg.SetHeader("New", "value")
	testutils.Equal(t, msg.Msg.Header.Get("Existing"), "val")
	testutils.Equal(t, msg.Msg.Header.Get("New"), "value")
}

func TestNATSMsg_Copy_DeepCopiesHeader(t *testing.T) {
	original := &NATSMsg{Msg: &nats.Msg{
		Subject: "orig",
		Data:    []byte("data"),
		Header:  nats.Header{"Key": {"val"}},
		Reply:   "_INBOX.1",
	}}

	copied := original.Copy("new.subject")

	// Mutate the copy
	copied.SetHeader("New", "added")

	// Original must not be affected
	testutils.Equal(t, original.Header()["New"] == nil, true)
	testutils.Equal(t, copied.Subject(), "new.subject")
	testutils.Equal(t, string(copied.Data()), "data")
}

func TestNATSMsg_Copy_DeepCopiesData(t *testing.T) {
	data := []byte("original")
	original := &NATSMsg{Msg: &nats.Msg{Data: data}}

	copied := original.Copy("subj")

	// Mutate the copy's data
	copyData := copied.Data()
	copyData[0] = 'X'

	// Original must not be affected
	testutils.Equal(t, string(original.Data()), "original")
}

func TestNATSMsg_Copy_NilHeader(t *testing.T) {
	original := &NATSMsg{Msg: &nats.Msg{
		Subject: "orig",
		Data:    []byte("data"),
	}}

	copied := original.Copy("new.subject")
	testutils.Equal(t, copied.Header() == nil, true)
}

func TestNATSMsg_Respond_NoConnection(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{}}
	err := msg.Respond([]byte("data"))
	testutils.Equal(t, err != nil, true)
}

func TestNATSMsg_RespondMsg_NoConnection(t *testing.T) {
	msg := &NATSMsg{Msg: &nats.Msg{}}
	reply := &NATSMsg{Msg: &nats.Msg{
		Data:    []byte("reply"),
		Subject: "reply.subject",
		Reply:   "_INBOX.1",
	}}
	err := msg.RespondMsg(reply)
	testutils.Equal(t, err != nil, true)
}
