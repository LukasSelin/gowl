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

## The `gowl` command

```bash
go build ./cmd/gowl
```

```
gowl lint    [-disable rules] [-fail-on severity] [-list] [-json] file.ofn
gowl diff    [-summary] [-exit-code] [-json] old.ofn new.ofn
gowl profile [-v] [-json] file.ofn
gowl fmt     [-w] [-canonical] file.ofn
gowl stats   [-json] file.ofn
```

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

- **Other serializations.** No Turtle, RDF/XML, OWL/XML or Manchester syntax;
  functional syntax only.
- **Reasoning.** No classification, satisfiability or entailment. Every query
  and rule reports what is asserted.
- **Full DL profile validation.** See Profiles above for exactly what is covered.
- **N-ary data ranges.** `DataSomeValuesFrom` takes a single data property.
- **Profile validation.** Nothing checks whether an ontology stays inside OWL 2
  DL, EL, QL or RL.

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
```

`owl/testdata` holds loosely formatted fixtures next to their golden renderings,
so the tests pin both that the parser tolerates real-world formatting and that
the renderer's output does not drift.
