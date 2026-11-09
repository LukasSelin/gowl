package gocode

import "testing"

// A plan can write a signature as a person would, and the code is read through
// go/types, which writes it its own way. Both have to come out the same.
func TestNormalizeSignature(t *testing.T) {
	keepT := map[string]bool{"T": true}
	for _, c := range []struct {
		in, pkg string
		keep    map[string]bool
		want    string
	}{
		// Names go, at every depth, and grouped names repeat their type.
		{"Write(p []byte) (n int, err error)", "io", nil, "Write([]uint8) (int, error)"},
		{"Write([]uint8) (int, error)", "io", nil, "Write([]uint8) (int, error)"},
		{"Pair(a, b int)", "x", nil, "Pair(int, int)"},
		{"Each(f func(i int, s string) (ok bool))", "x", nil, "Each(func(int, string) bool)"},
		// A single unnamed result has no parentheses; none at all, nothing.
		{"String() string", "owl", nil, "String() string"},
		{"Close()", "x", nil, "Close()"},
		{"  Spaced ( a  int )  ( string ,error ) ", "x", nil, "Spaced(int) (string, error)"},
		// Variadic stays variadic.
		{"Log(format string, args ...any)", "x", nil, "Log(string, ...any)"},
		// interface{} is any; rune is int32.
		{"Put(v interface{})", "x", nil, "Put(any)"},
		{"Runes() []rune", "x", nil, "Runes() []int32"},
		// A bare name of the interface's own package is qualified, so a plan
		// can write IRI() IRI inside package owl; a type parameter is not.
		{"IRI() IRI", "owl", nil, "IRI() owl.IRI"},
		{"IRI() owl.IRI", "owl", nil, "IRI() owl.IRI"},
		{"Get(k string) (T, bool)", "cache", keepT, "Get(string) (T, bool)"},
		{"Items() []Pair[T]", "cache", keepT, "Items() []cache.Pair[T]"},
		// Composite types.
		{"Index() map[owl.Class][]*owl.Axiom", "x", nil, "Index() map[owl.Class][]*owl.Axiom"},
		{"Stream(in <-chan int, out chan<- string) chan (<-chan int)", "x", nil, "Stream(<-chan int, chan<- string) chan (<-chan int)"},
		{"Fixed() [4]byte", "x", nil, "Fixed() [4]uint8"},
		{"Opts() struct{ A int; B string }", "x", nil, "Opts() struct{A int; B string}"},
		{"seal()", "x", nil, "seal()"},
	} {
		got, err := normalizeSignature(c.in, c.pkg, c.keep)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q in %s = %q, want %q", c.in, c.pkg, got, c.want)
		}
	}
}

func TestNormalizeSignatureRefusesWhatIsNotOne(t *testing.T) {
	for _, in := range []string{"", "String", "String() string {", "(x int)", "1x()", "Do(x int"} {
		if got, err := normalizeSignature(in, "x", nil); err == nil {
			t.Errorf("%q = %q, want an error", in, got)
		}
	}
}

// The names a signature gocode wrote leaves bare are its type parameters.
func TestBareNames(t *testing.T) {
	got := bareNames("Get(string, func(k K) V) (V, bool)")
	if len(got) != 2 || !got["K"] || !got["V"] {
		t.Errorf("bareNames = %v, want K and V", got)
	}
	if got := bareNames("Write([]uint8) (int, error)"); len(got) != 0 {
		t.Errorf("bareNames = %v, want none", got)
	}
}
