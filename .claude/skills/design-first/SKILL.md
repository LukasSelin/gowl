---
name: design-first
description: Plan a Go change as an OWL ontology (design/<change>.ofn) before writing the code, check the plan against the module with gowl, then implement to it and verify the code matches. Use before adding or reshaping exported Go types, struct fields, interfaces, packages or imports between packages, when asked to build or implement a feature, or when the design-gate hook refuses an edit. Skip it for a change that adds no exported structure, such as most bug fixes.
argument-hint: "<what the change should do>"
---

# Plan a Go change as an ontology, then build to it

The plan is written first because that is when it is cheap to be wrong. A type
that repeats an existing one is one line to delete in a plan, and a package to
unpick once the code exists. Afterwards the plan keeps the implementation
honest: `gowl conform` compares the two, so a type that crept in or a promise
that was dropped shows up instead of passing unnoticed.

A plan covers structure only: exported types, their fields, the interfaces they
satisfy, the methods of interfaces, packages, and imports between packages. It
says nothing about functions, other types' methods or behaviour, so it does not
replace tests.

Some decisions outlive a change: what the core may import, which layer a package
is in, which interfaces may not change. Those are not in a plan but in
`design/invariants.ofn`, which is checked on every change and never retired.

## 1. See what is already there

```bash
go run ./cmd/gowl gocode -o <scratch>/now.ofn .
```

Write that to the session scratchpad, not the repository. Search it for types
that could do the job. Its first lines also give the namespace your plan has to
use, in `Prefix(:=<...>)`.

## 2. Write the plan

Create `design/<change>.ofn` next to the module's `go.mod`, named after the
change. Name only what the change adds or alters, plus the existing types those
additions point at. Names are the ones `gowl gocode` writes:

| In Go | In the plan |
|---|---|
| exported type `store.Holding` | `Declaration(Class(:store.Holding))` |
| field holding a module type: `Item *Item` (also `[]Item`, `[]*Item`) | `Declaration(ObjectProperty(:store.Holding.Item))`, `ObjectPropertyDomain(:store.Holding.Item :store.Holding)`, `ObjectPropertyRange(:store.Holding.Item :store.Item)` |
| field holding a basic type: `Count int` | `Declaration(DataProperty(:store.Holding.Count))`, `DataPropertyDomain(:store.Holding.Count :store.Holding)`, `DataPropertyRange(:store.Holding.Count xsd:integer)` |
| satisfies module interface `store.Priced` | `SubClassOf(:store.Holding :store.Priced)` |
| interface method `Price(item Item) (int, error)` on `store.Priced` | `AnnotationAssertion(:method :store.Priced "Price(item Item) (int, error)")` |
| package `gowl/internal/store` | `ClassAssertion(:Package :package.gowl.internal.store)` |
| that package imports `gowl/owl` | `ObjectPropertyAssertion(:imports :package.gowl.internal.store :package.gowl.owl)` |

Strings are `xsd:string`, booleans `xsd:boolean`, integers `xsd:integer` and
floats `xsd:double`. A method is written as Go writes it; parameter names are
optional, and a bare type of the interface's own package, like `Item` above,
means `store.Item`. Name every method an interface you add or change declares,
unexported ones included. Leave out fields of maps, functions and types from outside
the module, and unexported anything: gocode records none of those, so a plan
that names them can never be satisfied.

Declare the existing types you mention too. An undeclared name is how the check
in the next step spots a typo.

```
# Why this change exists, in a sentence or two.
Prefix(:=<https://example.com/store#>)
Prefix(rdfs:=<http://www.w3.org/2000/01/rdf-schema#>)
Prefix(xsd:=<http://www.w3.org/2001/XMLSchema#>)

Ontology(<https://example.com/store/design/holdings>
    Declaration(Class(:store.Item))

    Declaration(Class(:store.Holding))
    AnnotationAssertion(rdfs:comment :store.Holding "What one warehouse has of an item.")

    Declaration(ObjectProperty(:store.Holding.Item))
    ObjectPropertyDomain(:store.Holding.Item :store.Holding)
    ObjectPropertyRange(:store.Holding.Item :store.Item)
)
```

The ontology-check hook lints and classifies the file each time you write it,
and blocks if it does not parse.

## 3. Check the plan

```bash
go run ./cmd/gowl plan -fail-on warning design/<change>.ofn
```

This reads the module as though the plan were already code:

- `same-shape-type`: a planned type repeats an existing one. Reuse or embed that
  type, or be ready to say why not.
- `new-orphan`: a planned type connects to nothing that exists. Often right for a
  helper, but check it is not a sign the design is off to one side.
- `undeclared-entity`: usually a typo in a name.
- `punned-entity`: the plan uses a name as something else than the code does, such
  as an object property for what is a data field.
- `invariant-violated` and the other invariant rules: the plan would break a rule
  in `design/invariants.ofn`. The report says which rule, and why it exists.
  Change the plan. If the rule is what is wrong, change it in
  `design/invariants.ofn` and tell the user which rule and why; that is a
  decision about the module, not about this change.

A plan that adds a package also places it: add a `ClassAssertion` putting the new
package in one of the groups in `design/invariants.ofn`, or the plan check reports
it as unplaced.

Fix the plan until you are happy with the result. Nothing has been built yet, so
this is the moment to change your mind.

## 4. Share the plan

Tell the user what the plan adds in plain terms: the types, how they connect to
what exists, and anything `gowl plan` flagged that you chose to keep. Carry on
without waiting unless they asked to review it first, or a choice is really
theirs to make, such as a new package.

## 5. Implement it

Write the code. Add doc comments as usual. The design-gate hook refuses an edit
that adds an exported type no plan names, which means the plan needs changing,
not the hook.

## 6. Verify

```bash
go run ./cmd/gowl conform -fail-on warning
```

It compares the plans under `design/` that differ from the base with the code,
both ways. A `missing-*` finding is a promise not kept, `unplanned-*` is
something nobody decided on, and `field-mismatch` and `method-mismatch` are
something that came out differently. The same check runs when you end a turn,
and reports each finding once.

```bash
go run ./cmd/gowl invariants -fail-on warning
```

checks the code against `design/invariants.ofn`. Each `invariant-violated` finding
names the rule, the package that breaks it, and the imports to blame. It also
runs when you end a turn.

**When the plan was wrong**, change it, and tell the user what changed and why. A
plan that moved because of what you learned is fine. Editing one only to silence a
finding defeats the point, so if you cannot explain the change, fix the code.

## 7. Finish

Commit the plan with the code so that reviewers can see it. Once merged it stops
being checked: it is a record of a decision, and later changes write their own.
Report what was built, and anywhere the result differs from the first version of
the plan.
