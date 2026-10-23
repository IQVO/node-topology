package main

import (
	"context"
	"errors"
	"log/slog"

	outboundkafka "github.com/claudioed/node-topology/internal/adapters/outbound/kafka"
	outboxrelay "github.com/claudioed/node-topology/internal/adapters/outbound/outbox"
)

// worker is a started background loop and the channel closed once it
// returned.
type worker struct {
	name string
	done chan struct{}
}

// startOutboxRelay starts the always-on relay goroutine over the Kafka sink.
// Kafka is dialled lazily on the first send, never here, so a broker that is
// down at boot only makes passes fail until it is up. The returned stop
// function cancels the relay; the worker's done channel closes after it
// returned and the sink was closed.
func startOutboxRelay(store outboxrelay.Store, cfg config, logger *slog.Logger) (*worker, func()) {
	sink := outboundkafka.NewRelaySink(cfg.brokers)
	relay := outboxrelay.NewOutboxRelay(store, sink,
		outboxrelay.WithLogger(logger),
		outboxrelay.WithInterval(cfg.relayInterval),
		outboxrelay.WithMaxAttempts(cfg.maxAttempts),
	)
	ctx, stop := context.WithCancel(context.Background())
	w := &worker{name: "outbox-relay", done: make(chan struct{})}
	go func() {
		defer close(w.done)
		defer func() {
			if err := sink.Close(); err != nil {
				logger.Error("kafka relay sink close failed", "error", err)
			}
		}()
		logger.Info("outbox relay running", "interval", cfg.relayInterval, "max_attempts", cfg.maxAttempts, "topic", outboundkafka.TopicEvents)
		if err := relay.Run(ctx); !errors.Is(err, context.Canceled) {
			logger.Error("outbox relay stopped", "error", err)
		}
	}()
	return w, stop
}
