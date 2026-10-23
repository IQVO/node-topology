---
name: how-to-add-an-integration-event
description: Publish a cross-service Kafka event from this service: CloudEvents 1.0 type naming, AsyncAPI, the type catalogue ADR, the encoder, the transactional outbox. Use when touching internal/adapters kafka or outbox code, a domain event, or apis/asyncapi*.yaml.
---

# How to add an integration event

node-topology publishes nine events and consumes none (ADR 0003, ADR 0004). The
envelope is CloudEvents 1.0 only; read `.claude/rules/integration-events.md` and
`.claude/rules/fleet/cloudevents.md` first. The worked example is
`LaneClosed`.

## Publishing a new event

1. **Domain event.** A typed struct in `internal/domain/<pkg>/events.go`
   (`lane.LaneClosed`): embeds `shared.Header`, fields exactly the model's `data`
   list, methods `EventName()`, `Entity()` (`node`, `lane`, `servicearea`, `region`,
   lowercase, no separators) and `SubjectID()` (the aggregate id), plus a
   `var _ shared.Event = ...` assertion. The aggregate command (`Lane.Close`)
   returns it; `Version()` after the command is the version it carries.
2. **Contract, one commit.** Add the message and its `data` schema to
   `apis/asyncapi.yaml`, AND one `type: com.retailnetwork.logistics.node-topology.<entity>.<Event> stream: events`
   line to `docs/adr/0004-cloudevents-envelope-and-type-catalogue.md`.
   `TestEventCatalogueMatchesContract` fails when the two disagree, so the push
   fails when they are not in the same commit. Lint:
   `npx --yes @stoplight/spectral-cli lint apis/asyncapi.yaml --ruleset .spectral.asyncapi.yaml`.
   Evolution is additive only; a breaking change is a new `:v2` dataschema.
3. **Encoder.** In `internal/adapters/outbound/kafka/payloads.go` add the payload
   struct (camelCase json tags, instants through `instant`, arrays never null) and
   in `encoder.go` a `case` in `payloadFor`. The envelope itself is built by
   `internal/adapters/kafka/cloudevents` only; never hand-build one. Add the event
   to `goldenCases()` in `encoder_test.go` (an exact-JSON test of every attribute
   and the payload) and bump the count in `TestEncoderCoversEveryPublishedEvent`.
4. **Publish from the use case** through `writer.publish` inside the same
   `ports.UnitOfWork` as the `Save` (`internal/application/usecases/writer.go`):
   the aggregate row and the outbox row commit or roll back together. Never
   publish after the commit and never call Kafka from a use case.
5. **Nothing else.** The relay (`internal/adapters/outbound/outbox/relay.go`)
   already drains every row; the topic is `retailnetwork.node-topology.events`
   (`kafka.TopicEvents`). An analytics-only event would need its own topic constant,
   stream and ADR.
6. **Prove it.** `go test -tags=integration ./internal/adapters/outbound/outbox/ -race -count=1`
   reads the events back from a real broker; extend `cmd/api/e2e_integration_test.go`
   when the event is part of a REST flow.

## Consuming an event from a sibling context

Not today: this context consumes nothing and calls no sibling. Adding a consumer
supersedes ADR 0003 first (new ADR), then follows `.claude/rules/fleet/kafka-testing-and-consumers.md`:
a consumer group id from a mandatory env var (boot error naming it, listed in
`docs/runtime.md` and the chart), an inbox table keyed by the CloudEvents `id`,
dispatch on the FULL `type`, tolerant decoding, a DLQ path that guards shutdown
(`TestDeadLetterCallsGuardCancellation`), adapters in a new inbound Kafka package under
`internal/adapters/inbound/`, and the vendored contract of the producer's AsyncAPI.

## Verify before opening the PR

`make check`, `go test ./internal/architecture/ -count=1` (catalogue, CloudEvents-only,
relay cap), the spectral lint of the AsyncAPI, and the integration command above.
