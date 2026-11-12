---
name: design-first
description: Plan a Go change as an OWL ontology (design/<change>.ofn) before writing the code, check the plan against the module with gowl, then implement to it and verify the code matches. Use before adding or reshaping exported Go types, struct fields, interfaces, exported functions and methods, packages or imports between packages, when asked to build or implement a feature, or when the design-gate hook refuses an edit. Skip it for a change that adds nothing exported, such as most bug fixes.
argument-hint: "<what the change should do>"
---

# Plan a Go change as an ontology, then build to it

The plan is written first because that is when it is cheap to be wrong. A type
that repeats an existing one is one line to delete in a plan, and a package to
unpick once the code exists. Afterwards the plan keeps the implementation
honest: `gowl conform` compares the two, so a type that crept in or a promise
that was dropped shows up instead of passing unnoticed.

A plan covers structure only: exported types, their fields, the interfaces they
satisfy, what the code offers by signature, which is the methods of types and
the functions of packages, and packages and the imports between them. It says
nothing about what a function does, so it does not replace tests.

Some decisions outlive a change: what the core may import, which layer a package
is in, how the types look, which functions and methods the code keeps offering.
Those are not in a plan but in `design/invariants.ofn`, which is checked on every
change and never retired.

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
| method `Price(item Item) (int, error)` on type `store.Priced` | `AnnotationAssertion(:method :store.Priced "Price(item Item) (int, error)")` |
| exported function `Total(hs []Holding) int` in package `gowl/internal/store` | `AnnotationAssertion(:func :package.gowl.internal.store "Total(hs []Holding) int")` |
| the type is an interface (or `"struct"`) | `AnnotationAssertion(:kind :store.Priced "interface")` |
| the type is declared in package `gowl/internal/store` | `AnnotationAssertion(:definedIn :store.Holding :package.gowl.internal.store)` |
| package `gowl/internal/store` | `ClassAssertion(:Package :package.gowl.internal.store)` |
| that package imports `gowl/owl` | `ObjectPropertyAssertion(:imports :package.gowl.internal.store :package.gowl.owl)` |

Strings are `xsd:string`, booleans `xsd:boolean`, integers `xsd:integer` and
floats `xsd:double`. A method or a function is written as Go writes it; parameter
names are optional, and a bare type of the package it is in, like `Item` above,
means `store.Item`. What a function calls the type parameters it declares is
yours to choose too. The parameters of a generic type are not: a method calls
them what the type's declaration does.

The annotations in the table describe a type or a package. They do not declare
it. A type the change adds needs its `Declaration(Class(...))` whatever else the
plan says about it, and a package it adds needs its `ClassAssertion(:Package ...)`.

Which methods and functions to name:

- every method an interface you add or change declares, unexported ones
  included;
- every exported method the change adds to any other type, or changes, except
  the ones an interface asks for: `SubClassOf(:store.Holding :store.Priced)`
  plans `Price` on `Holding`;
- every exported function the change adds or changes.

Leave out fields of maps, functions and types from outside the module, constants,
variables, and unexported anything: gocode records none of those, so a plan that
names them can never be satisfied. Leave out anything that is not in the table
too. A class expression, a `DisjointClasses` or a group is checked by nothing,
and the next step reports it as `unchecked-axiom`: a rule belongs in
`design/invariants.ofn`.

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
- `undeclared-entity`: usually a typo in a name. It is also what a `:method`,
  `:kind`, `:definedIn` or `:func` about a new type or package gets when the plan
  never declares that type or package.
- `punned-entity`: the plan uses a name as something else than the code does, such
  as an object property for what is a data field.
- `unchecked-axiom`: the plan states something nothing checks. Say it in the
  terms of the table above, or move it to `design/invariants.ofn` if it is a rule.
- `malformed-method`, `malformed-func`: a signature that does not read as one.
- `invariant-violated` and the other invariant rules: the plan would break a rule
  in `design/invariants.ofn`. The report says which rule, and why it exists.
  Change the plan. If the rule is what is wrong, change it in
  `design/invariants.ofn` and tell the user which rule and why; that is a
  decision about the module, not about this change.

The invariants are about types and what the code offers as well as about
packages, and the plan is read as though it were built, with what it says and no
more:

- A change that adds a package places it first, in `design/invariants.ofn` and
  not in the plan: a `ClassAssertion` there puts the new package in one of the
  file's groups, or the plan check reports it as unplaced. A group assertion in
  a plan is checked by nothing.
- A rule about types sees a planned type through the plan. Give it an
  `rdfs:comment` saying what it is for, and its `:kind` and `:definedIn`, and
  the rules about documentation, kinds and where types live are checked now
  rather than once the code exists.
- `unplanned-func` or `unplanned-method` from the invariants means the plan adds
  to something they fix: an interface whose methods they name, or a package or
  type in `:Fixed`. Adding to it is a change to `design/invariants.ofn` first.

In this repository the invariants fix every package and close every group of
packages, so that last step is part of any change to what a package exports.
Before the plan, in `design/invariants.ofn`:

- for an exported function that is added, renamed, removed or given another
  signature, add or change its `:func` line under the package;
- for a new package, declare it, put it in `:Library`, `:Internal` or `:Tool`
  and in `:Fixed`, add it to the rule that closes that group, and list its
  functions.

Then tell the user what you changed there and why. Until the code is written,
`gowl invariants` reports the new line as `missing-func`, or the new package as
`missing-package`. That is the file being ahead of the code, and it goes when
the code catches up.

`go run ./cmd/gowl invariants -facts` prints the facts the rules are checked
against, which is the thing to read when a rule does not say what you expected.

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
something nobody decided on, and `field-mismatch`, `method-mismatch`,
`func-mismatch`, `kind-mismatch` and `misplaced-type` are something that came out
differently. The same check runs when you end a turn, and reports each finding
once.

```bash
go run ./cmd/gowl invariants -fail-on warning
```

checks the code against `design/invariants.ofn`. Each `invariant-violated` finding
names the rule, the package or type that breaks it, and the imports to blame when
there are any. A `missing-func` or `missing-method` is something the invariants
say the code keeps offering, and it no longer does. It also runs when you end a
turn.

**When the change adds something the code should keep**, such as a method
other packages will be written against, say so in `design/invariants.ofn` with a
`:method` of its own, and tell the user. A function is there already: in this
repository its `:func` line came before the plan. A plan stops being checked
once it is merged; the invariants do not. Only methods and functions are
annotations there. What kind a type is, where it lives and what it embeds are
facts, and holding a type to one is a rule, such as
`SubClassOf(ObjectOneOf(:store.Priced) :Interface)`.

**When the plan was wrong**, change it, and tell the user what changed and why. A
plan that moved because of what you learned is fine. Editing one only to silence a
finding defeats the point, so if you cannot explain the change, fix the code.

## 7. Finish

Commit the plan with the code so that reviewers can see it. Once merged it stops
being checked: it is a record of a decision, and later changes write their own.
Report what was built, and anywhere the result differs from the first version of
the plan.
