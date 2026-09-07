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

## Rendering

`owl.Functional(x)` renders any construct in OWL 2 Functional-Style Syntax with
full IRIs. `o.Functional()` renders a whole document, abbreviating IRIs with the
ontology's prefixes; `o.WriteFunctional(w)` streams it to an `io.Writer`.

`String()` follows two conventions: IRIs and entities print as their bare IRI
(so `fmt.Println(pizza)` is readable), while expressions, axioms and ontologies
print as functional syntax.

## What's covered

All six entity kinds; the full class expression grammar; object and data
property expressions and data ranges including `DatatypeRestriction` facets;
and the OWL 2 axiom set — class axioms, property axioms and characteristics,
property chains, `HasKey`, individual assertions, and annotation axioms.

Not yet:

- **Parsing.** Rendering is one-way; there is no reader for Turtle, RDF/XML or
  functional syntax.
- **Reasoning.** No classification, satisfiability or entailment.
- **Axiom annotations.** `Annotation(...)` nested inside an axiom is not
  modelled; annotate the entity with `AnnotationAssertion` instead.
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
| `owl/builder.go` | the fluent `Define*` layer |
| `owl/render.go` | functional-syntax rendering and `Equal` |
| `owl/walk.go` | entity traversal, `Signature`, `References` |

```bash
go test ./...
```
