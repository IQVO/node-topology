package kafka

import (
	"context"
	"errors"
	"testing"

	segmentio "github.com/segmentio/kafka-go"

	"github.com/claudioed/node-topology/internal/application/outbox"
)

type fakeWriter struct {
	got []segmentio.Message
	err error
}

func (f *fakeWriter) WriteMessages(_ context.Context, msgs ...segmentio.Message) error {
	f.got = append(f.got, msgs...)
	return f.err
}

func TestRelaySinkWritesEachMessageToItsTopic(t *testing.T) {
	fw := &fakeWriter{}
	sink := NewRelaySinkWithWriter(fw)
	err := sink.Send(context.Background(),
		outbox.Message{Topic: "a.events", Key: []byte("k1"), Value: []byte("v1"), Headers: []outbox.Header{{Key: "content-type", Value: "x"}}},
		outbox.Message{Topic: "a.analytics", Key: []byte("k2"), Value: []byte("v2")},
	)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(fw.got) != 2 || fw.got[0].Topic != "a.events" || string(fw.got[0].Key) != "k1" || string(fw.got[0].Value) != "v1" ||
		len(fw.got[0].Headers) != 1 || fw.got[0].Headers[0].Key != "content-type" || string(fw.got[0].Headers[0].Value) != "x" ||
		fw.got[1].Topic != "a.analytics" || len(fw.got[1].Headers) != 0 {
		t.Fatalf("written = %+v", fw.got)
	}
}

func TestRelaySinkNoMessagesIsANoop(t *testing.T) {
	fw := &fakeWriter{}
	if err := NewRelaySinkWithWriter(fw).Send(context.Background()); err != nil || len(fw.got) != 0 {
		t.Fatalf("err=%v written=%d", err, len(fw.got))
	}
}

func TestRelaySinkRefusesAMessageWithoutTopic(t *testing.T) {
	fw := &fakeWriter{}
	err := NewRelaySinkWithWriter(fw).Send(context.Background(), outbox.Message{EventType: "t"})
	if err == nil || len(fw.got) != 0 {
		t.Fatalf("err=%v written=%d, want an error and no write", err, len(fw.got))
	}
}

func TestRelaySinkWrapsWriterErrors(t *testing.T) {
	boom := errors.New("broker down")
	err := NewRelaySinkWithWriter(&fakeWriter{err: boom}).Send(context.Background(), outbox.Message{Topic: "t"})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap %v", err, boom)
	}
}

func TestRelaySinkClose(t *testing.T) {
	if err := NewRelaySinkWithWriter(&fakeWriter{}).Close(); err != nil {
		t.Fatalf("closing a fake writer: %v", err)
	}
	sink := NewRelaySink("127.0.0.1:1")
	if err := sink.Close(); err != nil {
		t.Fatalf("closing a real, never used writer: %v", err)
	}
}
