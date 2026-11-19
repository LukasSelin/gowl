# Contributing to gowl

gowl is an OWL 2 library for Go. Contributions are welcome, including bug
reports, fixes, new syntaxes, new lint rules and wider coverage of OWL 2.

## Building and testing

gowl needs Go 1.25 or later and nothing outside the standard library. Keep it
that way: a program that imports gowl should get no other modules with it.

```bash
gofmt -l .
go vet ./...
go test ./...
```

CI runs the same checks on Linux and Windows.

## The rules every change keeps

[`design/invariants.ofn`](design/invariants.ofn) records how gowl's packages are
layered. On every pull request, CI checks the code against it with
[godesign](https://github.com/LukasSelin/godesign). In short:

- Every package belongs to a group: `Library`, `Internal` or `Tool`. A new
  package needs one `ClassAssertion` in the file putting it in one.
- `owl` imports nothing else from the module.
- The library imports only other library packages and packages from outside
  the module.
- The library and the pure internal packages do no I/O of their own, so no
  `os`, `net`, `log` and the like.
- The methods of `owl.Node`, `owl.Entity` and `owl.Axiom` change only after
  their contract in the file does.

A finding names the rule, the reason the rule exists, and the package that
breaks it. To run the check yourself:

```bash
go install github.com/LukasSelin/godesign/cmd/godesign@latest
godesign invariants
```

If the rule itself is what is wrong, change it in the same pull request and say
why.

## Planning a change first (optional)

gowl was built with a way of working in which a change's structure is written
down before the code. That structure is its exported types, fields, interfaces,
packages and imports. The plan is a small ontology, `design/<change>.ofn`, and
the code is checked against it afterwards.

You don't have to work this way. If your pull request includes a plan, CI runs
`godesign conform` and holds the code to it. If it includes none, CI skips that
check.

If you use Claude Code, the godesign plugin walks you through writing the plan
and checks the code against it as you go. It needs `godesign` on your `PATH`:

```bash
claude plugin marketplace add LukasSelin/godesign
claude plugin install godesign@godesign
```

godesign's README describes the plugin and how Go code maps to a plan.
