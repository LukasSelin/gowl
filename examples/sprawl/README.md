# Adding a feature to doppel without the sprawl

[doppel](https://github.com/LukasSelin/doppel) finds functions that already
exist. It does not look at the rest of a change: the packages it adds, the
imports between them, and the exported types it mints. Its README says why:
it measures code against the corpus's own practice, so "it cannot flag a
layering violation — that is a linter's job".

This walkthrough adds a feature to doppel with gowl doing that job. The tool
is `sprawl`, in this directory. It compares two
[`gowl gocode`](../../README.md#describing-a-go-module) descriptions of a
module, the base and the changed tree, against an architecture written in OWL.
It reports four things:

| Part | What it reports | Fails the gate? |
|---|---|---|
| footprint | packages, import edges, exported types and fields, added and removed | no |
| unplaced packages | a package the architecture does not put in a layer | when new |
| rule violations | a package the reasoner finds on both sides of a rule | when new |
| twins | a new type whose name or fields match an existing type | no, advisory |

The feature is `doppel analyze --format sarif`. It writes SARIF 2.1.0 so that
GitHub code scanning can show near-duplicate pairs as alerts. Two versions are
in [`doppel-sarif/`](doppel-sarif) as patches against doppel commit `9807c32`.
[`draft.patch`](doppel-sarif/draft.patch) is the version written the quick
way. [`final.patch`](doppel-sarif/final.patch) is the version after the gate.
Every output below comes from running these commands.

## Setup

From the gowl repository root, with doppel cloned beside it:

```bash
git clone https://github.com/LukasSelin/doppel ../doppel
git -C ../doppel checkout 9807c32
go install ./cmd/gowl
```

## 1. Before writing anything, look at what is there

Describe the base and check it on its own:

```bash
gowl gocode -o base.ofn ../doppel
go run ./examples/sprawl -arch examples/sprawl/doppel.ofn base.ofn
```

```
sprawl: github.com/LukasSelin/doppel — 29 packages, 107 import edges, 200 exported types

type twins already here (45), the 12 closest
  reporter.CorpusMetrics  ~ snapshot.CorpusMetrics  (same name, 8 of 8 fields: NNAtOrAboveThreshold, NNP50, NNP90, NNP99, NNScored, NNTotal, TotalNodes, UniqueSubtrees)
  cmd.Params              ~ snapshot.Params  (same name, 8 of 13 fields: Calibrate, ChannelK, Generated, MaxPerFunc, MinNodes, StructMin, TestsMode, Threshold)
  identity.Function       ~ snapshot.Unit  (6 of 6 fields: Digest, File, Key, Line, Name, Package)
  dashboard.PairRow       ~ identity.PairChange  (6 of 9 fields: A, B, Explain, MergeWorthy, Overlap, Score)
  analyzer.Retrieval      ~ retriever.Candidate  (6 of 7 fields: Call, CallSim, Concept, Shape, Total, TrophicSim)
  dashboard.Unit          ~ snapshot.Unit  (same name, 6 of 15 fields: File, Key, Line, Name, Package, Role)
  ...

architecture: holds
```

The architecture holds: every package is placed and no rule is broken. The
twins show the sprawl that is already there. Three types locate a function
by package, name, file and line: `snapshot.Unit`, `identity.Function` and
`dashboard.Unit`. `reporter.CorpusMetrics` repeats `snapshot.CorpusMetrics`
field for field. A SARIF result needs a file and a line for each side of a
pair, so the obvious design would add a fourth locator type.

## 2. The architecture

doppel declares no architecture, so [`doppel.ofn`](doppel.ofn) supplies one.
It was read off doppel's import graph and package docs, and has three parts.

**Layers.** Each package is placed in one layer:

| Layer | Packages |
|---|---|
| entrypoint | the root package, `cmd` |
| presentation | `reporter`, `dashboard` |
| engine: pipeline | `analyzer`, `snapshot`, `identity`, `retriever` and 13 more |
| engine: frontend | `gofront`, `lexfront` |
| engine: foundation | `syntax`, `canon`, `detmath`, `parallel`, `clique` |
| harness | `bench` |

**Reaches classes.** sprawl writes one axiom per import, and the architecture
defines what a package may not reach:

```
SubClassOf(internal:snapshot ObjectSomeValuesFrom(:imports internal:analyzer))   # written by sprawl
EquivalentClasses(:ReachesPresentation ObjectSomeValuesFrom(:dependsOn :Presentation))
```

**Rules.** Each rule is a disjointness. Its comment is the text the report
prints:

```
DisjointClasses(Annotation(rdfs:comment "Only the entrypoint imports the entrypoint.") :Library :ReachesEntrypoint)
DisjointClasses(Annotation(rdfs:comment "The engine never depends on presentation.") :Engine :ReachesPresentation)
DisjointClasses(Annotation(rdfs:comment "A frontend knows the IR, not the pipeline that reads it.") :Frontend :ReachesPipeline)
DisjointClasses(Annotation(rdfs:comment "Foundation packages import nothing from the module.") :Foundation :ReachesModule)
DisjointClasses(Annotation(rdfs:comment "Nothing shipped depends on the benchmark harness.") :Shipped :ReachesHarness)
```

Everything here is OWL 2 EL, so `el.Classify` decides it. `:imports` is a
subproperty of the transitive `:dependsOn`, so a rule about reaching a layer
covers every path to it.

## 3. The quick version

```bash
git -C ../doppel apply "$PWD/examples/sprawl/doppel-sarif/draft.patch"
```

The draft makes three decisions. Each one is easy to defend on its own:

- **SARIF gets its own package.** `internal/sarif` mirrors the SARIF schema
  as exported types (`Log`, `Run`, `Result`, `Location`, ...). It defines its
  own input model, `sarif.Function` and `sarif.Pair`, so that it does not
  depend on the rest of doppel.
- **Rule text sits beside the kinds.** Every pair kind, such as `diverged copy`
  or `thin wrappers`, becomes a SARIF rule. The rule text goes in
  `internal/analyzer`, next to the kind constants, so that a new kind cannot
  ship without one.
- **`cmd` converts.** A snapshot's pairs are turned into `[]sarif.Pair` and
  written out.

It builds, and `doppel analyze --format sarif .` writes a valid log. Run the
gate:

```bash
go run ./examples/sprawl -arch examples/sprawl/doppel.ofn base.ofn ../doppel
```

```
sprawl: github.com/LukasSelin/doppel

footprint
  packages  +1 −0     internal/sarif
  imports   +2 −0     cmd → internal/sarif, internal/analyzer → internal/sarif
  types     +13 −0    sarif.ArtifactLocation, sarif.Driver, sarif.Function, sarif.Location, sarif.Log, sarif.Message, sarif.Pair, sarif.PhysicalLocation, sarif.Region, sarif.Result, sarif.Rule, sarif.Run, and 1 more
  fields    +36 −0

unplaced packages (1) — place each in a layer of the architecture, or put its code in a package that has one
  internal/sarif  new

new types with twins (2) — reuse the existing type, or say why not
  sarif.Function  ~ identity.Function  (same name, 4 of 4 fields: File, Line, Name, Package)
                  ~ snapshot.Unit  (4 of 4 fields: File, Line, Name, Package)
                  ~ dashboard.Unit  (4 of 4 fields: File, Line, Name, Package)
  sarif.Pair      ~ snapshot.Pair  (same name, 3 of 6 fields: Kind, MergeWorthy, Score)

gate: failed (1 unplaced package)
```

The gate reports two problems:

- **The package has no layer.** The gate does not guess where a new package
  belongs. Someone has to decide, and the decision is a line in the
  architecture that a reviewer will see.
- **There are twins.** Every field of `sarif.Function` is already on
  `snapshot.Unit`, the type a snapshot's pairs point at. That is the fourth
  locator type from step 1. `sarif.Pair` repeats part of `snapshot.Pair`.

## 4. Place the package, and a rule fires

SARIF is an output format, so it belongs in presentation. Copy the
architecture to `placed.ofn`, and add one line to the copy next to
`SubClassOf(internal:dashboard :Presentation)`:

```
SubClassOf(internal:sarif :Presentation)
```

Then run the gate again. The `-o` flag also writes out the ontology the
reasoner used:

```bash
go run ./examples/sprawl -arch placed.ofn -o draft.ofn base.ofn ../doppel
```

```
rule violations (4)
  The engine never depends on presentation.
    internal/analyzer → internal/sarif  new
    internal/family → internal/analyzer → internal/sarif  new
    internal/snapshot → internal/analyzer → internal/sarif  new
    internal/identity → internal/snapshot → internal/analyzer → internal/sarif  new

gate: failed (4 new rule violations)
```

_(Footprint and twins omitted; they are as above.)_

The second decision in the draft put rule text in `analyzer`, which made the
engine import an output format. `analyzer` was not the only package affected.
Through it, `family`, `snapshot` and `identity` now depend on presentation as
well. Each line shows the path of imports that does it.

To see the axioms behind a line, ask `gowl classify` about that ontology:

```bash
gowl classify -explain "internal:analyzer owl:Nothing" draft.ofn
```

```
internal:analyzer ⊑ owl:Nothing follows from:
  DisjointClasses(Annotation(rdfs:comment "The engine never depends on presentation."^^xsd:string) :Engine :ReachesPresentation)
  EquivalentClasses(:ReachesPresentation ObjectSomeValuesFrom(:dependsOn :Presentation))
  SubClassOf(internal:analyzer :Pipeline)
  SubClassOf(internal:analyzer ObjectSomeValuesFrom(:imports internal:sarif))
  SubClassOf(internal:sarif :Presentation)
  SubClassOf(:Pipeline :Engine)
  SubObjectPropertyOf(:imports :dependsOn)
```

This also explains why sprawl checks rules itself instead of listing
unsatisfiable classes. With the disjointness axioms in place,
`gowl classify -unsatisfiable draft.ofn` names eight packages, not four. It
adds `reporter`, `cmd`, the root and `bench`. Those packages break no rule
themselves; they only import something that does, and an empty class makes
every class that requires it empty too. So sprawl classifies without the rules,
then checks each package against each rule.

## 5. Start over, and ask doppel about the functions

Take the draft back out:

```bash
git -C ../doppel apply -R "$PWD/examples/sprawl/doppel-sarif/draft.patch"
```

The rework will need a function that writes JSON. Before writing it, check
whether doppel already has one. doppel's own probe takes a draft of the
function on stdin, and lists the functions in the corpus that resemble it:

```bash
cd ../doppel
go run . query --near reporter . <<'EOF'
import (
	"encoding/json"
	"io"
)

func writeLog(w io.Writer, log any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(log)
}
EOF
cd -
```

```
#1  identity.WriteJSON  internal/identity/render.go:144
    evidence: 134.9 nats (shape 126.2, concept 3.6, call 5.1)  code-shape: 0.93  locality: 0.00
#2  identity.WriteJSONDelta  internal/identity/render.go:157
    evidence: 134.9 nats (shape 126.2, concept 3.6, call 5.1)  code-shape: 0.93  locality: 0.00
#3  reporter.encodeJSON  internal/reporter/json.go:31
    evidence: 118.3 nats (shape 109.6, concept 3.6, call 5.1)  code-shape: 0.77  locality: 0.00
```

_(Abridged.)_ `reporter.encodeJSON` is in the package the rework will use. Its
doc comment says every JSON write in the tool goes through it, so that two
outputs stay byte-comparable. The SARIF writer should use it too.

That is how the work divides in this walkthrough. doppel answers questions
about functions. sprawl answers questions about packages, imports and types.

## 6. The rework

```bash
git -C ../doppel apply "$PWD/examples/sprawl/doppel-sarif/final.patch"
```

Each finding changes one thing:

- **Placement.** There is no new package. SARIF is presentation, and doppel's
  presentation layer already exists: the writer is `reporter.PrintSARIF`, in
  `internal/reporter/sarif.go`.
- **The rule.** `analyzer` is not touched. The rule text sits in `reporter`,
  where [`kind.go`](https://github.com/LukasSelin/doppel/blob/9807c32/internal/reporter/kind.go)
  already renders every pair kind for the text report, the markdown report and
  the family census.
- **The twins.** The writer reads a `snapshot.Snapshot` directly. A side of a
  pair is a `snapshot.Unit`, and its key becomes the SARIF fingerprint. The
  SARIF wire structs are unexported: they are SARIF's shape, not doppel's, and
  nothing outside the file builds one.
- **The encoder.** The log goes through `encodeJSON`.

```bash
go run ./examples/sprawl -arch examples/sprawl/doppel.ofn base.ofn ../doppel
```

```
sprawl: github.com/LukasSelin/doppel

footprint
  packages  +0 −0
  imports   +0 −0
  types     +0 −0
  fields    +0 −0

gate: passed
```

Both versions do the same job, and the final one is not smaller. The draft
added 242 lines in five files. The final version adds 267 lines in four
files, 91 of them a test. What it does not add is structure. doppel's tests
pass on it. On doppel's own source, `doppel analyze --format sarif .` reports
20 pairs. The first is a diverged copy in `internal/identity`, located at
line 894, with its twin at line 899 as the related location.

## Keeping it in place

Either argument to sprawl can be a module directory, so in CI the base is a
worktree at the merge base. The module path in gowl's `go.mod` is plain
`gowl`, so build sprawl from a gowl checkout:

```bash
go build -o sprawl ./examples/sprawl                   # in gowl
git worktree add ../base "$(git merge-base origin/master HEAD)"
sprawl -arch architecture.ofn ../base .                # in the repository being changed
```

It exits 0 when the gate passes, 1 when it fails, and 2 on a usage or load
error. An existing violation is listed but does not fail the gate. The
change that would fix it is a different change. The architecture should live
in the repository it describes, next to the code, so that adding a layer goes
through review like any other edit.

## What it does not do

- **Only exported types.** gowl gocode describes what a package exports.
  Unexported types are private to their package, and the gate does not see
  them.
- **Fields are matched by name and type, not meaning.** Two types that hold
  the same data under different field names are not twins.
- **Twins are advisory.** Some new types should exist even when another type
  has the same fields. The report asks for a reason, but does not require one.
- **Rules are OWL 2 EL.** A rule is something a package must not reach. "A
  frontend imports only foundation packages" needs a universal restriction,
  which EL lacks, so it is written as "a frontend does not reach the
  pipeline".
- **One path per violation.** The imports shown are the ones in the
  derivation the reasoner found. There may be other paths.
- **No functions.** Duplicated functions are doppel's job.
