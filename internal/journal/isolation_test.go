package journal

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageImportsNothingFromReport asserts, by parsing this package's own source files,
// that none of them import internal/report. The dependency is one-way by design: the
// journal is kern-orch's own vocabulary, and internal/report.StepEvent stays a projection
// of it (built in a later issue) rather than the other way around — see the package doc.
// Parsing the files directly, rather than trusting go/build's import graph, is what catches
// the import even if some future refactor made it compile through an intermediate package.
func TestPackageImportsNothingFromReport(t *testing.T) {
	const forbidden = "github.com/yoann/kern-orch/internal/report"

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir(.): %v", err)
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("ParseFile(%s): %v", path, err)
		}
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if importPath == forbidden {
				t.Fatalf("%s imports %s, which this package must never depend on", path, forbidden)
			}
		}
	}
}
