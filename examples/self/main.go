// Command self describes gowl's own Go code as an OWL 2 ontology, so that the
// repository's linter has a real, non-toy ontology of this repository to run on.
// Run it from the repository root:
//
//	go run ./examples/self > examples/self/gowl.ofn
//	go run ./cmd/gowl lint examples/self/gowl.ofn
//
// The generator itself lives in internal/gocode, and "gowl gocode" runs it on
// any other Go module. See that package for the mapping.
package main

import (
	"bufio"
	"fmt"
	"os"

	"gowl/internal/gocode"
)

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	o, err := gocode.Build(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "self:", err)
		os.Exit(1)
	}
	w := bufio.NewWriter(os.Stdout)
	if err := o.WriteFunctional(w); err != nil {
		fmt.Fprintln(os.Stderr, "self:", err)
		os.Exit(1)
	}
	if err := w.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "self:", err)
		os.Exit(1)
	}
}
