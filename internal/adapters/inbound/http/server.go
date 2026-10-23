package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/claudioed/node-topology/internal/application/usecases"
	"github.com/claudioed/node-topology/internal/application/views"
	"github.com/claudioed/node-topology/internal/domain/lane"
	"github.com/claudioed/node-topology/internal/domain/node"
	"github.com/claudioed/node-topology/internal/domain/region"
	"github.com/claudioed/node-topology/internal/domain/servicearea"
)

// The use-case contracts the handlers call. *usecases.X satisfy them; the
// handler tests substitute use cases over in-memory doubles.
type (
	registerNode interface {
		Execute(context.Context, usecases.RegisterNodeCommand) (views.Node, error)
	}
	declareProfile interface {
		Execute(context.Context, usecases.DeclareOperationalProfileCommand) (views.Node, error)
	}
	nodeTransition interface {
		Execute(context.Context, node.ID) (views.Node, error)
	}
	suspendNode interface {
		Execute(context.Context, node.ID, string) (views.Node, error)
	}
	getNode interface {
		Execute(context.Context, node.ID) (views.Node, error)
	}
	listNodes interface {
		Execute(context.Context, usecases.ListNodesQuery) ([]views.Node, error)
	}
	openLane interface {
		Execute(context.Context, usecases.OpenLaneCommand) (views.Lane, error)
	}
	closeLane interface {
		Execute(context.Context, lane.ID) (views.Lane, error)
	}
	listLanes interface {
		Execute(context.Context, usecases.ListLanesQuery) ([]views.Lane, error)
	}
	assignServiceArea interface {
		Execute(context.Context, usecases.AssignServiceAreaCommand) (views.ServiceArea, error)
	}
	resolveServiceArea interface {
		Execute(context.Context, servicearea.PostalPrefix) (views.ServiceArea, error)
	}
	defineRegion interface {
		Execute(context.Context, usecases.DefineRegionCommand) (views.Region, error)
	}
	getRegion interface {
		Execute(context.Context, region.ID) (views.Region, error)
	}
	listRegions interface {
		Execute(context.Context) ([]views.Region, error)
	}
)

// Server holds everything the router needs; cmd/api fills every field.
type Server struct {
	RegisterNode       registerNode
	DeclareProfile     declareProfile
	ActivateNode       nodeTransition
	SuspendNode        suspendNode
	RetireNode         nodeTransition
	GetNode            getNode
	ListNodes          listNodes
	OpenLane           openLane
	CloseLane          closeLane
	ListLanes          listLanes
	AssignServiceArea  assignServiceArea
	ResolveServiceArea resolveServiceArea
	DefineRegion       defineRegion
	GetRegion          getRegion
	ListRegions        listRegions

	// Idempotency guards every state-changing route; it is mandatory.
	Idempotency Idempotency
	// Readiness backs /readyz; the zero gate is ready with no dependency.
	Readiness *Readiness
	// Logger receives the access log; slog.Default when nil.
	Logger *slog.Logger
}

// ErrNoIdempotency is returned by NewRouter when the Server has no
// Idempotency middleware: there is no unkeyed mode.
var ErrNoIdempotency = errors.New("http server: Idempotency is required (every state-changing route takes an Idempotency-Key)")

// NewRouter builds the chi router. Order of the middleware, outermost first:
// access log, recover (a panic is a 500 problem the log still sees), rejectNUL
// (before anything is recorded), then the route; on a state-changing route the Idempotency-Key
// middleware wraps the handler.
func NewRouter(s *Server) (http.Handler, error) {
	if s.Idempotency == nil {
		return nil, ErrNoIdempotency
	}
	r := chi.NewRouter()
	r.Use(accessLog(s.Logger), recoverer, rejectNUL)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, routeNotFound, "no route matches "+r.Method+" "+r.URL.EscapedPath())
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, methodNotAllowed, r.Method+" is not allowed on "+r.URL.EscapedPath())
	})

	r.Get("/healthz", handleHealthz)
	r.Get("/readyz", readyz(s.Readiness))

	s.mountNodes(r)
	s.mountLanes(r)
	s.mountServiceAreas(r)
	s.mountRegions(r)
	return r, nil
}

// keyed registers a state-changing route behind the Idempotency-Key
// middleware. The route string ("POST /nodes") is the idempotency scope.
func (s *Server) keyed(r chi.Router, method, pattern string, h http.HandlerFunc) {
	r.With(s.Idempotency(method+" "+pattern)).MethodFunc(method, pattern, h)
}

// recoverer turns a panic into a 500 problem and logs it.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler { //nolint:errorlint // the sentinel is compared by identity on purpose
					panic(p)
				}
				slog.ErrorContext(r.Context(), "panic in handler", "panic", p, "method", r.Method, "path", r.URL.Path)
				writeProblem(w, r, internalProblem, "an unexpected error occurred")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// accessLog logs one line per request with its status and duration.
func accessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK
			}
			logger.LogAttrs(r.Context(), levelFor(r.URL.Path, status), "request",
				slog.String("method", r.Method), slog.String("path", r.URL.EscapedPath()),
				slog.Int("status", status), slog.Duration("duration", time.Since(start)))
		})
	}
}

// levelFor keeps the probes quiet and flags server failures.
func levelFor(path string, status int) slog.Level {
	switch {
	case status >= http.StatusInternalServerError:
		return slog.LevelError
	case path == "/healthz" || path == "/readyz":
		return slog.LevelDebug
	default:
		return slog.LevelInfo
	}
}
