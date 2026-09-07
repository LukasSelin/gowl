package el_test

import (
	"fmt"
	"strings"
	"testing"

	"gowl/el"
	"gowl/owl"
)

// benchOntology builds a balanced hierarchy of n classes with existential
// restrictions threaded across it, which is the shape that makes saturation do
// real work rather than just walking a tree.
func benchOntology(b *testing.B, n int) *owl.Ontology {
	b.Helper()
	var sb strings.Builder
	sb.WriteString("Prefix(:=<http://example.org/o#>)\nOntology(<http://example.org/o>\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, "  SubClassOf(:C%d :C%d)\n", i, i/2)
		if i%20 == 0 {
			fmt.Fprintf(&sb, "  SubClassOf(:C%d ObjectSomeValuesFrom(:part :C%d))\n", i, i/3)
			fmt.Fprintf(&sb, "  SubClassOf(ObjectSomeValuesFrom(:part :C%d) :P%d)\n", i/3, i)
		}
	}
	sb.WriteString("  TransitiveObjectProperty(:part)\n)\n")

	o, err := owl.ParseFunctionalString(sb.String())
	if err != nil {
		b.Fatal(err)
	}
	return o
}

func BenchmarkClassify(b *testing.B) {
	for _, n := range []int{500, 2000, 8000} {
		o := benchOntology(b, n)
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				el.Classify(o)
			}
		})
	}
}

// BenchmarkClassifyWithoutExplanations shows what recording provenance costs,
// which is the number to look at when deciding whether to turn it off.
func BenchmarkClassifyWithoutExplanations(b *testing.B) {
	o := benchOntology(b, 2000)
	b.ReportAllocs()
	for b.Loop() {
		el.Classify(o, el.WithoutExplanations())
	}
}

func BenchmarkInferredAxioms(b *testing.B) {
	c := el.Classify(benchOntology(b, 2000))
	b.ReportAllocs()
	for b.Loop() {
		c.InferredAxioms()
	}
}
