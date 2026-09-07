package owl_test

import (
	"fmt"
	"strconv"
	"testing"

	"gowl/owl"
)

// GenerateOntology builds a synthetic ontology with nClasses named classes
// arranged in a branching hierarchy, nProps object properties, and one
// individual per leaf. It exists so benchmarks have something corpus-shaped to
// chew on without vendoring a real ontology.
func GenerateOntology(nClasses, nProps int) *owl.Ontology {
	o := owl.New("http://example.org/bench")
	o.Prefix("", "http://example.org/bench#")

	classes := make([]owl.Class, nClasses)
	for i := range classes {
		classes[i] = o.Class(":C" + strconv.Itoa(i))
	}
	props := make([]owl.ObjectProperty, nProps)
	for i := range props {
		props[i] = o.ObjectProperty(":p" + strconv.Itoa(i))
		o.DefineObjectProperty(props[i]).Label("property " + strconv.Itoa(i))
	}

	for i, c := range classes {
		b := o.Define(c).Label("class " + strconv.Itoa(i))
		if i > 0 {
			// A branching factor of 3 gives a hierarchy of realistic depth
			// rather than a single chain.
			b.SubClassOf(classes[(i-1)/3])
		}
		if nProps > 0 {
			p := props[i%nProps]
			b.SubClassOf(owl.Some(p, classes[(i+1)%nClasses]))
		}
		if i%10 == 0 {
			ind := o.Individual(":i" + strconv.Itoa(i))
			o.DefineIndividual(ind).Type(c)
		}
	}
	return o
}

// benchSizes keeps the benchmark grid small enough to run quickly while still
// showing how each operation scales.
var benchSizes = []int{100, 1000, 5000}

func BenchmarkAncestorsOf(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		deepest := o.Class(":C" + strconv.Itoa(n-1))
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.AncestorsOf(deepest)
			}
		})
	}
}

func BenchmarkDescendantsOf(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		root := o.Class(":C0")
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.DescendantsOf(root)
			}
		})
	}
}

func BenchmarkSuperClassesOf(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		c := o.Class(":C" + strconv.Itoa(n/2))
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.SuperClassesOf(c)
			}
		})
	}
}

func BenchmarkSignature(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.Signature()
			}
		})
	}
}

func BenchmarkClasses(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.Classes()
			}
		})
	}
}

func BenchmarkAxiomsReferencing(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		c := o.Class(":C" + strconv.Itoa(n/2))
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.AxiomsReferencing(c)
			}
		})
	}
}

func BenchmarkLabel(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		c := o.Class(":C" + strconv.Itoa(n-1))
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.Label(c)
			}
		})
	}
}

func BenchmarkParseFunctional(b *testing.B) {
	for _, n := range benchSizes {
		src := GenerateOntology(n, 10).Functional()
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			b.SetBytes(int64(len(src)))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := owl.ParseFunctionalString(src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkRender(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			o.Index()
			b.ReportAllocs()
			for b.Loop() {
				_ = o.Functional()
			}
		})
	}
}

func BenchmarkDiff(b *testing.B) {
	for _, n := range benchSizes {
		from := GenerateOntology(n, 10)
		to := GenerateOntology(n, 10)
		to.Add(owl.SubClassOf{Sub: to.Class(":C1"), Super: to.Class(":C99")})
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = owl.DiffOntologies(from, to)
			}
		})
	}
}

// BenchmarkNewIndex measures index construction, the one-time cost the query
// benchmarks deliberately exclude by warming the index first.
func BenchmarkNewIndex(b *testing.B) {
	for _, n := range benchSizes {
		o := GenerateOntology(n, 10)
		b.Run(fmt.Sprintf("classes=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = owl.NewIndex(o)
			}
		})
	}
}
