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
gowl plan    [-base ref] [-fail-on severity] [-json] plan.ofn [module-dir]
gowl conform [-base ref] [-design plan.ofn] [-fail-on severity] [-seen file] [-json] [module-dir]
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

Exit status is 0 on success, 1 when a check fails (findings at or above
`-fail-on`, or a non-empty diff under `-exit-code`), and 2 on a usage or parse
error — so `gowl lint`, `gowl conform` and `gowl diff -exit-code` drop straight
into CI.

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
- Packages become `:Package` individuals, linked by `:imports`.

```bash
go run ./examples/self > examples/self/gowl.ofn
gowl lint -fail-on info examples/self/gowl.ofn
```

```
info: orphan-class: class :el.Classification has no asserted superclass or equivalence
info: orphan-class: class :lint.Rule has no asserted superclass or equivalence
info: orphan-class: class :owl.ClassBuilder has no asserted superclass or equivalence
...
22 finding(s)
```

_(IRIs abbreviated here; the CLI prints them in full.)_

None of these findings is a warning or an error. The 22 `orphan-class` notes
are the types that satisfy no interface in the module: the builders, option
structs, `owl.Index`, and the root interfaces `owl.Node` and `rdf.Term`. They
are accurate, and at `info` they do not fail the default run.

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
classes             115
object properties   128
data properties     33
individuals         22

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

The checks see only what `gocode` records:

- exported types;
- fields whose type is a module type (directly, or through a pointer or slice)
  or a basic type.

Maps, functions and methods are not recorded. A type that is only ever a
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

`gowl conform` compares the code with the plan, both ways:

| Rule | Severity | Reports |
|---|---|---|
| `missing-type` | error | a type the plan names and the code does not have |
| `missing-field` | error | a field the plan names and the code does not have |
| `field-mismatch` | error | a field on another struct, or holding another type, than the plan says |
| `missing-interface` | error | a type that does not satisfy an interface the plan says it satisfies |
| `missing-package` | error | a package the plan names and the module does not have |
| `missing-import` | error | an import between packages that the plan names and the code does not have |
| `unplanned-type` | error | a type the change added that no plan names |
| `unplanned-package` | error | a package the change added that no plan names |
| `unplanned-field` | warning | a field the change added or retyped that no plan names |
| `unplanned-import` | warning | an import the change added that no plan names |

Suppose the code leaves out `Amount` and adds a type nobody planned:

```
$ gowl conform
compared with main merge-base (c5bb468eaa49): 1 plan(s), 4 planned item(s) checked
  plans:     design/holdings.ofn

error: missing-field: the plan names field :store.Holding.Amount, and the code does not have it as an exported field of a module or basic type
error: unplanned-type: type :store.Memo is new, and no plan names it

2 finding(s)
```

The plans are the files under `design/` that differ from the base, or the ones
named with `-design`. The base defaults to where this branch left the default
branch, so a plan committed partway through a branch still counts. Once merged, a
plan is part of the base and is no longer checked: it records a decision, and
later changes write their own. For the same reason, a plan has to reach the
default branch together with its code, or be named with `-design`. A change that
adds no exported structure, such as most bug fixes, passes with no plan at all.

A plan can only say as much as `gocode` records:

- **Structure only.** Methods, functions and behaviour are outside it, so a plan
  does not replace tests.
- **Exported types and some of their fields.** A field of a map, a function or a
  type from outside the module is not recorded, so a plan cannot name one and
  `conform` does not see one being added.
- **Additions only.** A plan says what should exist, so removing something is not
  reported.
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
shell command, and any field, import or package, is caught at the end of the turn
instead.

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
  only what is asserted.
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
| `internal/gocode/` | describing a Go module's code as an ontology (`gowl gocode`), comparing two versions of it (`gowl sprawl`), and comparing it with a plan (`gowl plan`, `gowl conform`) |
| `design/` | the plans for changes in progress, and a record of past ones |
| `.claude/hooks/` | Claude Code hooks: ontology checks on `.ofn` edits, a gate on exported types no plan names, and `gowl sprawl` and `gowl conform` before a turn ends |
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
