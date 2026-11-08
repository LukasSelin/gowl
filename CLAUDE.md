# gowl

## Plan Go changes before writing them

Before adding or reshaping exported Go types, struct fields, interfaces, packages
or imports between packages, write the plan as an ontology in
`design/<change>.ofn`, check it with `gowl plan`, and then implement to it. The
`design-first` skill has the steps. A change that adds no exported structure,
such as most bug fixes, needs no plan.

Hooks hold this in place:

- `design-gate` refuses an edit that adds an exported type no plan names.
- `conform-check` runs `gowl conform` when a turn ends and reports drift between
  the plan and the code.

If a plan turns out to be wrong, change it and tell the user what changed and why.
Do not edit a plan only to silence a finding.
