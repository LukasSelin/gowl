package owl

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// IsBuiltin reports whether an entity comes from the OWL, RDF, RDFS or XSD
// vocabulary. Those terms are always available and no ontology is expected to
// declare them.
func IsBuiltin(e Entity) bool {
	iri := string(e.IRI())
	for _, ns := range []IRI{NamespaceOWL, NamespaceRDF, NamespaceRDFS, NamespaceXSD} {
		if strings.HasPrefix(iri, string(ns)) && len(iri) > len(ns) {
			return true
		}
	}
	return false
}

// Term is one entry in a [Vocabulary]: an entity together with the names a
// caller is likely to know it by.
type Term struct {
	Entity Entity
	// Name is the entity's compact form when a prefix applies, else its full
	// IRI. This is the name [Vocabulary.Prompt] presents.
	Name    string
	Label   string
	Comment string
}

// Vocabulary is the closed set of terms an ontology defines, in a form built
// for handing to something that must choose among them — a language model, a
// completion UI, a validator for externally supplied axioms.
//
// It answers three questions the raw signature does not: what may I write
// (Prompt), what does this name mean (Lookup, Resolve), and does this axiom
// stay inside the vocabulary (Validate, ParseAxiom).
//
// A Vocabulary is a snapshot taken when it is built. It does not track later
// edits to the ontology, and it is safe to share across goroutines for reading.
type Vocabulary struct {
	prefixes *Prefixes
	terms    []Term

	// Names and labels both map to several terms, because an IRI can be punned
	// across entity kinds and two entities can carry the same label. Ambiguity
	// is reported rather than silently resolved.
	byEntity map[Entity]int
	byName   map[string][]int
	byLabel  map[string][]int
}

// VocabularyOption adjusts which entities [NewVocabulary] includes.
type VocabularyOption func(*vocabConfig)

type vocabConfig struct {
	includeBuiltins bool
	declaredOnly    bool
	kinds           map[Kind]bool
}

// WithBuiltins keeps the OWL, RDF, RDFS and XSD terms, which are excluded by
// default because a caller choosing domain terms rarely wants them listed.
// They stay valid in [Vocabulary.Validate] either way.
func WithBuiltins() VocabularyOption {
	return func(c *vocabConfig) { c.includeBuiltins = true }
}

// DeclaredOnly restricts the vocabulary to entities the ontology declares,
// dropping any that merely appear in an axiom.
func DeclaredOnly() VocabularyOption {
	return func(c *vocabConfig) { c.declaredOnly = true }
}

// OnlyKinds restricts the vocabulary to the given entity kinds.
func OnlyKinds(kinds ...Kind) VocabularyOption {
	return func(c *vocabConfig) {
		c.kinds = make(map[Kind]bool, len(kinds))
		for _, k := range kinds {
			c.kinds[k] = true
		}
	}
}

// NewVocabulary collects an ontology's terms, in signature order: by kind, then
// by IRI.
func NewVocabulary(o *Ontology, opts ...VocabularyOption) *Vocabulary {
	cfg := &vocabConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	comments := comments(o)
	index := o.Index()

	v := &Vocabulary{
		prefixes: o.Prefixes,
		byEntity: make(map[Entity]int),
		byName:   make(map[string][]int),
		byLabel:  make(map[string][]int),
	}
	for _, e := range o.Signature() {
		if !cfg.includeBuiltins && IsBuiltin(e) {
			continue
		}
		if cfg.declaredOnly && !index.IsDeclared(e) {
			continue
		}
		if cfg.kinds != nil && !cfg.kinds[e.Kind()] {
			continue
		}
		v.add(Term{
			Entity:  e,
			Name:    v.name(e.IRI()),
			Label:   index.Label(e),
			Comment: comments[e.IRI()],
		})
	}
	return v
}

// comments collects the first rdfs:comment asserted for each IRI. The index
// tracks labels but not comments, and a vocabulary wants both.
func comments(o *Ontology) map[IRI]string {
	out := make(map[IRI]string)
	for ax := range o.All() {
		a, ok := Unwrap(ax).(AnnotationAssertion)
		if !ok || a.Property != RDFSComment {
			continue
		}
		if _, seen := out[a.Subject]; seen {
			continue
		}
		if l, ok := a.Value.(Literal); ok {
			out[a.Subject] = l.Value
		}
	}
	return out
}

// name is the compact form of an IRI when a prefix applies, else the IRI.
func (v *Vocabulary) name(iri IRI) string {
	if c, ok := v.prefixes.Compact(iri); ok {
		return c
	}
	return string(iri)
}

func (v *Vocabulary) add(t Term) {
	i := len(v.terms)
	v.terms = append(v.terms, t)
	v.byEntity[t.Entity] = i
	v.byName[t.Name] = append(v.byName[t.Name], i)
	if full := string(t.Entity.IRI()); full != t.Name {
		v.byName[full] = append(v.byName[full], i)
	}
	if t.Label != "" {
		v.byLabel[foldLabel(t.Label)] = append(v.byLabel[foldLabel(t.Label)], i)
	}
}

func foldLabel(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// Len returns the number of terms.
func (v *Vocabulary) Len() int { return len(v.terms) }

// Terms returns every term, in signature order. The slice is shared; do not
// modify it.
func (v *Vocabulary) Terms() []Term { return v.terms }

// TermsOfKind returns the terms of one entity kind, in IRI order.
func (v *Vocabulary) TermsOfKind(k Kind) []Term {
	var out []Term
	for _, t := range v.terms {
		if t.Entity.Kind() == k {
			out = append(out, t)
		}
	}
	return out
}

// Contains reports whether e is one of the vocabulary's terms. Builtin
// entities are not members unless [WithBuiltins] was used; [Vocabulary.Validate]
// accepts them regardless.
func (v *Vocabulary) Contains(e Entity) bool {
	_, ok := v.byEntity[e]
	return ok
}

// Lookup resolves a name to a term. It accepts a compact name, a full IRI, a
// <bracketed> IRI, or an rdfs:label matched without regard to case. It reports
// false when the name is unknown or matches more than one term; use
// [Vocabulary.Resolve] for an error that says which.
func (v *Vocabulary) Lookup(name string) (Term, bool) {
	matches := v.candidates(name)
	if len(matches) != 1 {
		return Term{}, false
	}
	return v.terms[matches[0]], true
}

// LookupKind is [Vocabulary.Lookup] restricted to one entity kind, which
// resolves the ambiguity when an IRI is punned or a label is shared.
func (v *Vocabulary) LookupKind(name string, k Kind) (Term, bool) {
	var found Term
	n := 0
	for _, i := range v.candidates(name) {
		if v.terms[i].Entity.Kind() == k {
			found, n = v.terms[i], n+1
		}
	}
	if n != 1 {
		return Term{}, false
	}
	return found, true
}

// Resolve returns the entity a name denotes, or an error naming the problem:
// unknown, or ambiguous across several terms.
func (v *Vocabulary) Resolve(name string) (Entity, error) {
	matches := v.candidates(name)
	switch len(matches) {
	case 1:
		return v.terms[matches[0]].Entity, nil
	case 0:
		if suggestion, ok := v.nearest(name); ok {
			return nil, fmt.Errorf("owl: %q is not in the vocabulary (did you mean %q?)", name, suggestion)
		}
		return nil, fmt.Errorf("owl: %q is not in the vocabulary", name)
	default:
		var kinds []string
		for _, i := range matches {
			kinds = append(kinds, v.terms[i].Entity.Kind().String())
		}
		return nil, fmt.Errorf("owl: %q is ambiguous: it names %s", name, strings.Join(kinds, ", "))
	}
}

// candidates returns the term indexes a name could denote, most specific form
// first: an exact name, then the name expanded through the prefixes, then a
// label. Later forms are only consulted when earlier ones find nothing, so a
// term named ":Pizza" is never shadowed by something labelled ":Pizza".
func (v *Vocabulary) candidates(name string) []int {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if strings.HasPrefix(name, "<") && strings.HasSuffix(name, ">") {
		name = name[1 : len(name)-1]
	}
	if m, ok := v.byName[name]; ok {
		return m
	}
	if iri, err := v.prefixes.Expand(name); err == nil {
		if m, ok := v.byName[string(iri)]; ok {
			return m
		}
	}
	return v.byLabel[foldLabel(name)]
}

// nearest finds a term whose name or label differs from the given one only by
// case or surrounding punctuation, which covers the common near-misses without
// pulling in edit-distance machinery.
func (v *Vocabulary) nearest(name string) (string, bool) {
	want := simplify(name)
	if want == "" {
		return "", false
	}
	for _, t := range v.terms {
		if simplify(t.Name) == want || (t.Label != "" && simplify(t.Label) == want) {
			return t.Name, true
		}
	}
	return "", false
}

// simplify reduces a name to its letters and digits, lowercased.
func simplify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// UnknownTermsError reports entities used outside the vocabulary.
type UnknownTermsError struct {
	Terms []Entity
}

func (e *UnknownTermsError) Error() string {
	names := make([]string, 0, len(e.Terms))
	for _, t := range e.Terms {
		names = append(names, fmt.Sprintf("%s %s", t.Kind(), t.IRI()))
	}
	return "owl: not in the vocabulary: " + strings.Join(names, ", ")
}

// Unknown returns the entities n references that the vocabulary does not
// contain, sorted by kind then IRI. Builtin entities never count as unknown:
// owl:Thing and the XSD datatypes are always available.
func (v *Vocabulary) Unknown(n Node) []Entity {
	seen := make(map[Entity]bool)
	Walk(n, func(e Entity) {
		if !v.Contains(e) && !IsBuiltin(e) {
			seen[e] = true
		}
	})
	return sortedEntities(seen)
}

// Validate reports an [UnknownTermsError] when n uses anything outside the
// vocabulary, and nil otherwise.
func (v *Vocabulary) Validate(n Node) error {
	if unknown := v.Unknown(n); len(unknown) > 0 {
		return &UnknownTermsError{Terms: unknown}
	}
	return nil
}

// ParseAxiom parses one axiom against the vocabulary's prefixes and rejects it
// if it uses any term the vocabulary does not define. This is the gate for
// axioms that arrive from somewhere untrusted — a language model, a form, an
// imported patch — in one call.
func (v *Vocabulary) ParseAxiom(src string) (Axiom, error) {
	ax, err := ParseAxiom(src, v.prefixes)
	if err != nil {
		return nil, err
	}
	if err := v.Validate(ax); err != nil {
		return nil, err
	}
	return ax, nil
}

// ParseClassExpression is [Vocabulary.ParseAxiom] for a class expression.
func (v *Vocabulary) ParseClassExpression(src string) (ClassExpression, error) {
	ce, err := ParseClassExpression(src, v.prefixes)
	if err != nil {
		return nil, err
	}
	if err := v.Validate(ce); err != nil {
		return nil, err
	}
	return ce, nil
}

// Prompt renders the vocabulary as a listing to put in front of whatever has to
// choose among the terms. Terms are grouped by kind, one per line, as
//
//	:Pizza — Pizza — a pizza with at least one topping
//
// with the label and comment omitted when absent. Only the prefixes the listing
// actually uses are declared, and comments are folded onto one line. An empty
// vocabulary renders as the empty string.
func (v *Vocabulary) Prompt() string {
	var b strings.Builder
	v.writePrompt(&b)
	return b.String()
}

// WritePrompt writes [Vocabulary.Prompt] to w.
func (v *Vocabulary) WritePrompt(w io.Writer) error {
	_, err := io.WriteString(w, v.Prompt())
	return err
}

func (v *Vocabulary) writePrompt(b *strings.Builder) {
	if len(v.terms) == 0 {
		return
	}

	if used := v.usedPrefixes(); len(used) > 0 {
		b.WriteString("Prefixes\n")
		for _, name := range used {
			ns, _ := v.prefixes.Namespace(name)
			b.WriteString("  " + name + ": " + string(ns) + "\n")
		}
		b.WriteString("\n")
	}

	first := true
	for _, k := range []Kind{
		KindClass, KindObjectProperty, KindDataProperty,
		KindAnnotationProperty, KindNamedIndividual, KindDatatype,
	} {
		terms := v.TermsOfKind(k)
		if len(terms) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false

		fmt.Fprintf(b, "%s (%d)\n", kindHeading(k), len(terms))
		for _, t := range terms {
			b.WriteString("  " + t.Name)
			if t.Label != "" {
				b.WriteString(" — " + oneLine(t.Label))
			}
			if t.Comment != "" {
				b.WriteString(" — " + oneLine(t.Comment))
			}
			b.WriteString("\n")
		}
	}
}

// usedPrefixes returns the prefixes at least one listed term is written with,
// so the header does not declare namespaces the listing never mentions.
func (v *Vocabulary) usedPrefixes() []string {
	seen := make(map[string]bool)
	for _, t := range v.terms {
		if i := strings.IndexByte(t.Name, ':'); i >= 0 && t.Name != string(t.Entity.IRI()) {
			seen[t.Name[:i]] = true
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func kindHeading(k Kind) string {
	switch k {
	case KindClass:
		return "Classes"
	case KindDatatype:
		return "Datatypes"
	case KindObjectProperty:
		return "Object properties"
	case KindDataProperty:
		return "Data properties"
	case KindAnnotationProperty:
		return "Annotation properties"
	case KindNamedIndividual:
		return "Individuals"
	}
	return k.String()
}

// oneLine collapses whitespace so a multi-line comment cannot break the
// one-term-per-line shape of the listing.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
