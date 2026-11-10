package gocode

import (
	"strings"
	"testing"
)

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
		// A function may have type parameters of its own. Each gets its
		// constraint, and none of them is taken for a type of the package.
		{"Map[T any](xs []T, f func(T) T) []T", "x", nil, "Map[T any]([]T, func(T) T) []T"},
		{"Keys[K comparable, V any](m map[K]V) []K", "x", nil, "Keys[K comparable, V any](map[K]V) []K"},
		{"Pair[K, V any](k K, v V) Entry[K, V]", "x", nil, "Pair[K any, V any](K, V) x.Entry[K, V]"},
		{"Sum[N ~int | ~float64](ns ...N) N", "x", nil, "Sum[N ~int | ~float64](...N) N"},
		{"Show[T fmt.Stringer](t T) string", "x", nil, "Show[T fmt.Stringer](T) string"},
		{"Sort[T Ordered](ts []T)", "x", nil, "Sort[T x.Ordered]([]T)"},
		// The type parameters of the type a method is on stay bare beside them.
		{"Each[U any](f func(T) U) []U", "cache", keepT, "Each[U any](func(T) U) []U"},
		// An interface written out is the same interface whatever order its
		// members come in, so they are sorted: methods first, then what it
		// embeds. go/types writes the code's the same way round.
		{"Reporter(r interface{ PushStep(PathStep); Report(Result); PopStep() }) Option", "cmp", nil,
			"Reporter(interface{PopStep(); PushStep(cmp.PathStep); Report(cmp.Result)}) cmp.Option"},
		{"Do(x interface{ io.Reader; String() string; comparable })", "x", nil, "Do(interface{String() string; comparable; io.Reader})"},
		// A constraint that is one interface around one thing is that thing.
		{"Sum[N interface{ ~int | ~float64 }](ns ...N) N", "x", nil, "Sum[N ~int | ~float64](...N) N"},
		{"Show[T interface{ fmt.Stringer }](t T) string", "x", nil, "Show[T fmt.Stringer](T) string"},
		{"Show[T interface{ String() string }](t T) string", "x", nil, "Show[T interface{String() string}](T) string"},
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
	for _, in := range []string{
		"", "String", "String() string {", "(x int)", "1x()", "Do(x int",
		// A signature is read as a declaration without a body, and it has to
		// be exactly that: no body, no receiver, and only one.
		"Do() {}", "(r T) Do()", "A(); func B()", "Do[T any]", "Do[](x int)",
	} {
		if got, err := normalizeSignature(in, "x", nil); err == nil {
			t.Errorf("%q = %q, want an error", in, got)
		}
	}
}

// What is wrong with a signature is said in a few words: no position in
// source the reader never wrote, and not the signature again, which whoever
// reports the error quotes already.
func TestMalformedSignatureSaysWhy(t *testing.T) {
	for _, in := range []string{"Do(x int", "Do", "Do() {}", "A(); func B()", "Do(x ...)"} {
		_, err := normalizeSignature(in, "x", nil)
		if err == nil {
			t.Errorf("%q: want an error", in)
			continue
		}
		why := err.Error()
		if why == "" || strings.ContainsAny(why[:1], "0123456789:") || strings.Contains(why, in) {
			t.Errorf("%q: error = %s\nwant a reason alone", in, why)
		}
	}
}

// What a signature calls the type parameters it declares is the writer's own,
// as what it calls its parameters is: two signatures that differ in nothing
// else are the same.
func TestSameSignature(t *testing.T) {
	for _, c := range []struct {
		a, b string
		same bool
	}{
		{"Price() int", "Price() int", true},
		{"Map[T any, U any]([]T, func(T) U) []U", "Map[A any, B any]([]A, func(A) B) []B", true},
		{"Pair[K comparable, V any](K, V) cache.Entry[K, V]", "Pair[A comparable, B any](A, B) cache.Entry[A, B]", true},
		{"Each[U any](func(T) U) []U", "Each[V any](func(T) V) []V", true},
		// Which parameter goes where is not the writer's own.
		{"Map[T any, U any]([]T, func(T) U) []U", "Map[T any, U any]([]U, func(T) U) []T", false},
		{"Pair[K comparable, V any](K, V)", "Pair[K any, V comparable](K, V)", false},
		{"Price() int", "Price() int64", false},
		{"Price() int", "Cost() int", false},
		// Nor are the parameters of the type a method is on: the method does
		// not declare them, so nothing says which is which but their names,
		// and a swap has to show.
		{"Get(K) (V, bool)", "Get(Key) (Val, bool)", false},
		{"Put(K, V)", "Put(V, K)", false},
		{"Each[U any](func(T) U) []U", "Each[V any](func(Elem) V) []V", false},
		{"Get(K) cache.V", "Get(K) V", false},
	} {
		if got := sameSignature(c.a, c.b); got != c.same {
			t.Errorf("sameSignature(%q, %q) = %v, want %v", c.a, c.b, got, c.same)
		}
		if got := sameSignature(c.b, c.a); got != c.same {
			t.Errorf("sameSignature(%q, %q) = %v, want %v", c.b, c.a, got, c.same)
		}
	}
}

func TestMethodName(t *testing.T) {
	for in, want := range map[string]string{
		"Price(store.Item) int": "Price",
		" Price (x int)":        "Price",
		"Map[T any]([]T) []T":   "Map",
		"Close()":               "Close",
		"NotASignature":         "NotASignature",
	} {
		if got := methodName(in); got != want {
			t.Errorf("methodName(%q) = %q, want %q", in, got, want)
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
