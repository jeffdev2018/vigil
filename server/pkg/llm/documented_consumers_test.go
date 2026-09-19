package llm

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

// The package doc promises this test by name: "TestDocumentedConsumersAreThe
// OnlyCallers fails until a new call site is reflected here". It did not
// exist. The list it guards had drifted to 5 entries against 26 real caller
// files, and the prose still said "All three consumers" — while naming five.
//
// That drift is not cosmetic. The package doc is, by its own words, "the
// source the operator-facing copy is written from (.env.example and
// apps/docs/content/docs/environment-variables*.mdx)". An operator whose
// policy forbids sending workspace content to a third party reads that list
// to decide whether this deployment does. A list covering a fifth of the
// call sites answers that question wrongly, and the two omissions found when
// this test was first run were not minor: the Brain's note embeddings
// (server/internal/service/brain_embedding.go) send note text to the
// embeddings vendor, while the doc claimed repo indexing was "the only
// consumer of the /embeddings surface".
//
// So the guarantee has to be mechanical. A comment that says a test holds it
// is worth nothing when the test is absent — that is a declared control, not
// a kept one.
//
// Detection is deliberately syntactic: any call to one of this façade's
// method names, anywhere under server/ outside this package, counts as a
// consumer. That over-approximates — a same-named method on an unrelated type
// would be caught too — and the trade is intentional. A false positive costs
// one line in the list (or an entry in unrelatedCallers below, with the
// reason); a false negative is an undocumented path to a third party, which
// is the failure this test exists to prevent.
var facadeMethods = map[string]bool{
	"Chat":                  true,
	"ChatStream":            true,
	"GenerateText":          true,
	"GenerateJSON":          true,
	"GenerateJSONWithUsage": true,
	"Embed":                 true,
}

// unrelatedCallers are files whose match is a same-named method on another
// type, proven case by case. Empty today: every match under server/ is a real
// consumer of this client. Add an entry only with the reason it is not one.
var unrelatedCallers = map[string]string{}

var consumerPathPattern = regexp.MustCompile(`server/[\w./-]+\.go`)

// documentedConsumers reads the paths listed in the package doc's Consumers
// section. It reads the doc comment rather than a Go slice on purpose: the
// prose is what an operator and the docs writer actually read, so the prose
// is what must be kept true.
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

// callerFiles walks the server tree and returns every file outside this
// package that calls one of facadeMethods.
func callerFiles(t *testing.T) map[string]bool {
	t.Helper()
	// Absolute, so the walk's own path segments are never "..", which the
	// hidden-directory rule below would otherwise skip — starting with the
	// root itself.
	serverRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve server root: %v", err)
	}
	selfDir := filepath.Join(serverRoot, "pkg", "llm")
	out := map[string]bool{}
	fset := token.NewFileSet()

	walkErr := filepath.WalkDir(serverRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// This package defines the methods; vendored and generated trees
			// are not consumers anyone documents.
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
			// A tree that does not parse is a build problem, not this test's.
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
			rel, rerr := filepath.Rel(serverRoot, path)
			if rerr != nil {
				return true
			}
			key := "server/" + filepath.ToSlash(rel)
			if _, skip := unrelatedCallers[key]; !skip {
				out[key] = true
			}
			return true
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", serverRoot, walkErr)
	}
	if len(out) == 0 {
		t.Fatal("found no callers at all; the detection is broken, not the tree")
	}
	return out
}

func TestDocumentedConsumersAreTheOnlyCallers(t *testing.T) {
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
		t.Errorf("%d file(s) reach this client without an entry in the "+
			"Consumers section of client.go's package doc:\n  %s\n"+
			"Add one entry per path saying what it sends upstream and with "+
			"what bound. That list is what the operator-facing copy in "+
			".env.example and the environment-variables docs is written "+
			"from, so an omission there tells an operator this deployment "+
			"sends less than it does.",
			len(undocumented), strings.Join(undocumented, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("%d path(s) are documented as consumers but no longer call "+
			"this client:\n  %s\nRemove the entry, or fix the path if the "+
			"file moved. A list that names a path nobody calls is as "+
			"misleading as one that omits a path somebody does.",
			len(stale), strings.Join(stale, "\n  "))
	}
}
