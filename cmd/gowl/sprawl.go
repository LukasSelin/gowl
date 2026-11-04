package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"

	"gowl/internal/gocode"
	"gowl/lint"
	"gowl/owl"
)

// cmdSprawl compares a Go module's types with the same module at a base
// commit. It describes both versions with gocode and reports the types the
// change added beside the existing ones rather than through them; see
// gocode.Compare for the checks. The base version is copied out of git, so
// the working tree, the index and any other worktrees are left alone.
func cmdSprawl(args []string) int {
	fs := flag.NewFlagSet("sprawl", flag.ContinueOnError)
	base := fs.String("base", "HEAD", "the commit to compare with: a branch, tag or commit")
	failOn := fs.String("fail-on", "error", "lowest severity that fails the run: info, warning or error")
	seen := fs.String("seen", "", "leave out findings already listed in this file, and add the reported ones to it")
	jsonOut := fs.Bool("json", false, "write one JSON object to stdout instead of text")
	dirs, err := parseArgs(fs, args)
	if err != nil {
		return exitProblem
	}
	if len(dirs) > 1 {
		fmt.Fprintln(os.Stderr, "gowl sprawl: need at most one module directory")
		return exitProblem
	}
	root := "."
	if len(dirs) == 1 {
		root = dirs[0]
	}

	threshold, err := lint.ParseSeverity(*failOn)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	// The working tree first: when it does not build, there is no point
	// copying out the base.
	after, err := gocode.Build(root)
	if err != nil {
		return failWith(*jsonOut, err)
	}
	commit, before, err := buildAt(root, *base)
	if err != nil {
		return failWith(*jsonOut, err)
	}

	d := gocode.Compare(before, after)
	findings, already := d.Findings, 0
	if *seen != "" {
		if findings, already, err = unseen(*seen, findings); err != nil {
			return failWith(*jsonOut, err)
		}
	}
	worst, any := lint.MaxSeverity(findings)
	failed := any && worst >= threshold

	if *jsonOut {
		writeJSON(sprawlJSON(filepath.ToSlash(root), *base, commit, after, d, findings, already, threshold))
	} else {
		printSprawl(*base, commit, after, d, findings, already)
	}
	if failed {
		return exitFailed
	}
	return exitOK
}

func printSprawl(base, commit string, o *owl.Ontology, d gocode.Delta, findings []lint.Finding, already int) {
	short := commit
	if len(short) > 12 {
		short = short[:12]
	}
	fmt.Printf("compared with %s (%s): %d new type(s), %d changed\n", base, short, len(d.Added), len(d.Changed))
	listClasses("new", o, d.Added)
	listClasses("changed", o, d.Changed)
	fmt.Println()

	for _, f := range findings {
		fmt.Printf("%s: %s: %s\n", f.Severity, f.Rule, f.Message)
	}
	switch {
	case len(findings) == 0 && already > 0:
		fmt.Printf("no new findings (%d reported before)\n", already)
	case len(findings) == 0:
		fmt.Println("no findings")
	case already > 0:
		fmt.Printf("\n%d finding(s), and %d reported before\n", len(findings), already)
	default:
		fmt.Printf("\n%d finding(s)\n", len(findings))
	}
}

// listClasses prints one labelled line of class names, eliding a long tail.
func listClasses(label string, o *owl.Ontology, cs []owl.Class) {
	const shown = 10
	if len(cs) == 0 {
		return
	}
	names := make([]string, 0, shown)
	for i, c := range cs {
		if i == shown {
			names = append(names, fmt.Sprintf("and %d more", len(cs)-shown))
			break
		}
		names = append(names, o.Render(c))
	}
	fmt.Printf("  %-8s %s\n", label+":", strings.Join(names, ", "))
}

// buildAt describes the module in dir as it was at commit rev, returning the
// commit rev resolved to.
func buildAt(dir, rev string) (string, *owl.Ontology, error) {
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, err // most likely not a git repository
	}
	commit, err := git(dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	if err != nil {
		return "", nil, fmt.Errorf("-base %s: not a commit in the repository holding %s", rev, dir)
	}
	// Where the module sits in the repository, e.g. "tools/gen/", or "".
	prefix, err := git(dir, "rev-parse", "--show-prefix")
	if err != nil {
		return "", nil, err
	}

	tmp, err := os.MkdirTemp("", "gowl-sprawl-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmp)
	// The whole tree, not just the module: a replace directive may point at
	// a sibling directory.
	if err := archive(top, commit, tmp); err != nil {
		return "", nil, err
	}
	o, err := gocode.Build(filepath.Join(tmp, filepath.FromSlash(prefix)))
	if err != nil {
		return "", nil, fmt.Errorf("the module at %s: %w", rev, err)
	}
	return commit, o, nil
}

// git runs a git command in dir and returns its trimmed output.
func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("git %s: %s", args[0], msg)
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

// archive writes the tree of commit, from the repository at repo, into dir.
func archive(repo, commit, dir string) error {
	cmd := exec.Command("git", "-C", repo, "archive", "--format=tar", commit)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git archive: %w", err)
	}
	extractErr := untar(out, dir)
	// Drain what is left, or git blocks writing it and Wait never returns.
	_, _ = io.Copy(io.Discard, out)
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("git archive: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return extractErr
}

// untar extracts the directories and regular files of a tar stream into dir.
// Links and git's pax header carry nothing gocode reads, so they are skipped.
func untar(r io.Reader, dir string) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("reading git archive: %w", err)
		}
		name := filepath.FromSlash(path.Clean(h.Name))
		if !filepath.IsLocal(name) {
			return fmt.Errorf("git archive: entry %q is outside the tree", h.Name)
		}
		target := filepath.Join(dir, name)
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		}
	}
}

// unseen leaves out the findings file already lists, one per line as
// lint.Finding.String writes them, and appends the rest so that the next run
// leaves them out too. It returns the findings left and how many it dropped.
func unseen(file string, findings []lint.Finding) ([]lint.Finding, int, error) {
	data, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, 0, err
	}
	known := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		known[strings.TrimRight(line, "\r")] = true
	}

	var fresh []lint.Finding
	var add strings.Builder
	for _, f := range findings {
		s := f.String()
		if known[s] {
			continue
		}
		known[s] = true
		fresh = append(fresh, f)
		add.WriteString(s + "\n")
	}
	if add.Len() > 0 {
		out, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, 0, err
		}
		_, err = out.WriteString(add.String())
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, 0, err
		}
	}
	return fresh, len(findings) - len(fresh), nil
}
