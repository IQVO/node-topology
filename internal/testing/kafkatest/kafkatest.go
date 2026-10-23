// Package kafkatest boots ONE real Kafka broker through testcontainers per
// test binary. It never reads KAFKA_BROKERS, never hardcodes a broker address
// and never skips: a missing Docker is a test failure (fleet rule,
// TestKafkaIntegrationTestsUseTestcontainers).
package kafkatest

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/testcontainers/testcontainers-go"
	tckafka "github.com/testcontainers/testcontainers-go/modules/kafka"
)

var (
	once      sync.Once
	container *tckafka.KafkaContainer
	brokers   []string
	startErr  error
)

// Brokers starts the shared broker on first use and returns its addresses.
func Brokers(t *testing.T) []string {
	t.Helper()
	once.Do(func() {
		ctx := context.Background()
		container, startErr = tckafka.Run(ctx, "confluentinc/confluent-local:7.6.1", tckafka.WithClusterID("node-topology-itest"))
		if startErr != nil {
			return
		}
		brokers, startErr = container.Brokers(ctx)
	})
	if startErr != nil {
		t.Fatalf("start kafka container: %v", startErr)
	}
	return brokers
}

// BrokerList is Brokers joined the way KAFKA_BROKERS is written.
func BrokerList(t *testing.T) string { return strings.Join(Brokers(t), ",") }

// Stop terminates the broker mid-test (the readiness subtest). Shutdown after
// it is a no-op.
func Stop(t *testing.T) {
	t.Helper()
	if container == nil {
		t.Fatal("kafkatest.Stop: the broker was never started")
	}
	if err := testcontainers.TerminateContainer(container); err != nil {
		t.Fatalf("stop kafka container: %v", err)
	}
	container = nil
}

// Message is a consumed Kafka message in neutral terms, so test files need not
// touch the Kafka client themselves.
type Message struct {
	Key     string
	Value   []byte
	Headers map[string]string
}

// ReadN reads exactly n messages from partition 0 of topic, from the start,
// and fails the test if they do not arrive within timeout. The topic may not
// exist yet: the reader keeps retrying until the producer has created it.
func ReadN(t *testing.T, topic string, n int, timeout time.Duration) []Message {
	t.Helper()
	reader := kafkago.NewReader(kafkago.ReaderConfig{Brokers: Brokers(t), Topic: topic, Partition: 0, MinBytes: 1, MaxBytes: 1 << 20, MaxWait: 250 * time.Millisecond})
	defer func() { _ = reader.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out := make([]Message, 0, n)
	for len(out) < n {
		m, err := reader.ReadMessage(ctx)
		if err != nil {
			t.Fatalf("read message %d of %d from %s: %v", len(out)+1, n, topic, err)
		}
		headers := make(map[string]string, len(m.Headers))
		for _, h := range m.Headers {
			headers[h.Key] = string(h.Value)
		}
		out = append(out, Message{Key: string(m.Key), Value: m.Value, Headers: headers})
	}
	return out
}

// Count returns how many messages partition 0 of topic holds right now (the
// high watermark), or 0 when the topic does not exist.
func Count(t *testing.T, topic string) int64 {
	t.Helper()
	conn, err := kafkago.DialLeader(context.Background(), "tcp", Brokers(t)[0], topic, 0)
	if err != nil {
		return 0
	}
	defer func() { _ = conn.Close() }()
	first, last, err := conn.ReadOffsets()
	if err != nil {
		t.Fatalf("read offsets of %s: %v", topic, err)
	}
	return last - first
}

// Shutdown terminates the shared broker; call it from TestMain.
func Shutdown() {
	if container != nil {
		_ = testcontainers.TerminateContainer(container)
	}
}
