# gowl

## Plan Go changes before writing them

Before adding or reshaping exported Go types, struct fields, interfaces, packages
or imports between packages, write the plan as an ontology in
`design/<change>.ofn`, check it with `gowl plan`, and then implement to it. The
`design-first` skill has the steps. A change that adds no exported structure,
such as most bug fixes, needs no plan.

`design/invariants.ofn` is different: it holds the rules the code keeps on every
change (layers, which packages may do I/O, interfaces whose methods are fixed),
and it is never retired. `gowl plan` checks each plan against it. A new package
has to be placed in one of its groups.

Hooks hold this in place:

- `design-gate` refuses an edit that adds an exported type no plan names.
- `conform-check` runs `gowl conform` when a turn ends and reports drift between
  the plan and the code.
- `invariants-check` runs `gowl invariants` when a turn ends and reports a rule
  the code breaks.

If a plan turns out to be wrong, change it and tell the user what changed and why.
Do not edit a plan only to silence a finding, and do not weaken an invariant to
get past one: changing a rule is a decision for the user to hear about.
