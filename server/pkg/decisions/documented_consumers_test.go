package decisions

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This package's doc names the callers that send workspace content to the
// decision endpoint, and promises this test holds the list. The promise is
// made mechanically from the start because pkg/llm made the same one in a
// comment and nowhere else: its list sat at five entries while twenty-eight
// files called it, and the operator-facing copy written from that list told
// administrators this deployment sent a fifth of what it sent.
//
// The walk mirrors pkg/llm's equivalent test rather than sharing code with
// it. Two public packages, each answering "who reaches me?" about itself,
// should not need a third package to do it — and a shared helper would make
// one package's contract depend on another's test tree.
var facadeMethods = map[string]bool{"Ask": true}

var consumerPathPattern = regexp.MustCompile(`server/[\w./-]+\.go`)

func documentedConsumers(t *testing.T) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "client.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse client.go: %v", err)
	}
	if file.Doc == nil {
		t.Fatal("client.go has no package doc; the Consumers list lives there")
	}
	doc := file.Doc.Text()
	const heading = "Consumers, and what they send upstream"
	idx := strings.Index(doc, heading)
	if idx < 0 {
		t.Fatalf("package doc no longer has a %q section", heading)
	}
	out := map[string]bool{}
	for _, m := range consumerPathPattern.FindAllString(doc[idx:], -1) {
		out[m] = true
	}
	if len(out) == 0 {
		t.Fatal("the Consumers section lists no file paths")
	}
	return out
}

func callerFiles(t *testing.T) map[string]bool {
	t.Helper()
	serverRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve server root: %v", err)
	}
	selfDir := filepath.Join(serverRoot, "pkg", "decisions")
	out := map[string]bool{}
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(serverRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if path == selfDir || base == "vendor" || base == "testdata" ||
				strings.HasPrefix(base, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !facadeMethods[sel.Sel.Name] {
				return true
			}
			// "Ask" is a plausible method name elsewhere, so the receiver has
			// to look like this client. Checking the file imports this package
			// keeps the over-approximation to files that could reach it at all.
			if !importsThisPackage(file) {
				return true
			}
			rel, rerr := filepath.Rel(serverRoot, path)
			if rerr != nil {
				return true
			}
			out["server/"+filepath.ToSlash(rel)] = true
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", serverRoot, walkErr)
	}
	return out
}

func importsThisPackage(file *ast.File) bool {
	for _, spec := range file.Imports {
		if strings.HasSuffix(strings.Trim(spec.Path.Value, `"`), "/pkg/decisions") {
			return true
		}
	}
	return false
}

func TestDocumentedDecisionConsumersAreTheOnlyCallers(t *testing.T) {
	documented := documentedConsumers(t)
	callers := callerFiles(t)

	var undocumented, stale []string
	for path := range callers {
		if !documented[path] {
			undocumented = append(undocumented, path)
		}
	}
	for path := range documented {
		if !callers[path] {
			stale = append(stale, path)
		}
	}
	sort.Strings(undocumented)
	sort.Strings(stale)

	if len(undocumented) > 0 {
		t.Errorf("%d file(s) ask this client for a decision without an entry "+
			"in the Consumers section of client.go's package doc:\n  %s\n"+
			"Add one entry per path saying what state it sends and what it "+
			"asks. An operator reads that list to know what leaves the "+
			"deployment.", len(undocumented), strings.Join(undocumented, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d path(s) are documented as consumers but no longer ask "+
			"this client:\n  %s\nRemove the entry, or fix the path if the file "+
			"moved.", len(stale), strings.Join(stale, "\n  "))
	}
}
