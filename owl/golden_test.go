package owl_test

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gowl/owl"
)

var update = flag.Bool("update", false, "rewrite the .golden files in testdata")

// TestGoldenRoundTrip parses each corpus fixture, renders it, and compares the
// result against a stored golden file. The fixtures are loosely formatted on
// purpose, so this pins two things at once: that the parser tolerates real
// formatting, and that the renderer's output does not drift.
//
// Regenerate with: go test ./owl -run TestGoldenRoundTrip -update
func TestGoldenRoundTrip(t *testing.T) {
	sources, err := filepath.Glob("testdata/*.ofn")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) == 0 {
		t.Fatal("no fixtures in testdata")
	}

	for _, src := range sources {
		t.Run(filepath.Base(src), func(t *testing.T) {
			data, err := os.ReadFile(src)
			if err != nil {
				t.Fatal(err)
			}
			o, err := owl.ParseFunctionalString(string(data))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := o.Functional()

			goldenPath := strings.TrimSuffix(src, ".ofn") + ".golden"
			if *update {
				if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("rendering drifted from %s\n--- want ---\n%s\n--- got ---\n%s",
					goldenPath, want, got)
			}
		})
	}
}

// TestFixturesReparse checks that rendering is a fixed point: parsing our own
// output must give back the same document.
func TestFixturesReparse(t *testing.T) {
	sources, _ := filepath.Glob("testdata/*.ofn")
	for _, src := range sources {
		t.Run(filepath.Base(src), func(t *testing.T) {
			data, _ := os.ReadFile(src)
			first, err := owl.ParseFunctionalString(string(data))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			rendered := first.Functional()
			second, err := owl.ParseFunctionalString(rendered)
			if err != nil {
				t.Fatalf("re-parse: %v", err)
			}
			if got := second.Functional(); got != rendered {
				t.Errorf("rendering is not a fixed point\n--- first ---\n%s\n--- second ---\n%s", rendered, got)
			}
			if d := owl.DiffOntologies(first, second); !d.Empty() {
				t.Errorf("round trip changed the axioms:\n%s", d)
			}
		})
	}
}

// TestFixturesCanonicalIsStable checks that canonicalizing twice is the same as
// canonicalizing once.
func TestFixturesCanonicalIsIdempotent(t *testing.T) {
	sources, _ := filepath.Glob("testdata/*.ofn")
	for _, src := range sources {
		t.Run(filepath.Base(src), func(t *testing.T) {
			data, _ := os.ReadFile(src)
			o, err := owl.ParseFunctionalString(string(data))
			if err != nil {
				t.Fatal(err)
			}
			for ax := range o.All() {
				once := owl.CanonicalAxiom(ax)
				twice := owl.CanonicalAxiom(once)
				if owl.Functional(once) != owl.Functional(twice) {
					t.Errorf("canonicalization is not idempotent:\n once: %s\ntwice: %s",
						owl.Functional(once), owl.Functional(twice))
				}
			}
		})
	}
}
