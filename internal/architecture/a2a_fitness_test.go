package architecture

// Fitness sensors of the A2A inbound adapter (docs/adr/0011-a2a-inbound-adapter.md).
// The adapter is additive and READ-ONLY, like the MCP one (ADR 0008):
//
//   - TestA2AAdapterDependencyRule: the adapter depends only on the application
//     and domain layers (never on an outbound adapter, the REST adapter or
//     cmd), and nothing else in internal/ depends on it.
//   - TestA2ASDKIsImportedByTheAdapterAndItsBinaryOnly: the SDK is imported by
//     internal/adapters/inbound/a2a and cmd/a2a (non-test files) and nowhere
//     else.
//   - TestNoOutboundA2AClient: no non-test Go file imports the SDK's client
//     package. A2A is an INBOUND surface here: this context calls no sibling
//     (docs/adr/0003), so an outbound A2A client would be a new dependency
//     direction that needs its own ADR.
//   - TestA2ADepsHoldReadUseCasesOnly: every field of the adapter's Deps is a
//     read use case (Get*, List*, Resolve*): the write gate. Delegation must
//     never widen what the context exposes, and the MCP charter has no write.
//   - TestA2ASensorBites proves the detectors flag what they must.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	archgo "github.com/arch-go/arch-go/api"
	"github.com/arch-go/arch-go/api/configuration"
)

const (
	a2aAdapterDir = "../adapters/inbound/a2a"
	a2aSDKPrefix  = "github.com/a2aproject/a2a-go"
	a2aClientPath = "github.com/a2aproject/a2a-go/v2/a2aclient"
)

func TestA2AAdapterDependencyRule(t *testing.T) {
	moduleInfo := configuration.Load(modulePath)

	t.Run("a2a adapter depends only on application and domain", func(t *testing.T) {
		assertArchGoPasses(t, archgo.CheckArchitecture(moduleInfo, configuration.Config{
			DependenciesRules: []*configuration.DependenciesRule{{
				Package: "**.internal.adapters.inbound.a2a.**",
				ShouldOnlyDependsOn: &configuration.Dependencies{Internal: []string{
					"**.internal.adapters.inbound.a2a.**",
					"**.internal.application.**",
					"**.internal.domain.**",
				}},
			}},
		}))
	})

	t.Run("nothing else depends on the a2a adapter", func(t *testing.T) {
		assertArchGoPasses(t, archgo.CheckArchitecture(moduleInfo, configuration.Config{
			DependenciesRules: []*configuration.DependenciesRule{{
				Package: "**.internal.**",
				ShouldNotDependsOn: &configuration.Dependencies{Internal: []string{
					"**.internal.adapters.inbound.a2a.**",
				}},
			}},
		}))
	})
}

// importPaths parses src and returns its import paths.
func importPaths(t *testing.T, filename string, src any) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse %s: %v", filename, err)
	}
	var out []string
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			t.Fatalf("%s: bad import %s", filename, imp.Path.Value)
		}
		out = append(out, path)
	}
	return out
}

// importersOf returns the files (non-test) among files that import a path for
// which match is true.
func importersOf(t *testing.T, files []string, match func(string) bool) []string {
	t.Helper()
	var out []string
	for _, file := range files {
		for _, path := range importPaths(t, file, nil) {
			if match(path) {
				out = append(out, filepath.ToSlash(file)+" imports "+path)
			}
		}
	}
	return out
}

func isA2ASDK(path string) bool { return strings.HasPrefix(path, a2aSDKPrefix) }

func isA2AClient(path string) bool { return path == a2aClientPath }

// allowedSDKImporter reports whether a "file imports path" finding is in one
// of the two places allowed to import the SDK.
func allowedSDKImporter(finding string) bool {
	return strings.HasPrefix(finding, "../adapters/inbound/a2a/") || strings.HasPrefix(finding, "../../cmd/a2a/")
}

func TestA2ASDKIsImportedByTheAdapterAndItsBinaryOnly(t *testing.T) {
	files := append(goFilesUnder(t, "..", false), goFilesUnder(t, "../../cmd", false)...)
	for _, finding := range importersOf(t, files, isA2ASDK) {
		if !allowedSDKImporter(finding) {
			t.Errorf("%s: the A2A SDK belongs to internal/adapters/inbound/a2a and cmd/a2a only (docs/adr/0011)", finding)
		}
	}
}

func TestNoOutboundA2AClient(t *testing.T) {
	files := append(goFilesUnder(t, "..", false), goFilesUnder(t, "../../cmd", false)...)
	if len(files) == 0 {
		t.Fatal("no Go files scanned: the sensor is looking in the wrong place")
	}
	for _, finding := range importersOf(t, files, isA2AClient) {
		t.Errorf("%s: node-topology calls no sibling over A2A; an outbound client needs its own ADR (docs/adr/0011)", finding)
	}
}

// depsFieldViolations returns one finding per field of the Deps struct in src
// whose type is not a read use case (a *usecases.Get*, List* or Resolve*).
func depsFieldViolations(filename string, src any) ([]string, error) {
	f, err := parser.ParseFile(token.NewFileSet(), filename, src, 0)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, decl := range f.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		out = append(out, depsViolationsOf(gen)...)
	}
	return out, nil
}

func depsViolationsOf(gen *ast.GenDecl) []string {
	var out []string
	for _, spec := range gen.Specs {
		ts, ok := spec.(*ast.TypeSpec)
		if !ok || ts.Name.Name != "Deps" {
			continue
		}
		st, ok := ts.Type.(*ast.StructType)
		if !ok {
			continue
		}
		for _, field := range st.Fields.List {
			if name := useCaseTypeName(field.Type); !isReadUseCase(name) {
				out = append(out, "Deps field of type "+name+" is not a read use case")
			}
		}
	}
	return out
}

// useCaseTypeName renders the type of a Deps field: "usecases.X" for a
// *usecases.X, the best-effort source form for anything else.
func useCaseTypeName(expr ast.Expr) string {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "<not a *usecases.X>"
	}
	pkg, _ := sel.X.(*ast.Ident)
	if pkg == nil || pkg.Name != "usecases" {
		return "<not a *usecases.X>"
	}
	return sel.Sel.Name
}

func isReadUseCase(name string) bool {
	for _, prefix := range []string{"Get", "List", "Resolve"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func TestA2ADepsHoldReadUseCasesOnly(t *testing.T) {
	var seen bool
	for _, file := range goFilesUnder(t, a2aAdapterDir, false) {
		got, err := depsFieldViolations(file, nil)
		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}
		for _, v := range got {
			t.Errorf("%s: %s; the A2A surface is read-only (docs/adr/0011, MCP charter ADR 0008)", file, v)
		}
		if strings.HasSuffix(file, "server.go") {
			seen = true
		}
	}
	if !seen {
		t.Fatal("internal/adapters/inbound/a2a/server.go not found: the sensor is looking in the wrong place")
	}
}

func TestA2ASensorBites(t *testing.T) {
	const writeDeps = `package a2a
import "github.com/claudioed/node-topology/internal/application/usecases"
type Deps struct {
	GetNode *usecases.GetNode
	RegisterNode *usecases.RegisterNode
	Other int
}`
	got, err := depsFieldViolations("deps.go", writeDeps)
	if err != nil || len(got) != 2 {
		t.Fatalf("a write use case and a foreign type in Deps must both be flagged, got %v (%v)", got, err)
	}
	const readDeps = `package a2a
import "github.com/claudioed/node-topology/internal/application/usecases"
type Deps struct {
	GetNode *usecases.GetNode
	ListNodes *usecases.ListNodes
	ResolveServiceArea *usecases.ResolveServiceArea
}`
	if got, err := depsFieldViolations("deps.go", readDeps); err != nil || len(got) != 0 {
		t.Fatalf("read use cases must pass, got %v (%v)", got, err)
	}

	const client = `package x
import "github.com/a2aproject/a2a-go/v2/a2aclient"
var _ = a2aclient.NewFromCard`
	if got := importPaths(t, "x.go", client); len(got) != 1 || !isA2AClient(got[0]) || !isA2ASDK(got[0]) {
		t.Fatalf("the client import must be flagged by both detectors, got %v", got)
	}
	if isA2AClient("github.com/a2aproject/a2a-go/v2/a2asrv") || !isA2ASDK("github.com/a2aproject/a2a-go/v2/a2asrv") {
		t.Fatal("the server package is SDK but not the client")
	}
	if allowedSDKImporter("../adapters/inbound/http/x.go imports "+a2aSDKPrefix) || !allowedSDKImporter("../../cmd/a2a/main.go imports x") {
		t.Fatal("only the adapter and cmd/a2a may import the SDK")
	}
}
