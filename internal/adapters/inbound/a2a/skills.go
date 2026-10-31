package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/claudioed/node-topology/internal/application/ports"
	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
	"github.com/claudioed/node-topology/internal/domain/shared"
)

// tracerName is the OTel instrumentation scope for A2A skill spans, mirroring
// the MCP adapter's mcp.tool spans.
const tracerName = "github.com/claudioed/node-topology/internal/adapters/inbound/a2a"

// Skill ids advertised on the Agent Card (see Card).
const (
	skillServingStation = "serving_station"
	skillFindNodes      = "find_nodes"
)

// skillRequest is the wire contract of every skill call: the first message
// part carries (as a Data part, or a Text part holding the same JSON) the skill
// id and its arguments. Strict on purpose: an agent client is machine to
// machine, so unknown fields are rejected, never ignored, and a misspelled
// filter like "regionID" must not silently widen a query to every region. (Go's
// encoding/json matches field names case-insensitively, so "regionid" still
// reaches regionId; only genuinely unknown names are refused.)
type skillRequest struct {
	Skill     string          `json:"skill"`
	Arguments json.RawMessage `json:"arguments"`
}

// servingStationArgs and findNodesArgs are the per-skill argument shapes.
type servingStationArgs struct {
	PostalPrefix string `json:"postalPrefix"`
}

type findNodesArgs struct {
	Type     string `json:"type"`
	RegionID string `json:"regionId"`
	Status   string `json:"status"`
}

// answer maps one incoming message to a skill and returns the agent's reply
// Message. It is the whole executor body: a message-only agent, one request
// mapped to one answer.
func answer(ctx context.Context, deps Deps, ec *a2asrv.ExecutorContext) a2a.Event {
	req, problem := parseSkillRequest(ec)
	if problem != nil {
		return reply(ec, envelope{Problem: problem}, problem.summary())
	}

	ctx, span := otel.Tracer(tracerName).Start(ctx, "a2a.skill "+req.Skill)
	defer span.End()
	span.SetAttributes(attribute.String("a2a.skill", req.Skill))

	dto, problem := run(ctx, deps, req)
	if problem != nil {
		span.SetStatus(codes.Error, problem.Detail)
		return reply(ec, envelope{Skill: req.Skill, Problem: problem}, problem.summary())
	}
	return reply(ec, envelope{Skill: req.Skill, Data: dto}, dto.summary)
}

// run dispatches one validated request to its skill.
func run(ctx context.Context, deps Deps, req skillRequest) (skillDTO, *problem) {
	switch req.Skill {
	case skillServingStation:
		data, problem := deps.servingStation(ctx, req.Arguments)
		if problem != nil {
			return skillDTO{}, problem
		}
		return skillDTO{ServingStation: &data, summary: data.summary()}, nil
	case skillFindNodes:
		data, problem := deps.findNodes(ctx, req.Arguments)
		if problem != nil {
			return skillDTO{}, problem
		}
		return skillDTO{Nodes: &data, summary: data.summary()}, nil
	default:
		return skillDTO{}, problemUnknownSkill(req.Skill)
	}
}

// reply builds the agent Message for one skill run: a Data part carrying the
// structured envelope and a Text part carrying the one-line summary, bound to
// the caller's context (and task, when the client referenced one).
func reply(ec *a2asrv.ExecutorContext, env envelope, summary string) a2a.Event {
	return a2a.NewMessageForTask(a2a.MessageRoleAgent, ec, a2a.NewDataPart(env), a2a.NewTextPart(summary))
}

// parseSkillRequest extracts the skillRequest from the message's first part. A
// Data part (or a Text part holding the same JSON object) is accepted;
// anything else is a request-shape problem, answered, not raised: a malformed
// delegation is a business answer for the calling agent, not a transport
// failure.
func parseSkillRequest(ec *a2asrv.ExecutorContext) (skillRequest, *problem) {
	msg := ec.Message
	if msg == nil || len(msg.Parts) == 0 {
		return skillRequest{}, problemMalformedRequest("the message carries no parts")
	}
	raw, err := partJSON(msg.Parts[0])
	if err != nil {
		return skillRequest{}, problemMalformedRequest(fmt.Sprintf("the first message part is not a skill request: %v", err))
	}
	var req skillRequest
	if err := decodeStrict(raw, &req); err != nil {
		return skillRequest{}, problemMalformedRequest(fmt.Sprintf("the skill request is not valid: %v", err))
	}
	if req.Skill == "" {
		return skillRequest{}, problemMalformedRequest(`the skill request carries no "skill" field`)
	}
	return req, nil
}

// partJSON renders one part's content as the JSON bytes of a skill request.
func partJSON(part *a2a.Part) ([]byte, error) {
	switch content := part.Content.(type) {
	case a2a.Data:
		return json.Marshal(content.Value)
	case a2a.Text:
		return []byte(string(content)), nil
	default:
		return nil, errors.New("expected a data part (or a text part holding the skill JSON)")
	}
}

// decodeStrict decodes one JSON document into dst, rejecting unknown fields
// and trailing data. Absent arguments decode as the empty object so each
// skill's own required-field checks produce the specific problem.
func decodeStrict(raw []byte, dst any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = []byte("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after the JSON document")
	}
	return nil
}

// rejectNUL refuses a U+0000 in any caller-supplied string: Postgres cannot
// store or compare it, and the REST adapter answers 400 for it too, so the
// agent gets the same answer instead of an internal error.
func rejectNUL(fields map[string]string) *problem {
	for name, value := range fields {
		if strings.ContainsRune(value, 0) {
			return problemInvalidArgument(name + " must not contain a NUL character (U+0000)")
		}
	}
	return nil
}

// servingStation is the composed read skill: the service area of one postal
// prefix and the station node it points at, from this context's own state.
func (d Deps) servingStation(ctx context.Context, rawArgs json.RawMessage) (servingStation, *problem) {
	var args servingStationArgs
	if err := decodeStrict(rawArgs, &args); err != nil {
		return servingStation{}, problemInvalidArgument(fmt.Sprintf("arguments: %v", err))
	}
	if strings.TrimSpace(args.PostalPrefix) == "" {
		return servingStation{}, problemInvalidArgument("postalPrefix is required")
	}
	if p := rejectNUL(map[string]string{"postalPrefix": args.PostalPrefix}); p != nil {
		return servingStation{}, p
	}
	area, err := d.ResolveServiceArea.Execute(ctx, servicearea.PostalPrefix(args.PostalPrefix))
	if err != nil {
		return servingStation{}, problemFromUseCase(err)
	}
	station, err := d.GetNode.Execute(ctx, node.ID(area.DeliveryStationNodeID))
	if err != nil {
		return servingStation{}, problemFromUseCase(err)
	}
	return toServingStation(area, station), nil
}

// findNodes is the filtered node listing skill, over the same use case as
// GET /nodes: every filter is optional and the set ones combine with AND.
func (d Deps) findNodes(ctx context.Context, rawArgs json.RawMessage) (nodeList, *problem) {
	var args findNodesArgs
	if err := decodeStrict(rawArgs, &args); err != nil {
		return nodeList{}, problemInvalidArgument(fmt.Sprintf("arguments: %v", err))
	}
	if p := rejectNUL(map[string]string{"type": args.Type, "regionId": args.RegionID, "status": args.Status}); p != nil {
		return nodeList{}, p
	}
	nodes, err := d.ListNodes.Execute(ctx, usecases.ListNodesQuery{
		Type: node.Type(args.Type), RegionID: region.ID(args.RegionID), Status: node.Status(args.Status),
	})
	if err != nil {
		return nodeList{}, problemFromUseCase(err)
	}
	out := make([]nodeView, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, toNode(n, false))
	}
	return nodeList{Count: len(out), Nodes: out}, nil
}

// problemFromUseCase maps a use case error to a problem answer, using the
// SAME classification as the REST adapter (internal/adapters/inbound/http
// problemCatalogue) so a delegating agent sees the outcome a REST caller
// would. Only the errors a READ can raise are listed (as in the MCP adapter):
// a repository miss and value validation of the filters. Anything else is an
// internal failure, still answered as a problem (500) whose detail never
// leaks the internal message; it is logged instead.
func problemFromUseCase(err error) *problem {
	switch {
	case errors.Is(err, ports.ErrNotFound):
		return &problem{Type: "not-found", Title: "Resource not found", Status: 404, Detail: err.Error()}
	case errors.Is(err, shared.ErrValidation):
		return &problem{Type: "validation-failed", Title: "Validation failed", Status: 422, Detail: err.Error()}
	default:
		slog.Error("a2a skill failed", "error", err)
		return &problem{Type: "internal-error", Title: "Internal server error", Status: 500, Detail: "an unexpected error occurred"}
	}
}
