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
```

`classify` runs the EL reasoner. `-axioms` writes the inferred taxonomy as an
ontology document, `-explain` justifies one subsumption, and
`-fail-on-incoherent` exits 1 when a class cannot have instances - which is the
form to put in CI.

Exit status is 0 on success, 1 when a check fails (lint findings at or above
`-fail-on`, or a non-empty diff under `-exit-code`), and 2 on a usage or parse
error — so `gowl lint` and `gowl diff -exit-code` drop straight into CI.

### JSON output

Every command except `fmt` takes `-json` and then writes exactly one JSON object
to stdout. `fmt` has none because its output is an ontology document, not a
report.

```bash
gowl lint -json onto.ofn | jq -r '.findings[] | select(.severity=="error") | .message'
gowl diff -json old.ofn new.ofn | jq '.counts'
gowl profile -json onto.ofn | jq -r '.in_profiles[]'
gowl stats -json onto.ofn | jq '.counts.classes'
gowl lint -list -json | jq -r '.rules[].name'
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
| `el/normalize.go` | rewriting axioms into EL normal form |
| `el/classify.go` | the completion rules and saturation |
| `el/query.go` | subsumption queries, taxonomy, explanations |
| `lint/` | the lint rule engine and built-in rules |
| `cmd/gowl/` | the command-line tool |
| `cmd/gowl/json.go` | the `-json` document shapes |
| `owl/testdata/` | corpus fixtures and their golden renderings |
| `owl/walk.go` | entity traversal, `Signature`, `References` |

```bash
go test ./...                                  # all tests
go test ./owl -bench . -benchtime 100x         # benchmarks
go test ./owl -run TestGoldenRoundTrip -update # regenerate golden files
go test ./owl -fuzz FuzzParseFunctional        # fuzz the parser
go generate ./vocab                            # refetch and regenerate vocab/
```

`owl/testdata` holds loosely formatted fixtures next to their golden renderings,
so the tests pin both that the parser tolerates real-world formatting and that
the renderer's output does not drift.
