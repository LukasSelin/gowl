# gowl

A typed OWL 2 ontology model for Go, plus a DSL for building and inspecting one.

`gowl` represents ontologies the way the [OWL 2 Structural Specification][spec]
does — as entities, class expressions and axioms — rather than as RDF triples
with OWL vocabulary sprinkled on top. Every construct is a distinct Go type, so
the compiler rejects statements that aren't well-formed: you cannot put a data
property where an object property belongs, or use an axiom as a class
expression.

[spec]: https://www.w3.org/TR/owl2-syntax/

```go
o := owl.New("http://example.org/pizza")
o.Prefix("", "http://example.org/pizza#")

pizza      := o.Class(":Pizza")
topping    := o.Class(":Topping")
meat       := o.Class(":MeatTopping")
hasTopping := o.ObjectProperty(":hasTopping")
hasBase    := o.ObjectProperty(":hasBase")

o.DefineObjectProperty(hasTopping).Domain(pizza).Range(topping)
o.DefineObjectProperty(hasBase).Functional()

o.Define(topping)
o.Define(meat).SubClassOf(topping)
o.Define(pizza).
    SubClassOf(owl.Some(hasTopping, topping), owl.Exactly(1, hasBase)).
    Label("Pizza")

o.Define(o.Class(":VegetarianPizza")).
    EquivalentTo(owl.And(pizza, owl.Only(hasTopping, owl.Not(meat))))

fmt.Println(o.Functional())
```

```
Prefix(:=<http://example.org/pizza#>)
...
Ontology(<http://example.org/pizza>
    Declaration(ObjectProperty(:hasTopping))
    ObjectPropertyDomain(:hasTopping :Pizza)
    ...
    SubClassOf(:Pizza ObjectSomeValuesFrom(:hasTopping :Topping))
    SubClassOf(:Pizza ObjectExactCardinality(1 :hasBase))
    AnnotationAssertion(rdfs:label :Pizza "Pizza"^^xsd:string)
    EquivalentClasses(:VegetarianPizza ObjectIntersectionOf(:Pizza ObjectAllValuesFrom(:hasTopping ObjectComplementOf(:MeatTopping))))
)
```

## Two ways to write the same thing

The fluent builders (`Define`, `DefineObjectProperty`, `DefineDataProperty`,
`DefineIndividual`) are sugar: each method appends ordinary axiom values. Drop
to struct literals whenever that reads better, and mix the two freely.

```go
o.Add(owl.SubClassOf{Sub: pizza, Super: owl.Some(hasTopping, topping)})
o.Add(owl.DisjointClasses{meat, cheese, vegetable})
```

Class expressions have both spec-named types and short constructors, so
`owl.And(a, owl.Some(p, b))` and `owl.ObjectIntersectionOf{a,
owl.ObjectSomeValuesFrom{Property: p, Filler: b}}` are the same value.

| Shorthand | OWL 2 construct | DL |
|---|---|---|
| `And` / `Or` / `Not` | `ObjectIntersectionOf` / `ObjectUnionOf` / `ObjectComplementOf` | ⊓ ⊔ ¬ |
| `Some` / `Only` | `ObjectSomeValuesFrom` / `ObjectAllValuesFrom` | ∃ ∀ |
| `Min` / `Max` / `Exactly` | object cardinality restrictions | ≥ ≤ = |
| `HasValue` / `Self` / `OneOf` | `ObjectHasValue` / `ObjectHasSelf` / `ObjectOneOf` | ∋ |
| `Inverse` | `ObjectInverseOf` | ⁻ |

`Min`, `Max` and `Exactly` take an optional filler; omit it for an unqualified
restriction.

## Names and prefixes

Entities are string-backed types (`type Class IRI`), so they are comparable,
usable as map keys and cheap to copy. `o.Class(":Pizza")` expands a CURIE
through the ontology's prefix table; absolute IRIs and `<bracketed>` forms pass
through unchanged.

Expansion failures don't panic and don't force an error check on every line —
the ontology records the first one, so check it once when you're done:

```go
if err := o.Err(); err != nil { ... }
```

## Inspecting an ontology

```go
o.Signature()                  // every entity mentioned, sorted by kind then IRI
o.Classes()                    // ...or just the classes
o.SubClassesOf(topping)        // asserted, named, direct
o.AncestorsOf(mozzarella)      // transitive closure of the asserted hierarchy
o.AxiomsReferencing(meat)      // every axiom mentioning an entity
o.TypesOf(margherita)          // asserted class assertions
o.ObjectValues(margherita, hasTopping)
o.Label(pizza)                 // first rdfs:label

owl.Signature(anyConstruct)    // works on a lone axiom or expression too
owl.Walk(anyConstruct, fn)     // visit every referenced entity
owl.Equal(a, b)                // structural equality, safe on slice-shaped constructs
```

These queries report what is **asserted**, not what is **entailed**. There is no
reasoner here: `AncestorsOf` closes over asserted `SubClassOf` axioms between
named classes and stops there.

They are all backed by an index that the ontology builds on first use and
discards on the next mutation, so a burst of queries costs one pass rather than
one scan each. `owl.NewIndex(o)` returns a standalone snapshot if you want to
hold one explicitly — useful when reading concurrently, since a snapshot never
rebuilds itself.

Because a stale index would be invisible, `Axioms()` returns a **copy**. To work
with the axioms without paying for that:

```go
for ax := range o.All() { ... }     // iterate, no copy
o.Rewrite(owl.CanonicalAxiom)       // transform in place, invalidates the index
o.Sort()                            // stable ordering, invalidates the index
```

### Scale

Indexing turns the hierarchy closures from quadratic into linear in the part of
the graph they reach. On a synthetic ontology of 5,000 classes (~21,000 axioms),
comparing the previous scanning implementation against the index:

| Query | Scanning | Indexed |
|---|---|---|
| `Signature` | 6.9 ms | 2 ns |
| `SuperClassesOf` | 96 µs | 16 ns |
| `Label` | 129 µs | 51 ns |
| `AncestorsOf` | 660 µs | 1.3 µs |
| `DescendantsOf` | 441 ms | 1.7 ms |

Building the index costs about 18 ms at that size and is the price of the first
query, so a single lookup on a large ontology is now *slower* than a scan would
have been; anything that asks more than a few questions wins immediately. End to
end on an 84,000-axiom, 3.8 MB document, including parsing: `gowl lint` 0.42 s,
`gowl stats` 0.66 s, `gowl diff` 0.65 s.

Numbers are from `go test ./owl -bench .` on one machine with synthetic data —
useful for the shape of the change, not as absolutes.

## Reading and writing functional syntax

`owl.Functional(x)` renders any construct in OWL 2 Functional-Style Syntax with
full IRIs. `o.Functional()` renders a whole document, abbreviating IRIs with the
ontology's prefixes; `o.WriteFunctional(w)` streams it to an `io.Writer`.

`String()` follows two conventions: IRIs and entities print as their bare IRI
(so `fmt.Println(pizza)` is readable), while expressions, axioms and ontologies
print as functional syntax.

Parsing goes the other way, and round-trips: rendering a parsed document
reproduces it byte for byte.

```go
o, err := owl.ParseFunctional(file)          // or ParseFunctionalString(s)
ax, err := owl.ParseAxiom(line, o.Prefixes)  // a single axiom
ce, err := owl.ParseClassExpression(s, nil)  // a single class expression
```

Errors carry a line number: `owl: line 3: unknown axiom Frobnicate`. The parser
handles `#` line comments, and a failed parse returns a nil result rather than a
half-built one. A fuzz target (`FuzzParseFunctional`) checks that malformed
input always yields an error rather than a panic, and that anything which parses
survives a render/re-parse cycle.

Two constructs in the grammar have no home in the model, and are reported as
errors instead of being silently dropped: n-ary `DataSomeValuesFrom` /
`DataAllValuesFrom`, and anonymous individuals as `AnnotationAssertion`
subjects. Annotations *on* annotations are parsed and discarded.

### Axiom annotations

`Annotation(...)` inside an axiom is modelled by the `Annotated` wrapper:

```go
SubClassOf(Annotation(rdfs:comment "why") :Dog :Animal)
```

parses to `owl.Annotated{Annotations: ..., Axiom: owl.SubClassOf{...}}` and
renders back by splicing the annotations into the argument list. A wrapper
changes an axiom's dynamic type, so code that switches on axiom types should
call `owl.Unwrap(ax)` first — the queries on `Ontology` already do.

## Closed vocabularies

`Vocabulary` turns an ontology's signature into the set of terms something else
is allowed to use — a language model choosing among them, a completion UI, a
validator for axioms that arrive from outside.

```go
v := owl.NewVocabulary(o)

fmt.Println(v.Prompt())          // a listing to put in front of a model
e, err := v.Resolve("has topping")  // a label, CURIE or IRI -> the entity
ax, err := v.ParseAxiom(line)    // parse, then reject any invented term
err = v.Validate(someAxiom)      // *owl.UnknownTermsError names the offenders
```

`Prompt` groups terms by kind and declares only the prefixes the listing
actually uses:

```
Prefixes
  : http://example.org/pizza#

Classes (2)
  :Pizza — Pizza — a pizza with at least one topping
  :Topping — Topping

Object properties (1)
  :hasTopping — has topping
```

`Resolve` accepts a compact name, a full or `<bracketed>` IRI, or an
`rdfs:label` matched without regard to case, and near misses come back as a
suggestion rather than a bare failure:

```
owl: "has_topping" is not in the vocabulary (did you mean ":hasTopping"?)
```

Ambiguity is reported, never guessed: a punned IRI or a shared label makes
`Lookup` fail and `Resolve` say why, with `LookupKind` available to break the
tie. Builtins are excluded from the listing by default — `owl:Thing` and the
XSD datatypes are noise in a term menu — but they always pass `Validate`.
`WithBuiltins`, `DeclaredOnly` and `OnlyKinds` adjust what is collected.

A `Vocabulary` is a snapshot: it does not track later edits to the ontology,
and is safe to share across goroutines for reading.

## Standard vocabularies

`vocab/` ships the vocabularies the web already agrees on, as typed constants
with the axioms their publishers assert. They are generated from the published
documents themselves, not transcribed.

```go
import (
    "gowl/owl"
    "gowl/vocab/dcterms"
    "gowl/vocab/skos"
)

o := owl.New("http://example.org/catalogue")
o.Prefix("", "http://example.org/catalogue#")
o.Prefix(skos.Prefix, skos.Namespace)

topic := o.Class(":Topic")
o.Define(topic).SubClassOf(skos.Concept).Label("Topic")
o.Add(
    owl.SubObjectPropertyOf{Sub: o.ObjectProperty(":broaderTopic"), Super: skos.Broader},
    owl.DataPropertyDomain{Property: dcterms.Title, Domain: topic},
)
```

Every term carries its kind, so `skos.Broader` cannot be written where a class
belongs, and its documentation comes from the vocabulary itself:

```go
// PrefLabel is skos:prefLabel, "preferred label".
//
// The preferred lexical label for a resource, in a given language.
PrefLabel owl.AnnotationProperty = "http://www.w3.org/2004/02/skos/core#prefLabel"
```

| Package | Terms | Vocabulary |
|---|---|---|
| `vocab/rdf` | 22 | RDF itself: `rdf:type`, containers, the reification vocabulary |
| `vocab/rdfs` | 15 | RDF Schema |
| `vocab/skos` | 32 | SKOS — concepts, schemes, broader/narrower |
| `vocab/skosxl` | 6 | SKOS labels as resources |
| `vocab/dcterms` | 104 | DCMI Metadata Terms |
| `vocab/dc` | 15 | the original fifteen Dublin Core elements |
| `vocab/foaf` | 75 | FOAF — people, accounts, documents |
| `vocab/prov` | 97 | PROV-O — entities, activities, agents |
| `vocab/dcat` | 56 | DCAT — catalogs, datasets, distributions |
| `vocab/org` | 45 | the W3C Organization Ontology |
| `vocab/time` | 106 | OWL-Time — instants, intervals, Allen relations |
| `vocab/schema` | 3018 | schema.org |

Each package exposes the same three things: the constants, `Ontology()` for a
fresh copy of the axioms, and `Vocabulary()` for the same terms as a closed set
with their labels.

```go
v := skos.Vocabulary()
ax, err := v.ParseAxiom("SubClassOf(skos:OrderedCollection skos:Collection)")
_, err = v.ParseAxiom("SubClassOf(skos:Concept <http://example.org/Invented>)")
// owl: not in the vocabulary: Class http://example.org/Invented
```

The `vocab` package itself is the index, for the things a program cannot
hard-code — importing it links in every vocabulary, schema.org included:

```go
entry, ok := vocab.Owner("http://www.w3.org/ns/prov#wasDerivedFrom")  // -> prov
entry, ok = vocab.ByPrefix("dcterms")
p := vocab.Prefixes()          // one table covering all of them
o := vocab.Merge(iri, entries...)  // one ontology, duplicates dropped
```

### How they are generated

`go generate ./vocab` re-runs `cmd/vocabgen`, which fetches each document
listed in `cmd/vocabgen/sources.go`, reads it as RDF, maps the triples onto the
structural model and writes two files per vocabulary: a `.ofn` holding the
ontology in functional syntax, and a `.go` holding the constants. The Go file
embeds the `.ofn` and parses it, so a generated package is exercised by the
same reader everything else uses. Both files are committed; nothing is fetched
at build time.

Reading RDF back into a structural ontology is not a lossless operation, and
`vocabgen` says so out loud:

```
vocabgen: skos       32 terms    254 triples    209 axioms     0 untranslated
vocabgen: schema    3026 terms  17949 triples  18229 axioms    20 untranslated
vocabgen:          untranslated: datatype declared as an instance x7, ...
```

Every triple it cannot translate is counted and, with `-v`, printed. What
remains untranslated across all twelve vocabularies is constructs OWL 2 has no
axiom for rather than gaps in the reader: one datatype declared a subclass of
another, an ontology header crediting its editors as anonymous nodes, a
property that is inverse functional *and* a datatype property, or two
properties related across the data/object divide.

The generated ontologies lint clean of errors, and `gowl lint` over them is
worth reading, because what it reports is these vocabularies being what they
are rather than the reader mangling them. `undeclared-entity` is terms they
reference from each other, which belong to whoever defines them.
`punned-entity` is the honest residue of reading OWL Full into a structural
model, and the one deliberate trade below. `missing-label` comes down to six
DCAT properties — `dcat:inCatalog`, `dcat:seriesMember` and the other
inverse-direction ones — that DCAT itself leaves unlabelled, defining them by
`owl:inverseOf` and a SKOS note alone. Inventing labels for them would be
making data up, so they stay as they are.

Two mapping decisions are worth knowing about, because neither follows from a
spec:

- **A bare `rdf:Property` has no OWL kind.** It is read as a data property when
  everything its range says is a datatype, and as an object property
  otherwise. So `schema:name` is a `DataProperty` and `schema:author` an
  `ObjectProperty`.
- **An unrecognised predicate becomes an annotation, not an error.**
  schema.org's `domainIncludes` and `rangeIncludes` stay the documentation they
  are, rather than being promoted to `rdfs:domain` and `rdfs:range`, which
  schema.org explicitly says they are not. Where a document also declares such
  a predicate a data property — DCMI does with `dcterms:title` — this puns it
  across two entity kinds, which OWL 2 DL forbids. Reading those triples as
  property assertions instead avoids the pun but turns every documented term
  into an individual, costing schema.org two thousand spurious entities, and
  discards the metadata whose property has no declared range. Keeping the
  documentation is the better trade; `punned-entity` makes the cost visible.

The RDF readers behind this live in `internal/rdf` — Turtle and RDF/XML, enough
for these documents — and are not part of gowl's public API. gowl's model is
structural; a triple store is a different thing, and would have to be designed
as one.

## Reasoning

`el.Classify` runs an OWL 2 EL classifier: it works out the class hierarchy the
axioms *entail*, not just the one they state. Everything else in this package
reports what is asserted; this is the part that reasons.

```go
c := el.Classify(o)

c.IsSubsumedBy(margherita, cheesyPizza)  // entailed, even if never asserted
c.SuperClassesOf(margherita)             // everything above it
c.DirectSuperClassesOf(margherita)       // the taxonomy, transitively reduced
c.EquivalentClassesOf(x)
c.UnsatisfiableClasses()                 // classes that cannot have instances
c.IsCoherent()                           // ...and whether there are any
c.InferredAxioms()                       // the result as ontology axioms
c.Explain(margherita, cheesyPizza)       // the axioms behind an entailment
c.Unsupported()                          // what it could not use
```

The classic case: nothing says a Margherita is a cheesy pizza, but it is.

```
$ gowl classify -explain ":Margherita :CheesyPizza" pizza.ofn
:Margherita ⊑ :CheesyPizza follows from:
  EquivalentClasses(:CheesyPizza ObjectIntersectionOf(:Pizza ObjectSomeValuesFrom(:hasTopping :CheeseTopping)))
  SubClassOf(:Margherita :Pizza)
  SubClassOf(:Margherita ObjectSomeValuesFrom(:hasTopping :Mozzarella))
  SubClassOf(:Mozzarella :CheeseTopping)
```

### How it works

Normalize every axiom into one of four shapes, then saturate a set of derived
facts with seven completion rules until nothing new appears - the algorithm of
Baader, Brandt and Lutz, the family ELK belongs to. Saturation is polynomial
and monotone: no tableau, no backtracking, no search. That is why EL scales to
ontologies the size of SNOMED when full OWL 2 DL does not, and it is why most
large biomedical ontologies are written in EL on purpose.

### What it covers, and what it refuses

Class expressions: named classes, `owl:Thing`, `owl:Nothing`,
`ObjectIntersectionOf`, `ObjectSomeValuesFrom`. Axioms: `SubClassOf`,
`EquivalentClasses`, `DisjointClasses`, `ObjectPropertyDomain`,
`SubObjectPropertyOf`, `EquivalentObjectProperties`,
`TransitiveObjectProperty` and property chains.

Everything else - nominals, `ObjectHasSelf`, data properties, property
**ranges**, keys, and every non-EL constructor - is listed by `Unsupported()`
rather than dropped in silence, because an axiom skipped quietly makes a
classification look complete when it is not:

```
not classified (1 axiom outside OWL 2 EL)
  ObjectPropertyRange(:hasTopping :Topping)

note: axioms outside OWL 2 EL were skipped, so a subsumption that is not
reported may still be entailed
```

To check a finite set of facts against axioms, rather than infer from them,
see [Invariants that outlive a change](#invariants-that-outlive-a-change): a
closed-world check of a small fragment, with counterexamples.

Results are always **sound** - what it derives is entailed. Skipping axioms
costs **completeness**, so when `Unsupported()` is non-empty, a subsumption it
does not report is not proof that the subsumption fails. Individuals are not
classified: this is a TBox reasoner.

### Explanations

`Explain` returns the axioms behind an entailment. Provenance is recorded as
each fact is derived rather than reconstructed afterwards, which is the only
practical way to get it - running a saturation backwards is a different and
much harder problem.

It is the support of the derivation actually found, **not a minimal
justification**: it entails the subsumption, but a smaller set might too. The
test suite checks the guarantee it does make - reclassifying an ontology built
from just those axioms reproduces the subsumption.

Recording provenance costs roughly 3.8x the time and 2.9x the memory (50 ms vs
13 ms on a synthetic 2,000-class ontology), so `el.WithoutExplanations()` turns
it off when classification is all you need.

### Trusting it

A reasoner that passes hand-worked examples can still be wrong, so the tests
also check properties that must hold for every ontology: reflexivity,
transitivity, agreement between `SuperClassesOf` and `SubClassesOf`, that every
asserted subsumption is entailed, that adding an axiom never removes an
entailment, and that every explanation re-entails what it explains. These run
over the corpus fixtures and over 40 generated ontologies, with a guard that
the batch infers something beyond what it was told, so the properties cannot
pass vacuously.

Rough shape at scale, from `go test ./el -bench .` on one machine with
synthetic data - useful for the trend, not as absolutes:

| Classes | Time | Memory |
|---|---|---|
| 500 | 9.9 ms | 1.9 MB |
| 2,000 | 50 ms | 8.1 MB |
| 8,000 | 187 ms | 55 MB |

## The `gowl` command

```bash
go build ./cmd/gowl
```

```
gowl lint    [-disable rules] [-fail-on severity] [-list] [-json] file.ofn
gowl diff    [-summary] [-exit-code] [-json] old.ofn new.ofn
gowl profile [-v] [-json] file.ofn
gowl classify [-axioms] [-explain "sub super"] [-unsatisfiable] [-fail-on-incoherent] [-json] file.ofn
gowl fmt     [-w] [-canonical] file.ofn
gowl stats   [-json] file.ofn
gowl gocode  [-o file.ofn] [module-dir]
gowl sprawl  [-base ref] [-fail-on severity] [-seen file] [-json] [module-dir]
gowl plan    [-base ref] [-invariants file] [-fail-on severity] [-json] plan.ofn [module-dir]
gowl conform [-base ref] [-design plan.ofn] [-fail-on severity] [-seen file] [-json] [module-dir]
gowl invariants [-file invariants.ofn] [-facts] [-fail-on severity] [-seen file] [-json] [module-dir]
```

`classify` runs the EL reasoner. `-axioms` writes the inferred taxonomy as an
ontology document, `-explain` justifies one subsumption, and
`-fail-on-incoherent` exits 1 when a class cannot have instances - which is the
form to put in CI.

`gocode` works in the other direction. It writes an ontology describing a Go
module's code, which the other commands can then read. See
[Describing a Go module](#describing-a-go-module). `sprawl` runs it twice, on
the working tree and on a base commit, to check what a change did to the
module's types. See [Catching sprawl](#catching-sprawl). `plan` and `conform`
keep a change to a design written before it. See
[Planning a change before writing it](#planning-a-change-before-writing-it).
`invariants` checks the code against the rules a module keeps on every change.
See [Invariants that outlive a change](#invariants-that-outlive-a-change).

Exit status is 0 on success, 1 when a check fails (findings at or above
`-fail-on`, or a non-empty diff under `-exit-code`), and 2 on a usage or parse
error — so `gowl lint`, `gowl conform`, `gowl invariants` and
`gowl diff -exit-code` drop straight into CI.

### JSON output

Every command except `fmt` and `gocode` takes `-json` and then writes exactly one
JSON object to stdout. Those two have none because their output is an ontology
document, not a report.

```bash
gowl lint -json onto.ofn | jq -r '.findings[] | select(.severity=="error") | .message'
gowl diff -json old.ofn new.ofn | jq '.counts'
gowl profile -json onto.ofn | jq -r '.in_profiles[]'
gowl stats -json onto.ofn | jq '.counts.classes'
gowl lint -list -json | jq -r '.rules[].name'
gowl sprawl -json | jq -r '.added[]'
gowl plan -json design/change.ofn | jq -r '.findings[].message'
gowl conform -json | jq -r '.designs[], .summary.failed'
gowl invariants -json | jq -r '.findings[].message'
```

Three properties make this safe to script against:

- **Failures are documents too.** A parse error prints `{"error": "..."}` on
  stdout and nothing on stderr, so a caller that reads stdout always gets
  something parseable. Exit statuses are identical in both modes.
- **Empty lists are `[]`, never `null`**, and `by_severity` always carries all
  three keys — so `.summary.by_severity.error` is a number, not a null.
- **`-json` changes the format, not the content.** `diff -summary` and
  `profile -v` mean the same thing in both modes, and axioms are rendered with
  the document's own prefixes rather than as expanded IRIs.

The shapes live in [`cmd/gowl/json.go`](cmd/gowl/json.go).

## Linting

`lint` checks structural policy: conventions and hygiene that hold regardless of
what an ontology means. Nothing here reasons, so it runs on ontologies no
reasoner could handle.

| Rule | Severity | Flags |
|---|---|---|
| `undeclared-entity` | warning | an entity used in an axiom but never declared |
| `punned-entity` | warning | one IRI used as more than one kind of entity |
| `missing-label` | info | a class or property with no `rdfs:label` |
| `deprecated-reference` | warning | an axiom referencing an `owl:deprecated` term |
| `duplicate-axiom` | warning | the same axiom asserted twice |
| `trivial-axiom` | warning | axioms asserting nothing, e.g. `SubClassOf(A A)` |
| `subclass-cycle` | error | a hierarchy cycle, which silently makes its classes equivalent |
| `orphan-class` | info | a class with no asserted superclass or equivalence |

One more rule is not in `Default()`, because it needs to be told which
vocabularies count as standard. `prefer-standard-term` reports a term an
ontology mints for itself when a reference vocabulary already names it —
matching on the local name and on `rdfs:label`, within one entity kind:

```go
rules := append(lint.Default(), lint.PreferStandardTerms(
    foaf.Vocabulary(), dcterms.Vocabulary(), schema.Vocabulary(),
))
```

```
info: prefer-standard-term: class :Person has the same name as foaf:Person; consider using it instead
info: prefer-standard-term: class :Human is labelled "Agent", which matches dcterms:Agent; consider using it instead
info: prefer-standard-term: objectproperty :knows has the same name as foaf:knows; consider using it instead
```

Reference order is the preference order — the first vocabulary to name a term
wins — and the rule stays quiet about a term whose alignment is already stated,
so `EquivalentClasses(:Person foaf:Person)` is not nagged about. It is `info`,
because minting your own term is often the right call. `gowl lint` wires it up
over everything in `vocab/`, with schema.org last.

A `lint.Rule` is an ordinary value, so a project can drop the built-ins it
disagrees with and add its own:

```go
rules := append(lint.Default(), lint.Rule{
    Name: "namespace", Severity: lint.Error,
    Check: func(o *owl.Ontology) []lint.Finding { ... },
})
findings := lint.Run(o, rules)
```

### Linting gowl itself

[`examples/self`](examples/self/main.go) type-checks this repository and
describes it as an OWL ontology, built with gowl's own builders.
[`examples/self/gowl.ofn`](examples/self/gowl.ofn) is the result, and it is
what the linter runs on here.

- Each exported Go type becomes a class.
- A type that satisfies one of the module's interfaces becomes a `SubClassOf`
  that interface. gowl's Go types for OWL therefore form an OWL class
  hierarchy themselves.
- Struct fields become object or data properties, with domains and ranges.
  An embedded field counts, under its type's name.
- Packages become `:Package` individuals, linked by `:imports` to the packages
  they import. A package from outside the module, such as `fmt`, is an
  `:ExternalPackage`.
- What the code offers is listed by signature: the methods of each type with
  the annotation `:method`, and the functions each package exports with the
  annotation `:func`.

```bash
go run ./examples/self > examples/self/gowl.ofn
gowl lint -fail-on info examples/self/gowl.ofn
```

```
info: orphan-class: class :el.Classification has no asserted superclass or equivalence
info: orphan-class: class :lint.Rule has no asserted superclass or equivalence
info: orphan-class: class :owl.ClassBuilder has no asserted superclass or equivalence
...
29 finding(s)
```

_(IRIs abbreviated here; the CLI prints them in full.)_

None of these findings is a warning or an error. The 29 `orphan-class` notes
are the types that satisfy no interface in the module: the builders, option
structs, `owl.Index`, the root interfaces `owl.Node` and `rdf.Term`, and the
vocabulary classes such as `:ExternalPackage`. They are accurate, and at `info`
they do not fail the default run.

The first run did not look like this. The linter reported 102 `duplicate-axiom`
warnings. They came from a bug in the generator: it called `o.Define(class)`
again for every `SubClassOf` edge, and `Define` declares the class each time it
is called. The fix was to keep one builder per class.
[`main_test.go`](examples/self/main_test.go) keeps it that way. The test fails
when the committed `gowl.ofn` no longer matches the code, and it fails on any
finding at `warning` or above.

The other commands work on the same file:

```
$ gowl stats examples/self/gowl.ofn
classes             122
object properties   144
data properties     41
individuals         64

$ gowl classify -explain ":owl.SubClassOf :owl.Node" examples/self/gowl.ofn
:owl.SubClassOf ⊑ :owl.Node follows from:
  SubClassOf(:owl.Axiom :owl.Node)
  SubClassOf(:owl.SubClassOf :owl.Axiom)
```

### Describing a Go module

The generator behind `examples/self` lives in
[`internal/gocode`](internal/gocode/gocode.go), and `gowl gocode` runs it on any
Go module. Give it the directory that holds the module's `go.mod`; it defaults
to the current directory:

```bash
gowl gocode ../doppel > doppel.ofn
gowl gocode -o doppel.ofn ../doppel   # the same, without shell redirection
gowl lint doppel.ofn
```

The module path in `go.mod` is plain `gowl`, so `go install` does not work
against the GitHub URL. To put the command on your `PATH`, clone the repository
and run `go install ./cmd/gowl`.

The ontology is named by the module path, here `<https://github.com/LukasSelin/doppel>`.
The mapping is the same as for gowl. The module has to build. Its own packages
are read from source. Everything they import is read from the export data that
`go list -export` produces inside that module, standard library and
third-party modules alike, so its dependencies must already be in the module
cache. Directories that hold their own `go.mod` are other modules and are
skipped, as the go tool skips them.

Besides its types, the description records each package's direct imports, and
what the code offers:

- A package the module imports from outside, standard library or dependency,
  is an `:ExternalPackage` individual, such as `:package.net.http`. Only the
  module's own imports are recorded, not what those packages import in turn.
  Whether a package is the module's own is decided by the packages found under
  its root, so a nested module counts as outside.
- A type lists each method it declares with the annotation `:method`, as
  `Name(parameter types) result types`, for example
  `AnnotationAssertion(:method :owl.Entity "Kind() owl.Kind")`. Parameter names
  are left out, a type is qualified by its package's name as the classes are,
  and `byte`, `rune` and `interface{}` are written `uint8`, `int32` and `any`.
- An interface lists every method. An unexported one counts, because one such
  as `owl.Axiom`'s `isAxiom` seals the interface: only its own package can
  implement it. An embedded interface of the module is a `SubClassOf` instead,
  and an embedded one from outside it, such as `io.Reader`, adds its methods.
  So does one of the module's embedded with type arguments, such as
  `Store[int]`, which is a superclass for none of them.
- Any other type lists the exported methods it declares itself, on the type or
  on its pointer. A method it gets from an embedded field is that field's. A
  method of a generic type calls the type's parameters what the type's
  declaration calls them, whatever its receiver does.
- A package lists each function it exports with the annotation `:func`, in the
  same form: `AnnotationAssertion(:func :package.gowl.owl "New(owl.IRI) *owl.Ontology")`.
  A function's own type parameters are written with it, one constraint to a
  name, as in `First[T zoo.Animal]([]T) (T, bool)`. The annotation `:name`
  gives the name the package declares, which is what its types are qualified
  with.
- A type says what it is underneath with the annotation `:kind`: `struct`,
  `interface`, or `basic`, `slice`, `array`, `map`, `chan`, `func` or
  `pointer`.
- A type declared in a group, `type ( … )`, with no comment of its own has the
  group's, which is what `go doc` shows for it. A comment that runs into a
  package clause and begins like a licence header, with `Copyright` or
  `All rights`, is not taken for the package's.
- Imports are read from the files that build on the machine running gocode, so
  an import that only a file such as `x_windows.go` makes shows up on Windows
  alone.
- Exported constants and variables are not recorded.
- A class is named after its package's name, not its path. Two packages of one
  name that each declare a type of one name therefore share a class, and what
  is said of either is said of both.

One importer serves the standard library and the dependencies, so each package
is loaded once. A cobra command's `context.Context` is therefore the same type
as the module's own. Loading the standard library and third-party code through
two importers would produce two different `context` packages, and the module
would fail to type-check.

### Catching sprawl

A codebase sprawls when a change adds new types next to the existing ones
instead of building on them: a struct copied under a new name, or a type
that nothing else uses. `gowl sprawl` looks for both in a change. It runs
`gocode` on the module twice, once on the working tree and once at `-base`
(default `HEAD`), and compares the two ontologies. The base version is copied
out with `git archive`, so the working tree, the index and other worktrees are
not touched.

- **`same-shape-type`** (warning) reports a type the change added or reshaped
  whose fields match another type's. Fields are compared by type and not by
  name, because a copy is usually renamed. The type may have the same fields
  as the other one (reuse it), every field of it plus more (embed it), or only
  fields it also has (use it instead). A match has to share at least two
  fields, and one of them has to be of a type the module defines.
- **`new-orphan`** (info) reports a new type that connects to no type that
  existed before. It satisfies none of the module's interfaces, no type
  satisfies it, and no field links it to an existing type in either direction.

On the module from the tests, with `Holding` and `Memo` added to `Item` and
`Stock`:

```
$ gowl sprawl
compared with HEAD (ea4c786a2d2d): 2 new type(s), 0 changed
  new:     :store.Holding, :store.Memo

warning: same-shape-type: :store.Holding has the same fields as :store.Stock (:store.Item, xsd:integer); consider reusing it
info: new-orphan: new type :store.Memo connects to no type that existed before: no interface, implementation or field links it to one

2 finding(s)
```

The checks look only at what the change did. Checked as if every type were
new, 32 of gowl's own types would match another of its types. Most are OWL
types: `ObjectSomeValuesFrom` and `ObjectAllValuesFrom` have the same fields
because the OWL specification gives them the same arguments. Reporting all of
those on every run would hide the one copy a change has just added. For the
same reason, a match that already held before the change is not reported
again just because one of its two types changed.

The checks see types and their fields, as `gocode` records them:

- exported types;
- fields whose type is a module type (directly, or through a pointer or slice)
  or a basic type.

Fields of maps are not recorded, and functions and methods are recorded only
as signatures, which these checks do not read. A type that is only ever a
function parameter, such as an options struct, therefore looks unconnected,
which is why `new-orphan` is only `info`. Duplicate functions are a job for a
clone detector.

Nothing reaches the default `-fail-on error`, so in CI pass `-fail-on warning`
and a base that covers the whole branch:

```bash
gowl sprawl -base origin/main -fail-on warning
```

A run takes about 3.5 seconds on this repository. Most of that is
type-checking the base copy.

#### Before an agent finishes

[`.claude/hooks/sprawl-check.sh`](.claude/hooks/sprawl-check.sh) is a Claude
Code `Stop` hook, registered in [`.claude/settings.json`](.claude/settings.json).
When a turn ends with Go changes since `HEAD`, or since `GOWL_SPRAWL_BASE` if
that is set, the hook runs `gowl sprawl` and passes any findings back to the
agent. The agent then reworks the change to reuse, embed or implement what is
already there, or tells the user why a new type is the right call.

`-seen` records each finding in a per-session file, so each finding is
reported only once. That also means the hook cannot keep the agent looping. A
turn whose Go changes were already checked costs about half a second. If the
tree does not build, the hook skips the check.

### Planning a change before writing it

`gowl sprawl` looks at a change after it is written. `gowl plan` and
`gowl conform` start earlier. The plan for a change is written first, as an
ontology in `design/<change>.ofn` next to the module's `go.mod`, and the code is
then held to it.

A plan uses the names `gocode` writes, so it can be compared with the code. It
names only what the change adds or alters, plus the existing types those
additions point at:

```
Prefix(:=<https://example.com/store#>)
Prefix(xsd:=<http://www.w3.org/2001/XMLSchema#>)

Ontology(<https://example.com/store/design/holdings>
    Declaration(Class(:store.Item))

    Declaration(Class(:store.Holding))
    Declaration(ObjectProperty(:store.Holding.What))
    ObjectPropertyDomain(:store.Holding.What :store.Holding)
    ObjectPropertyRange(:store.Holding.What :store.Item)
    Declaration(DataProperty(:store.Holding.Amount))
    DataPropertyDomain(:store.Holding.Amount :store.Holding)
    DataPropertyRange(:store.Holding.Amount xsd:integer)
)
```

`gowl plan` reads the module as though the plan were already code, so the checks
from `gowl sprawl` run before anything is built. It also reports what the plan
adds to the linter's findings, leaving out `orphan-class` and `missing-label`. A
name the plan uses and never declares, usually a typo, shows up as
`undeclared-entity`.

```
$ gowl plan -fail-on warning design/holdings.ofn
compared with main merge-base (c5bb468eaa49): 1 new type(s), 0 changed
  new:     :store.Holding

warning: same-shape-type: :store.Holding has the same fields as :store.Stock (:store.Item, xsd:integer); consider reusing it

1 finding(s)
```

A plan also names what the change makes the code offer: the methods of a type,
and the functions of a package. Each is a signature. Parameter names and
spacing are the writer's own, and a bare type, such as `Holding` in package
`store`, means `store.Holding`. A plan can say what kind of type one is, and
which package defines it:

```
    AnnotationAssertion(:kind :store.Holding "struct")
    AnnotationAssertion(:definedIn :store.Holding :package.example.com.store)
    AnnotationAssertion(:method :store.Holding "Worth(price int) int")
    AnnotationAssertion(:func :package.example.com.store "Total(holdings []Holding) int")

    Declaration(Class(:store.Priced))
    AnnotationAssertion(:kind :store.Priced "interface")
    AnnotationAssertion(:method :store.Priced "Price(item Item) (cents int, err error)")
```

An interface's methods are its contract, so a plan that adds or changes an
interface names every method it declares. For any other type it names the
exported methods the change adds, apart from the ones an interface asks for:
saying that the type satisfies the interface plans those.

What a signature calls the type parameters it declares is the writer's own, as
its parameter names are: `Map[A, B any](xs []A, f func(A) B) []B` is the code's
`Map[T, U any]`. The parameters of a generic type are another matter, because a
method does not declare them. A plan calls them what the type's declaration
does, and so does `gocode`, whatever a method's receiver calls them.

These annotations describe a type or a package. They do not name it into being:
a new type still needs its `Declaration(Class(…))`, and a new package its
`ClassAssertion(:Package …)`. An annotation about something the module does not
have and the plan does not declare is reported by `gowl plan` as
`undeclared-entity`, the same as any other name the plan uses and never
declares. That is also how a slip in a name shows up.

A plan can say only what `gocode` writes about code. Anything else it states,
such as a `DisjointClasses`, a class expression or a group, is checked by
nothing, and `gowl plan` reports it as `unchecked-axiom` while it is still a
plan. It reports a method or a function that is not a signature too, as
`malformed-method` or `malformed-func`.

`gowl conform` compares the code with the plan, both ways:

| Rule | Severity | Reports |
|---|---|---|
| `missing-type` | error | a type the plan names and the code does not have |
| `kind-mismatch` | error | a type that is another kind of type than the plan says: a struct where it says interface |
| `misplaced-type` | error | a type the code defines in another package than the plan says |
| `missing-field` | error | a field the plan names and the code does not have |
| `field-mismatch` | error | a field on another struct, or holding another type, than the plan says |
| `missing-interface` | error | a type that does not satisfy an interface the plan says it satisfies |
| `missing-package` | error | a package the plan names and the module does not have |
| `missing-import` | error | an import between packages that the plan names and the code does not have |
| `unplanned-type` | error | a type the change added that no plan names |
| `unplanned-package` | error | a package the change added that no plan names |
| `unplanned-field` | warning | a field the change added or retyped that no plan names |
| `unplanned-import` | warning | an import the change added that no plan names |
| `missing-method`, `missing-func` | error | a method the plan gives a type, or a function it gives a package, that the code does not declare |
| `method-mismatch`, `func-mismatch` | error | one the code declares with another signature than the plan's |
| `unplanned-method` | error on an interface, warning on any other type | a method the change added, or changed, that no plan names |
| `unplanned-func` | warning | a function the change added, or changed, that no plan names |
| `malformed-method`, `malformed-func` | error | one in a plan that is not a signature |

A plan is about the module's own structure, so the change's imports from outside
the module are not reported: what the module may depend on is a rule for the
[invariants](#invariants-that-outlive-a-change). A plan that does name one, as
`ObjectPropertyAssertion(:imports :package.gowl.store :package.strings)`, has it
checked.

Suppose the code leaves out `Amount` and adds a type nobody planned:

```
$ gowl conform
compared with main merge-base (c5bb468eaa49): 1 plan(s), 4 planned item(s) checked
  plans:     design/holdings.ofn

error: missing-field: the plan names field :store.Holding.Amount, and the code does not have it as an exported field of a module or basic type
error: unplanned-type: type :store.Memo is new, and no plan names it

2 finding(s)
```

Or suppose the plan has the four lines about `Holding` above, and the code gives
`Total` a second parameter and adds a function nobody planned:

```
$ gowl conform -fail-on warning
compared with main merge-base (246f55cd3a00): 1 plan(s), 8 planned item(s) checked
  plans:     design/holdings.ofn

error: func-mismatch: in the plan, :package.example.com.store has the function Total([]store.Holding) int, and the code has Total([]store.Holding, int) int
warning: unplanned-func: :package.example.com.store now offers the function Largest([]store.Holding) store.Holding, and no plan names it

2 finding(s)
```

The plans are the files under `design/` that differ from the base, or the ones
named with `-design`. The base defaults to where this branch left the default
branch, so a plan committed partway through a branch still counts. Once merged, a
plan is part of the base and is no longer checked: it records a decision, and
later changes write their own. For the same reason, a plan has to reach the
default branch together with its code, or be named with `-design`. A change that
adds nothing exported, such as most bug fixes, passes with no plan at all.

A plan can only say as much as `gocode` records:

- **Structure only.** What the code offers is in it by signature; what a
  function does is not, so a plan does not replace tests.
- **What is exported.** Types, some of their fields, methods and functions. A
  field of a map, a function or a type from outside the module is not recorded,
  nor is a constant or a variable, so a plan cannot name one and `conform` does
  not see one being added.
- **Additions only.** A plan says what should exist, so removing something is not
  reported. The invariants say what the code keeps offering, so that removing a
  function or a method they name is.
- **Go only.** `gocode` reads Go modules.

#### Holding an agent to the plan

Three pieces in [`.claude/`](.claude) make this the order Claude Code works in.
They run only in Claude Code sessions, so to hold everyone to a plan, also run
`gowl conform -fail-on warning` in CI.

- The [`design-first`](.claude/skills/design-first/SKILL.md) skill walks through
  writing the plan, checking it, building to it and verifying the result.
- [`.claude/hooks/design-gate.sh`](.claude/hooks/design-gate.sh) is a `PreToolUse`
  hook that refuses an `Edit` or `Write` adding an exported type no `design/*.ofn`
  file names. A bug fix adds no types, so it never notices one. It lets through
  anything it cannot read, and `GOWL_DESIGN_GATE=off` turns it off.
- [`.claude/hooks/conform-check.sh`](.claude/hooks/conform-check.sh) is a `Stop`
  hook that runs `gowl conform` when a turn ends with Go changes since the base,
  or since `GOWL_DESIGN_BASE` if that is set. It reports each finding once, as
  the sprawl hook does. A turn that only wrote a plan has nothing to compare yet,
  and a tree that does not build skips the check.

The gate sees only `Edit` and `Write`, and only types. Anything changed through a
shell command, and any field, method, function, import or package, is caught at
the end of the turn instead.

### Invariants that outlive a change

A plan is about one change, says only what should exist, and is retired once it
is merged. Some decisions are not like that: the core does no I/O, `owl` imports
nothing else in the module, every exported type is documented, the library keeps
offering `owl.ParseFunctional`. Those go in `design/invariants.ofn`, which is
never retired. `gowl invariants` checks the code against it, and `gowl plan`
checks every plan against it, so a plan that would break a rule fails while it
is still a few lines of a design.

The file says three kinds of thing: what may import what, how the code looks,
and what it offers.

```
Prefix(:=<https://example.com/shop#>)
Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)

Ontology(<https://example.com/shop/design/invariants>
    # Groups: facts the code cannot state.
    ClassAssertion(:Core :package.example.com.shop.core)
    ClassAssertion(:Core :package.example.com.shop.billing)
    ClassAssertion(:Edge :package.example.com.shop.api)
    ClassAssertion(:Effectful :package.os)
    ClassAssertion(:Effectful :package.net.http)

    # A derived property: imports, followed any distance.
    SubObjectPropertyOf(:imports :dependsOn)
    TransitiveObjectProperty(:dependsOn)

    # Layers.
    DisjointClasses(Annotation(rdfs:comment "The core does no I/O")
        :Core ObjectSomeValuesFrom(:dependsOn :Effectful))
    SubClassOf(Annotation(rdfs:comment "Every package is placed on purpose")
        :Package ObjectUnionOf(:Core :Edge))

    # Shape.
    SubClassOf(Annotation(rdfs:comment "Every exported type says what it is for")
        :Type :Documented)
    SubClassOf(Annotation(rdfs:comment "Interfaces belong to the core, and the edge implements them")
        :Interface ObjectSomeValuesFrom(:definedIn :Core))

    # A contract: these are the methods of core.Store, now and later.
    AnnotationAssertion(:method :core.Store "Get(id int) (Thing, error)")

    # Capabilities: what the shop keeps offering.
    AnnotationAssertion(:func :package.example.com.shop.core "Open(name string) (Store, error)")
    AnnotationAssertion(:func :Core "Version() string")
)
```

Suppose billing starts to use the api and drops its `Version`, and the api gains
an interface with no comment:

```
$ gowl invariants
checked against design/invariants.ofn: 8 rule(s)

error: invariant-violated: Every exported type says what it is for: :api.Hook is :Type, and :api.Hook is not :Documented
error: invariant-violated: Interfaces belong to the core, and the edge implements them: :api.Hook is :Interface, and :api.Hook has no :definedIn to anything that is :Core
error: invariant-violated: The core does no I/O: :package.example.com.shop.billing is :Core, and :package.example.com.shop.billing :dependsOn :package.net.http, which is :Effectful (through :package.example.com.shop.billing →:imports :package.example.com.shop.api →:imports :package.net.http)
error: missing-func: in the invariants, :package.example.com.shop.billing has the function Version() string, and the code does not declare it

4 finding(s)
```

The rule comes first, in its own words, then the package or type that breaks
it, and for the third the imports to blame. That is a counterexample, the way a
model checker gives one, and it is the shortest the facts allow.

**How it reads the file.** A reasoner reads axioms open-world: that a package is
not said to import `os` does not mean it does not. A description of code is
different. It is complete for what it records, so the check is closed-world:
what the code does not do, it does not do. Each axiom has one of five jobs:

- **The code's facts** are what the code says about its packages and its types,
  listed below. Only the code says so: an assertion in the file about one of
  the code's words, such as `:Package`, `:imports` or `:Documented`, is refused.
  To forbid a fact, use `NegativeObjectPropertyAssertion`.
- **Groups** are `ClassAssertion`s with a class of the file's own, such as
  `:Core`. A group can hold packages or types. `ClassAssertion(:ExternalPackage …)`
  declares a package the module does not depend on yet, so that a rule can
  forbid it.
- **Derived properties** come from `SubObjectPropertyOf` between named
  properties and `TransitiveObjectProperty`. They may only define a property
  the facts do not state, so a rule can define `:dependsOn` from `:imports`,
  but cannot add an import.
- **Rules** are `SubClassOf`, `DisjointClasses` and
  `NegativeObjectPropertyAssertion`, over named classes, `owl:Thing`,
  `owl:Nothing`, `ObjectIntersectionOf`, `ObjectUnionOf`, `ObjectComplementOf`,
  `ObjectOneOf`, `ObjectSomeValuesFrom`, `ObjectAllValuesFrom` and
  `ObjectHasValue`, with a property or its `ObjectInverseOf`.
- **Capabilities** are `:method` and `:func` annotations: what a type, a
  package or a group offers, by signature.

**The facts.** In the description `gocode` writes, a type is a class. A rule is
about things, so here each type is an individual under the same name, beside the
packages: `:core.Store` in a rule is the type `core.Store`.

| Fact | Says |
|---|---|
| `:Package`, `:ExternalPackage` | a package of the module, or one from outside it that the module imports |
| `:imports` | the subject package imports the object package directly |
| `:Type` | an exported named type of the module |
| `:Struct`, `:Interface` | what the type is underneath; a type of another kind is in neither |
| `:Documented` | a type with a doc comment, its own or its declaration group's, or a package with a package comment |
| `:definedIn` | the package that declares the type |
| `:implements` | an interface of the module that the type satisfies, directly or not |
| `:holds` | a type of the module that one of the type's exported fields holds, directly or through a pointer or slice |

`gowl invariants -facts` prints the facts for the module as it stands, with the
file's groups, as an ontology. That is the thing to read when a rule does not
say what you expected:

```
$ gowl invariants -facts
    ...
    ClassAssertion(:Core :package.example.com.shop.core)
    ClassAssertion(:Documented :core.Store)
    ClassAssertion(:Interface :api.Hook)
    ClassAssertion(:Type :api.Hook)
    ObjectPropertyAssertion(:definedIn :api.Hook :package.example.com.shop.api)
    ObjectPropertyAssertion(:imports :package.example.com.shop.billing :package.example.com.shop.api)
    ...
```

The subject of a `SubClassOf`, and each operand of a `DisjointClasses`, has to
be bounded by the facts: a named class, `ObjectOneOf`, `ObjectHasValue`,
`ObjectSomeValuesFrom`, or an intersection or union built from those.
`owl:Thing`, `ObjectComplementOf` and `ObjectAllValuesFrom` are refused there,
because who they include depends on which names happen to be mentioned, and a
rule over them could change its answer when an unrelated package is named.

**What the code offers.** A plan names a function or a method once, for the
change that adds it. The invariants name the ones the code keeps:

- `AnnotationAssertion(:func P "…")` on a package: `P` exports that function,
  with that signature.
- `AnnotationAssertion(:method T "…")` on a type: `T` has that method.
- On a group, such as `:Core` above, every package in it, or every type, has
  it. A bare type in the signature is read in each member's own package.

The code has to have each, as written, and may offer more. Two things offer
exactly what the file lists, so that one more is reported as well as one fewer:

- an interface the file gives methods to by name, rather than through a group,
  because every implementation depends on the whole of it;
- a package or a type the file puts in the group `:Fixed`. With nothing listed,
  it offers nothing.

```
    ClassAssertion(:Fixed :package.example.com.shop.core)
```

```
error: unplanned-func: the invariants fix the functions of :package.example.com.shop.core, and the code adds Close() error
```

An interface of the module that another embeds is not among that interface's
methods: it is something it `:implements`. So an interface whose methods are
fixed can still grow by embedding, unless a rule says what it may embed:

```
    SubClassOf(Annotation(rdfs:comment "core.Store embeds nothing of the module's")
        ObjectOneOf(:core.Store) ObjectAllValuesFrom(:implements owl:Nothing))
```

Only methods and functions are written as annotations here. What kind a type
is, and which package defines it, are facts, and holding a type to one is a
rule, such as `SubClassOf(ObjectOneOf(:core.Store) :Interface)`. A `:kind` or
`:definedIn` annotation, which is how a plan says the same, is refused in this
file rather than left unchecked.

| Rule | Severity | Reports |
|---|---|---|
| `invariant-violated` | error | a rule the code breaks, once for each package or type that breaks it |
| `unsupported-invariant` | error | an axiom outside what the checker reads, with the reason; it is an error because a rule nobody checks reads as if it held |
| `unknown-package` | error | a package that is no package of the module, the standard library, a dependency or a plan for the change, and is not declared an `:ExternalPackage`: usually a misspelling, which in a rule that forbids a package quietly forbids nothing |
| `unknown-type` | error | a type the file names that the module does not export, and no plan for the change names |
| `empty-term` | warning | a group or property the rules use that nothing defines, so they hold or fail for nothing |
| `missing-method`, `missing-func` | error | a method or function the file names that the code does not declare |
| `method-mismatch`, `func-mismatch` | error | one the code declares with another signature than the file's |
| `unplanned-method`, `unplanned-func` | error | one the code adds to an interface the file gives methods to, or to something `:Fixed` |
| `missing-type`, `missing-package` | error | methods or functions given to something that is neither a type or package of the module nor a group with anything in it |

**What it can and cannot see.** It sees packages and their direct imports;
types, with their kind, their documentation, their package, the interfaces they
satisfy and the types their fields hold; and methods and functions, by
signature. Imports are a proxy for effects, and a coarse one: `fmt` both formats
strings and prints them. Packages from outside the module have no imports of
their own in the facts, so `:dependsOn` follows the module's own packages and
stops at the first package outside it. Methods and functions are not facts a
rule can range over, only signatures to hold the code to. Constants, variables
and what a function does are not seen at all.

**The checker.** [`internal/icv`](internal/icv/icv.go) does the checking. It
knows nothing about Go and does no I/O; [`CheckInvariants`](internal/gocode/invariants.go)
gives it the facts and turns what it reports into findings. Its tests compare
it, on 60 random finite models, with the semantics written out the slow,
obvious way. They also check:

- that a universal is a negated existential of the negation, and that De
  Morgan holds;
- that the derived transitive property is exactly what a search reaches, with
  the shortest path as its proof;
- that every path is a chain of asserted facts;
- that naming an unrelated package changes no answer;
- that the order the rules are written in changes nothing.

As with the reasoner, the batch has to contain both broken and kept rules, so
the comparison cannot pass vacuously.

**gowl's own.** [`design/invariants.ofn`](design/invariants.ofn) holds the
following rules. About layers:

- every package is in the library, internal or the tools;
- `owl` imports nothing else in the module;
- the library never reaches into internal packages;
- the library and the pure internal packages never reach `os`, the network,
  `os/exec` or logging;
- `internal/icv`, the checker, builds on `owl` and the standard library alone.

About shape:

- every package has a package comment, and every exported type a doc comment;
- the library is `owl`, `el`, `lint`, `vocab` and the generated vocabularies,
  so a fifth hand-written package is decided in the file first;
- a generated vocabulary builds on `owl` and the standard library alone, and
  defines no types of its own;
- `internal/icv` exports two types, its report and the violations in it.

About what the code offers:

- the methods of `owl.Node` and `owl.Entity`, and the seal of `owl.Axiom`, are
  fixed, and of the module's interfaces they embed `owl.Node` and no other;
- `owl` keeps the functions that make, read, write, walk and compare an
  ontology, such as `New`, `ParseFunctional`, `Equal` and `DiffOntologies`, and
  `owl.Ontology` keeps `Add`, `All`, `Signature`, `Functional`,
  `WriteFunctional` and `Err`;
- `el` keeps `Classify`, and `el.Classification` its queries; `lint` keeps
  `Default` and `Run`; `vocab` keeps `All` and `ByPrefix`;
- every generated vocabulary offers `Ontology()` and `Vocabulary()`, and being
  `:Fixed`, nothing else;
- `internal/icv` offers `Validate`, and being `:Fixed`, nothing else.

```
$ gowl invariants
checked against design/invariants.ofn: 69 rule(s)

no findings
```

`TestOwnInvariantsHold` in [`examples/self`](examples/self/main_test.go) keeps
them in `go test ./...`.

[`.claude/hooks/invariants-check.sh`](.claude/hooks/invariants-check.sh) is a
`Stop` hook. When a turn ends with changes to the Go code or to the invariants
themselves, it runs `gowl invariants` and hands any finding back to the agent,
once.

### Asking questions of Kubernetes' API types

`gowl gocode` copies code into OWL, and a reasoner can do little with the
result. Go has no inheritance, so the classes have no hierarchy. Field domains
and ranges, package individuals and their imports are all outside OWL 2 EL, so
`gowl classify` ignores them.

[`examples/k8s`](examples/k8s/main.go) writes only structure a reasoner can
use, and reads the questions from a hand-written file:

- A field becomes an existential restriction over a shared property. Value
  fields use `embeds` or `hasField`, under a transitive `contains`. Pointers,
  slices and maps use `mayHave`, under a transitive `mayContain`. Fields of
  basic types are left out.
- A type's IRI is its pkg.go.dev URL, so `core/v1.Event` and `events/v1.Event`
  stay two classes. Each package gets a prefix such as `appsv1:`.
- Source is read syntactically, so nothing has to be type-checked or loaded.
  Generated files are skipped.

[`questions.ofn`](examples/k8s/questions.ofn) asks seven questions as defined
classes, and states one rule:

```
EquivalentClasses(:RunsPods ObjectIntersectionOf(:ObjectKind
    ObjectSomeValuesFrom(:contains corev1:PodSpec)))
DisjointClasses(:ObjectKind :ListKind)
```

On k8s.io/api v0.37.2 it reads 65 packages and 1,523 types, and skips 244
generated files. It takes about two seconds, and every axiom it writes is in
OWL 2 EL:

```bash
go mod download k8s.io/api@v0.37.2 k8s.io/apimachinery@v0.37.2
M=$(go env GOMODCACHE)
go run ./examples/k8s -questions examples/k8s/questions.ofn -answers -o k8s.ofn \
    $M/k8s.io/api@v0.37.2 $M/k8s.io/apimachinery@v0.37.2/pkg/apis/meta/v1
```

```
:ObjectKind (173)  includes :MayRunPods, :MountsSecrets, :RunsPods, :SelectsByLabel, :UsesStandardConditions
:ListKind (154)
:RunsPods (18)
:MayRunPods (19)  includes :RunsPods
:MountsSecrets (19)  includes :MayRunPods, :RunsPods
:SelectsByLabel (47)  includes :MayRunPods, :RunsPods
:UsesStandardConditions (24)
```

_(Member lists omitted.)_

- **Runs pods:** Deployment, StatefulSet, DaemonSet and ReplicaSet in every API
  version that has them, plus Job, CronJob, Pod and PodTemplate.
- **May run pods:** ReplicationController as well, because its `Template` is a
  `*PodTemplateSpec`.
- **Rule:** no type breaks it.

Some answers are inferences the reasoner made between the questions
themselves:

- **`:MayRunPods ⊑ :MountsSecrets`.** A pod spec reaches a Secret volume source
  through its volumes, so every kind that may run pods can mount a Secret.
- **`:MayRunPods ⊑ :SelectsByLabel`.** A pod spec also reaches a label selector
  through its affinity and topology-spread terms, so 19 of the 47 kinds in
  `:SelectsByLabel` are pod runners. The other 28 select objects themselves:
  admission policies, NetworkPolicy, PodDisruptionBudget,
  HorizontalPodAutoscaler and others. The question was written too broadly, and
  the answer shows it.

`gowl classify -explain` shows the field path behind any answer:

```
$ gowl classify -explain "batchv1:CronJob :RunsPods" k8s.ofn
batchv1:CronJob ⊑ :RunsPods follows from:
  ...
  SubClassOf(batchv1:CronJob ObjectSomeValuesFrom(:hasField batchv1:CronJobSpec))
  SubClassOf(batchv1:CronJobSpec ObjectSomeValuesFrom(:hasField batchv1:JobTemplateSpec))
  SubClassOf(batchv1:JobSpec ObjectSomeValuesFrom(:hasField corev1:PodTemplateSpec))
  SubClassOf(batchv1:JobTemplateSpec ObjectSomeValuesFrom(:hasField batchv1:JobSpec))
  SubClassOf(corev1:PodTemplateSpec ObjectSomeValuesFrom(:hasField corev1:PodSpec))
  SubObjectPropertyOf(:hasField :contains)
  TransitiveObjectProperty(:contains)
```

OWL's open world limits what can be asked. A question cannot be about something
being absent, such as "which kinds have no selector". It also cannot count
anything, or find cycles. [`main_test.go`](examples/k8s/main_test.go) runs the
real questions against a miniature of the API types under `testdata`.

## Profiles

`owl.CheckProfile(o, owl.ProfileEL)` returns every reason an ontology falls
outside a profile; `owl.Profiles(o)` returns the profiles it is in.

EL, QL and RL check the syntactic restrictions of the OWL 2 Profiles
specification, including the positional rules — QL and RL allow different class
expressions on the left and right of an inclusion, and the checker tracks which
side it is on. Datatype-map restrictions are not checked.

**DL is checked only partially**, and deliberately so: it covers the global
restriction on simple properties — a property that is transitive or implied by a
property chain may not be used in a cardinality restriction, a self restriction,
a disjointness axiom, or a functional, inverse-functional, irreflexive or
asymmetric assertion. That is the DL restriction that most often breaks
reasoners in practice. Typing separation, datatype cycles and anonymous
individual well-formedness are not checked, so a DL pass is not proof of DL
membership. `owl.NonSimpleProperties(o)` exposes the underlying computation.

## Diffing

`owl.DiffOntologies(from, to)` compares two ontologies by canonical form, so
reordering an axiom's operands, or the axioms themselves, is not a change:

```
- SubClassOf(:Child :Root)
+ SubClassOf(:Child owl:Thing)

1 axiom added, 1 axiom removed, affecting 3 entities
```

A modified axiom shows up as a removal plus an addition. OWL has no notion of
editing an axiom in place, and pairing them up would invent structure the
documents don't have.

Comparison is a multiset, so a document asserting an axiom twice differs from
one asserting it once. `Diff.Entities()` gives the terms touched by the change,
which is usually what a release note wants.

## Canonicalization

`owl.CanonicalAxiom` and `owl.CanonicalClassExpression` sort and de-duplicate
the operands of commutative constructs, flatten nested intersections and unions,
and collapse one-operand ones. Order is preserved where it carries meaning: the
sides of `SubClassOf`, the links of a property chain, the subject and object of
an assertion.

The canonical form is for comparison, not storage — it is deliberately not
round-trip faithful, so `Functional()` still renders axioms as asserted. Use
`gowl fmt -canonical` to normalize a file on purpose.

## What's covered

All six entity kinds; the full class expression grammar; object and data
property expressions and data ranges including `DatatypeRestriction` facets;
the OWL 2 axiom set — class axioms, property axioms and characteristics,
property chains, `HasKey`, individual assertions, and annotation axioms — plus
axiom annotations, a reader and writer for functional syntax, canonicalization,
diffing, profile checking, a lint rule engine, and the `gowl` command.

Not yet:

- **Other serializations.** Functional syntax only. `internal/rdf` reads Turtle
  and RDF/XML well enough to generate `vocab/`, but that is a generator
  detail, not an API, and there is no OWL/XML or Manchester syntax.
- **Reasoning beyond EL.** The classifier covers OWL 2 EL; there is no DL
  reasoning, no ABox reasoning, and no minimal justifications. See
  [Reasoning](#reasoning) for what it covers and what it reports as
  unsupported. Outside the reasoner, every query and lint rule still reports
  only what is asserted. The closed-world checker behind `gowl invariants` is
  internal, and covers a small fragment.
- **Full DL profile validation.** See Profiles above for exactly what is covered.
- **N-ary data ranges.** `DataSomeValuesFrom` takes a single data property.

## Layout

| File | Contents |
|---|---|
| `owl/iri.go` | `IRI`, the `Prefixes` table, package docs |
| `owl/entity.go` | the six entity types and the standard vocabulary |
| `owl/literal.go` | `Literal` and its constructors |
| `owl/expression.go` | class expressions, property expressions, data ranges, shorthands |
| `owl/axiom.go` | every axiom type |
| `owl/ontology.go` | the `Ontology` container and its queries |
| `owl/index.go` | the query index behind those queries |
| `owl/builder.go` | the fluent `Define*` layer |
| `owl/render.go` | functional-syntax rendering and `Equal` |
| `owl/lex.go` | functional-syntax tokenizer |
| `owl/parse.go` | functional-syntax parser |
| `owl/canon.go` | canonicalization for comparison |
| `owl/diff.go` | ontology diffing |
| `owl/profile.go` | OWL 2 profile checking |
| `owl/vocabulary.go` | closed vocabularies: `Prompt`, `Resolve`, `Validate` |
| `vocab/` | the generated standard vocabularies, and the index over them |
| `cmd/vocabgen/sources.go` | which vocabularies ship, and where they come from |
| `internal/rdf/` | Turtle and RDF/XML readers (generator only) |
| `internal/rdfowl/` | mapping RDF triples onto the structural model |
| `internal/vocabgen/` | writing a Go package for one vocabulary |
| `internal/gocode/` | describing a Go module's code as an ontology (`gowl gocode`), comparing two versions of it (`gowl sprawl`), comparing it with a plan (`gowl plan`, `gowl conform`), and checking it against its invariants (`gowl invariants`) |
| `internal/icv/` | checking facts against OWL axioms under the closed-world assumption, with counterexamples |
| `design/` | `invariants.ofn`, the rules the code keeps on every change; the plans for changes in progress; and a record of past ones |
| `.claude/hooks/` | Claude Code hooks: ontology checks on `.ofn` edits, a gate on exported types no plan names, and `gowl sprawl`, `gowl conform` and `gowl invariants` before a turn ends |
| `.claude/skills/` | Claude Code skills: adding an ontology term, and planning a Go change before writing it |
| `el/normalize.go` | rewriting axioms into EL normal form |
| `el/classify.go` | the completion rules and saturation |
| `el/query.go` | subsumption queries, taxonomy, explanations |
| `lint/` | the lint rule engine and built-in rules |
| `cmd/gowl/` | the command-line tool |
| `cmd/gowl/json.go` | the `-json` document shapes |
| `examples/self/` | this repository described as an ontology, linted by its own linter |
| `examples/k8s/` | Go type structure for an EL reasoner, with questions about Kubernetes' API types |
| `owl/testdata/` | corpus fixtures and their golden renderings |
| `owl/walk.go` | entity traversal, `Signature`, `References` |

```bash
go test ./...                                  # all tests
go test ./owl -bench . -benchtime 100x         # benchmarks
go test ./owl -run TestGoldenRoundTrip -update # regenerate golden files
go test ./owl -fuzz FuzzParseFunctional        # fuzz the parser
go generate ./vocab                            # refetch and regenerate vocab/
go run ./examples/self > examples/self/gowl.ofn # regenerate the self-description
```

`owl/testdata` holds loosely formatted fixtures next to their golden renderings,
so the tests pin both that the parser tolerates real-world formatting and that
the renderer's output does not drift.
