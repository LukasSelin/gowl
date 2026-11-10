# gowl

## Plan Go changes before writing them

Before adding or reshaping exported Go types, struct fields, interfaces, exported
functions and methods, packages or imports between packages, write the plan as
an ontology in `design/<change>.ofn`, check it with `gowl plan`, and then
implement to it. The `design-first` skill has the steps. A change that adds
nothing exported, such as most bug fixes, needs no plan.

`design/invariants.ofn` is different: it holds the rules the code keeps on every
change, and it is never retired. They say what may import what (layers, which
packages may do I/O), how the code looks (every exported type documented, which
packages there are, what a generated vocabulary may hold), and what it offers
(every function each package exports, the methods the library keeps, and the
interfaces whose methods are fixed). `gowl plan` checks each plan against it. A
planned type needs an `rdfs:comment` saying what it is for.

Every package is `:Fixed` there, and every group of packages is closed. So two
kinds of change start in `design/invariants.ofn`, before the plan:

- An exported function that is added, renamed, removed or given another
  signature: add or change its `:func` line.
- A new package: declare it, put it in a group and in `:Fixed`, add it to the
  rule that closes the group, and list its functions.

Tell the user what you changed there and why. Until the code is written,
`gowl invariants` reports the new line as `missing-func` or `missing-package`.
That is the file being ahead of the code, and it goes when the code catches up.

Hooks hold this in place:

- `design-gate` refuses an edit that adds an exported type no plan names.
- `conform-check` runs `gowl conform` when a turn ends and reports drift between
  the plan and the code.
- `invariants-check` runs `gowl invariants` when a turn ends and reports a rule
  the code breaks.

If a plan turns out to be wrong, change it and tell the user what changed and why.
Do not edit a plan only to silence a finding, and do not weaken an invariant to
get past one: changing a rule is a decision for the user to hear about.
