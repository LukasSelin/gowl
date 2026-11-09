package icv

import (
	"fmt"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"

	"gowl/owl"
)

// The tests in this file check properties that hold for every model, rather
// than the answer to one worked case. The reference they compare with is the
// semantics written out the slow, obvious way: each relation a set of pairs,
// the transitive one built by a search from every individual, and each class
// expression a set computed straight from its definition.

const (
	worlds    = 60
	perWorld  = 12
	maxDepth  = 3
	worldSize = 9
)

// world is a random finite model: individuals i0..in-1, classes A, B and C,
// and properties p and q. r is p and q followed any distance, which the
// validator derives from its rules and the reference by search.
type world struct {
	n       int
	members map[string]map[int]bool
	rel     map[string]map[[2]int]bool
}

const rules = `
	SubObjectPropertyOf(:p :r)
	SubObjectPropertyOf(:q :r)
	TransitiveObjectProperty(:r)
`

func newWorld(rng *rand.Rand) world {
	w := world{n: worldSize, members: map[string]map[int]bool{}, rel: map[string]map[[2]int]bool{}}
	for _, c := range []string{"A", "B", "C"} {
		w.members[c] = map[int]bool{}
		for i := 0; i < w.n; i++ {
			if rng.Intn(3) == 0 {
				w.members[c][i] = true
			}
		}
	}
	for _, p := range []string{"p", "q"} {
		w.rel[p] = map[[2]int]bool{}
		for i := 0; i < w.n; i++ {
			for j := 0; j < w.n; j++ {
				if i != j && rng.Intn(7) == 0 {
					w.rel[p][[2]int{i, j}] = true
				}
			}
		}
	}
	w.rel["r"] = map[[2]int]bool{}
	for x := 0; x < w.n; x++ {
		for y, d := range w.distances(x) {
			if d > 0 {
				w.rel["r"][[2]int{x, y}] = true
			}
		}
	}
	return w
}

// distances finds, by breadth-first search over p and q, how many steps each
// individual is from x; an unreached one is absent.
func (w world) distances(x int) map[int]int {
	dist := map[int]int{}
	frontier := []int{x}
	for d := 1; len(frontier) > 0; d++ {
		var next []int
		for _, a := range frontier {
			for b := 0; b < w.n; b++ {
				if (w.rel["p"][[2]int{a, b}] || w.rel["q"][[2]int{a, b}]) && dist[b] == 0 {
					if _, seen := dist[b]; !seen {
						dist[b] = d
						next = append(next, b)
					}
				}
			}
		}
		frontier = next
	}
	return dist
}

func (w world) data() string {
	var b strings.Builder
	for i := 0; i < w.n; i++ {
		fmt.Fprintf(&b, "ClassAssertion(:Ind :i%d)\n", i) // every individual is named
	}
	for _, c := range []string{"A", "B", "C"} {
		for i := 0; i < w.n; i++ {
			if w.members[c][i] {
				fmt.Fprintf(&b, "ClassAssertion(:%s :i%d)\n", c, i)
			}
		}
	}
	for _, p := range []string{"p", "q"} {
		for i := 0; i < w.n; i++ {
			for j := 0; j < w.n; j++ {
				if w.rel[p][[2]int{i, j}] {
					fmt.Fprintf(&b, "ObjectPropertyAssertion(:%s :i%d :i%d)\n", p, i, j)
				}
			}
		}
	}
	return b.String()
}

func index(i owl.Individual) int {
	_, local, _ := strings.Cut(string(i.(owl.NamedIndividual)), "#i")
	n, err := strconv.Atoi(local)
	if err != nil {
		panic(i)
	}
	return n
}

func local(e owl.Entity) string {
	_, l, _ := strings.Cut(string(e.IRI()), "#")
	return l
}

// pairs is the reference's reading of a property expression.
func (w world) pairs(p owl.ObjectPropertyExpression) map[[2]int]bool {
	switch q := p.(type) {
	case owl.ObjectProperty:
		return w.rel[local(q)]
	case owl.ObjectInverseOf:
		out := map[[2]int]bool{}
		for e := range w.rel[local(q.Property)] {
			out[[2]int{e[1], e[0]}] = true
		}
		return out
	}
	panic(p)
}

// ext is the reference's reading of a class expression.
func (w world) ext(ce owl.ClassExpression) map[int]bool {
	out := map[int]bool{}
	all := func(keep func(int) bool) map[int]bool {
		for i := 0; i < w.n; i++ {
			if keep(i) {
				out[i] = true
			}
		}
		return out
	}
	switch c := ce.(type) {
	case owl.Class:
		switch c {
		case owl.Thing:
			return all(func(int) bool { return true })
		case owl.Nothing:
			return out
		}
		return all(func(i int) bool { return w.members[local(c)][i] })
	case owl.ObjectIntersectionOf:
		return all(func(i int) bool {
			for _, op := range c {
				if !w.ext(op)[i] {
					return false
				}
			}
			return true
		})
	case owl.ObjectUnionOf:
		return all(func(i int) bool {
			for _, op := range c {
				if w.ext(op)[i] {
					return true
				}
			}
			return false
		})
	case owl.ObjectComplementOf:
		in := w.ext(c.Operand)
		return all(func(i int) bool { return !in[i] })
	case owl.ObjectOneOf:
		for _, i := range c {
			out[index(i)] = true
		}
		return out
	case owl.ObjectSomeValuesFrom:
		rel, in := w.pairs(c.Property), w.ext(c.Filler)
		return all(func(i int) bool {
			for j := 0; j < w.n; j++ {
				if rel[[2]int{i, j}] && in[j] {
					return true
				}
			}
			return false
		})
	case owl.ObjectAllValuesFrom:
		rel, in := w.pairs(c.Property), w.ext(c.Filler)
		return all(func(i int) bool {
			for j := 0; j < w.n; j++ {
				if rel[[2]int{i, j}] && !in[j] {
					return false
				}
			}
			return true
		})
	case owl.ObjectHasValue:
		rel, v := w.pairs(c.Property), index(c.Value)
		return all(func(i int) bool { return rel[[2]int{i, v}] })
	}
	panic(ce)
}

func randProperty(rng *rand.Rand) string {
	return []string{":p", ":q", ":r", "ObjectInverseOf(:p)", "ObjectInverseOf(:r)"}[rng.Intn(5)]
}

func randIndividual(rng *rand.Rand) string { return fmt.Sprintf(":i%d", rng.Intn(worldSize)) }

// randExpr is any class expression of the fragment.
func randExpr(rng *rand.Rand, depth int) string {
	if depth == 0 || rng.Intn(4) == 0 {
		switch rng.Intn(7) {
		case 0:
			return "owl:Thing"
		case 1:
			return "owl:Nothing"
		case 2:
			return fmt.Sprintf("ObjectOneOf(%s %s)", randIndividual(rng), randIndividual(rng))
		case 3:
			return fmt.Sprintf("ObjectHasValue(%s %s)", randProperty(rng), randIndividual(rng))
		}
		return []string{":A", ":B", ":C"}[rng.Intn(3)]
	}
	switch rng.Intn(5) {
	case 0:
		return fmt.Sprintf("ObjectIntersectionOf(%s %s)", randExpr(rng, depth-1), randExpr(rng, depth-1))
	case 1:
		return fmt.Sprintf("ObjectUnionOf(%s %s)", randExpr(rng, depth-1), randExpr(rng, depth-1))
	case 2:
		return fmt.Sprintf("ObjectComplementOf(%s)", randExpr(rng, depth-1))
	case 3:
		return fmt.Sprintf("ObjectSomeValuesFrom(%s %s)", randProperty(rng), randExpr(rng, depth-1))
	}
	return fmt.Sprintf("ObjectAllValuesFrom(%s %s)", randProperty(rng), randExpr(rng, depth-1))
}

// randAnchored is a class expression the fragment accepts as the subject of a
// check.
func randAnchored(rng *rand.Rand, depth int) string {
	if depth == 0 || rng.Intn(3) == 0 {
		switch rng.Intn(3) {
		case 0:
			return fmt.Sprintf("ObjectOneOf(%s %s %s)", randIndividual(rng), randIndividual(rng), randIndividual(rng))
		case 1:
			return fmt.Sprintf("ObjectHasValue(%s %s)", randProperty(rng), randIndividual(rng))
		}
		return []string{":A", ":B", ":C"}[rng.Intn(3)]
	}
	switch rng.Intn(3) {
	case 0:
		return fmt.Sprintf("ObjectIntersectionOf(%s %s)", randAnchored(rng, depth-1), randExpr(rng, depth-1))
	case 1:
		return fmt.Sprintf("ObjectUnionOf(%s %s)", randAnchored(rng, depth-1), randAnchored(rng, depth-1))
	}
	return fmt.Sprintf("ObjectSomeValuesFrom(%s %s)", randProperty(rng), randExpr(rng, depth-1))
}

// randChecks writes checks over anchored subjects: SubClassOf and
// DisjointClasses.
func randChecks(rng *rand.Rand) []string {
	var out []string
	for k := 0; k < perWorld; k++ {
		if rng.Intn(3) == 0 {
			out = append(out, fmt.Sprintf("DisjointClasses(%s %s)", randAnchored(rng, maxDepth), randAnchored(rng, maxDepth)))
		} else {
			out = append(out, fmt.Sprintf("SubClassOf(%s %s)", randAnchored(rng, maxDepth), randExpr(rng, maxDepth)))
		}
	}
	return out
}

func sortedKeys(m map[int]bool) []int {
	var out []int
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func violators(vs []Violation, ax owl.Axiom) []int {
	var out []int
	for _, v := range vs {
		if owl.CanonicalKey(v.Axiom) == owl.CanonicalKey(ax) {
			out = append(out, index(v.Subject))
		}
	}
	slices.Sort(out)
	return out
}

// checkPaths asserts that each path is a chain of facts the data asserts.
func checkPaths(t *testing.T, data *owl.Ontology, vs []Violation) {
	t.Helper()
	asserted := map[string]bool{}
	for ax := range data.All() {
		asserted[owl.CanonicalKey(ax)] = true
	}
	for _, v := range vs {
		for k, f := range v.Path {
			if !asserted[owl.CanonicalKey(f)] {
				t.Errorf("path of %v holds %v, which the data does not assert", v.Axiom, f)
			}
			if k > 0 && v.Path[k-1].Object != f.Subject {
				t.Errorf("path of %v is broken between %v and %v", v.Axiom, v.Path[k-1], f)
			}
		}
	}
}

// Every check the validator answers, it answers as the reference does, for
// every counterexample; and no anchored subject is refused.
func TestAgreesWithTheReference(t *testing.T) {
	violated, held := 0, 0
	for seed := int64(0); seed < worlds; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w := newWorld(rng)
		data := parse(t, w.data())
		checks := randChecks(rng)
		constraints := parse(t, rules+strings.Join(checks, "\n"))
		r := Validate(data, constraints)
		if len(r.Unsupported) != 0 {
			t.Fatalf("seed %d: refused an anchored check: %v", seed, r.Unsupported)
		}
		checkPaths(t, data, r.Violations)
		for ax := range constraints.All() {
			var want []int
			switch x := ax.(type) {
			case owl.SubClassOf:
				sub, super := w.ext(x.Sub), w.ext(x.Super)
				for _, i := range sortedKeys(sub) {
					if !super[i] {
						want = append(want, i)
					}
				}
			case owl.DisjointClasses:
				a, b := w.ext(x[0]), w.ext(x[1])
				for _, i := range sortedKeys(a) {
					if b[i] {
						want = append(want, i)
					}
				}
			default:
				continue
			}
			got := violators(r.Violations, ax)
			if !slices.Equal(got, want) {
				t.Errorf("seed %d: %v\nviolated by %v, want %v", seed, ax, got, want)
			}
			if len(want) > 0 {
				violated++
			} else {
				held++
			}
		}
	}
	// A validator that never found anything, or always did, would pass the
	// comparison above with a broken reference too.
	if violated == 0 || held == 0 {
		t.Errorf("of the checks, %d were violated and %d held; the batch needs both", violated, held)
	}
	t.Logf("%d checks violated, %d held", violated, held)
}

// The transitive closure the rules derive is exactly what a search reaches, and
// each counterexample's path is a shortest one.
func TestClosureIsReachability(t *testing.T) {
	reached := 0
	for seed := int64(0); seed < worlds; seed++ {
		w := newWorld(rand.New(rand.NewSource(seed)))
		data := parse(t, w.data())
		var checks []string
		for x := 0; x < w.n; x++ {
			for y := 0; y < w.n; y++ {
				checks = append(checks, fmt.Sprintf("NegativeObjectPropertyAssertion(:r :i%d :i%d)", x, y))
			}
		}
		r := Validate(data, parse(t, rules+strings.Join(checks, "\n")))
		checkPaths(t, data, r.Violations)
		got := map[[2]int][]owl.ObjectPropertyAssertion{}
		for _, v := range r.Violations {
			x := owl.Unwrap(v.Axiom).(owl.NegativeObjectPropertyAssertion)
			got[[2]int{index(x.Subject), index(x.Object)}] = v.Path
		}
		for x := 0; x < w.n; x++ {
			dist := w.distances(x)
			for y := 0; y < w.n; y++ {
				path, derived := got[[2]int{x, y}]
				d, reachable := dist[y]
				reachable = reachable && d > 0
				if derived != reachable {
					t.Errorf("seed %d: r(i%d, i%d) derived %v, reachable %v", seed, x, y, derived, reachable)
					continue
				}
				if !derived {
					continue
				}
				reached++
				if len(path) != d || index(path[0].Subject) != x || index(path[len(path)-1].Object) != y {
					t.Errorf("seed %d: path for r(i%d, i%d) = %v, want a chain of %d from i%d to i%d", seed, x, y, path, d, x, y)
				}
			}
		}
	}
	if reached == 0 {
		t.Error("nothing was reachable in any world; the test is vacuous")
	}
}

// What the logic says must be equal, the validator treats as equal: a
// universal is a negated existential of the negation, and De Morgan holds.
func TestIdentities(t *testing.T) {
	for seed := int64(0); seed < worlds; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w := newWorld(rng)
		data := parse(t, w.data())
		var everyone []string
		for i := 0; i < w.n; i++ {
			everyone = append(everyone, fmt.Sprintf(":i%d", i))
		}
		all := "ObjectOneOf(" + strings.Join(everyone, " ") + ")"
		// Who fails SubClassOf(everyone, X) is everyone outside X.
		outside := func(x string) []int {
			ax := "SubClassOf(" + all + " " + x + ")"
			constraints := parse(t, rules+ax)
			return violators(Validate(data, constraints).Violations, slices.Collect(constraints.All())[len(slices.Collect(constraints.All()))-1])
		}
		for k := 0; k < 4; k++ {
			p, e, a, b := randProperty(rng), randExpr(rng, 2), randExpr(rng, 2), randExpr(rng, 2)
			for _, pair := range [][2]string{
				{"ObjectAllValuesFrom(" + p + " " + e + ")", "ObjectComplementOf(ObjectSomeValuesFrom(" + p + " ObjectComplementOf(" + e + ")))"},
				{"ObjectComplementOf(ObjectIntersectionOf(" + a + " " + b + "))", "ObjectUnionOf(ObjectComplementOf(" + a + ") ObjectComplementOf(" + b + "))"},
				{"ObjectComplementOf(ObjectUnionOf(" + a + " " + b + "))", "ObjectIntersectionOf(ObjectComplementOf(" + a + ") ObjectComplementOf(" + b + "))"},
			} {
				if l, r := outside(pair[0]), outside(pair[1]); !slices.Equal(l, r) {
					t.Errorf("seed %d: %s and %s differ: %v vs %v", seed, pair[0], pair[1], l, r)
				}
			}
		}
	}
}

// render writes a report out in full, so that two can be compared.
func render(r Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "checked %d\n", r.Checked)
	for _, list := range [][]Violation{r.Violations, r.Unsupported} {
		for _, v := range list {
			fmt.Fprintf(&b, "%s | %s | %v | %s\n", owl.Functional(v.Axiom), v.Subject, v.Path, v.Reason)
		}
		b.WriteString("--\n")
	}
	fmt.Fprintf(&b, "empty %v\n", r.Empty)
	return b.String()
}

// Naming an individual nobody relates to changes no answer about an anchored
// subject. That is what anchoring is for: without it, a rule over owl:Thing
// would gain a counterexample every time some unrelated name was mentioned.
func TestUnrelatedNamesChangeNothing(t *testing.T) {
	for seed := int64(0); seed < worlds; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w := newWorld(rng)
		data := parse(t, w.data())
		checks := rules + strings.Join(randChecks(rng), "\n")
		before := Validate(data, parse(t, checks))
		after := Validate(data, parse(t, checks+"\nNegativeObjectPropertyAssertion(:p :z1 :z2)"))
		after.Checked--
		if render(before) != render(after) {
			t.Errorf("seed %d: naming :z1 and :z2 changed the report\nbefore:\n%s\nafter:\n%s", seed, render(before), render(after))
		}
	}
}

// The report does not depend on the order the constraints were written in.
func TestOrderDoesNotMatter(t *testing.T) {
	for seed := int64(0); seed < worlds; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w := newWorld(rng)
		data := parse(t, w.data())
		lines := append(strings.Split(strings.TrimSpace(rules), "\n"), randChecks(rng)...)
		lines = append(lines, "TransitiveObjectProperty(:p)", "EquivalentClasses(:A :B)") // two refusals
		want := render(Validate(data, parse(t, strings.Join(lines, "\n"))))
		for k := 0; k < 3; k++ {
			rng.Shuffle(len(lines), func(i, j int) { lines[i], lines[j] = lines[j], lines[i] })
			if got := render(Validate(data, parse(t, strings.Join(lines, "\n")))); got != want {
				t.Fatalf("seed %d: shuffled constraints gave another report\nwant:\n%s\ngot:\n%s", seed, want, got)
			}
		}
	}
}
