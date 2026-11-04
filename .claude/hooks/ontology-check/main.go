// Command ontology-check runs gowl's linter and EL reasoner over an ontology
// edit and reports what the edit changed: new lint findings, classes that
// became unsatisfiable, and subsumptions the reasoner now derives that nobody
// asserted.
//
// It has three modes:
//
//	ontology-check pre    < hook.json   snapshot a .ofn file before an edit
//	ontology-check post   < hook.json   compare the edited file with the snapshot
//	ontology-check compare before.ofn after.ofn
//
// pre and post are Claude Code PreToolUse/PostToolUse hooks. post blocks (asks
// the agent to fix the edit) when the file no longer parses, when a class
// becomes unsatisfiable, when the ontology becomes inconsistent, or when a new
// error-level lint finding appears; anything else it finds is passed back as
// context. compare prints the same report for a human or a skill, and exits 1
// when post would have blocked.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gowl/el"
	"gowl/lint"
	"gowl/owl"
	"gowl/vocab"
)

// maxListed caps every list in the report, so one sweeping edit cannot flood
// the agent's context.
const maxListed = 15

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "pre":
		os.Exit(runPre())
	case "post":
		os.Exit(runPost())
	case "compare":
		os.Exit(runCompare(os.Args[2:]))
	}
	usage()
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: ontology-check pre|post < hook.json\n       ontology-check compare before.ofn after.ofn")
	os.Exit(2)
}

// --- hook plumbing -----------------------------------------------------------

type hookInput struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

// readHook decodes the hook payload and returns the absolute path of the file
// the tool touched, or "" when it is not an ontology this tool checks.
func readHook() (hookInput, string) {
	var in hookInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		return in, ""
	}
	p := in.ToolInput.FilePath
	if !strings.EqualFold(filepath.Ext(p), ".ofn") {
		return in, ""
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(in.Cwd, p)
	}
	return in, filepath.Clean(p)
}

// snapshotPath is where pre leaves the file's previous content for post. The
// directory is per session so two sessions editing one file do not trade
// baselines.
func snapshotPath(session, file string) string {
	if session == "" {
		session = "nosession"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(file)))
	return filepath.Join(os.TempDir(), "gowl-ontology-check", session, hex.EncodeToString(sum[:8]))
}

// absentMarker is written instead of a snapshot when the file does not exist
// yet, so post can tell "new file" from "no snapshot taken".
const absentMarker = "\x00absent\x00"

func runPre() int {
	in, file := readHook()
	if file == "" {
		return 0
	}
	snap := snapshotPath(in.SessionID, file)
	if err := os.MkdirAll(filepath.Dir(snap), 0o755); err != nil {
		return 0 // a missing baseline only weakens post; never block the edit
	}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		data = []byte(absentMarker)
	} else if err != nil {
		return 0
	}
	_ = os.WriteFile(snap, data, 0o644)
	return 0
}

func runPost() int {
	in, file := readHook()
	if file == "" {
		return 0
	}
	after, err := os.ReadFile(file)
	if err != nil {
		return 0
	}

	before, baseline := baselineFor(in.SessionID, file)
	rel := displayPath(in.Cwd, file)
	rep := analyze(rel, before, baseline, after)

	out := map[string]any{}
	text := rep.String()
	if rep.blocking() {
		out["decision"] = "block"
		out["reason"] = text
		out["systemMessage"] = "ontology-check: " + rel + ": " + rep.headline()
	} else {
		out["hookSpecificOutput"] = map[string]any{
			"hookEventName":     "PostToolUse",
			"additionalContext": text,
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(out)
	return 0
}

// baselineFor returns the file's content before the edit and a description of
// where it came from. A nil result with a non-empty description means the file
// is new; with an empty one, no baseline could be found.
func baselineFor(session, file string) ([]byte, string) {
	snap := snapshotPath(session, file)
	if data, err := os.ReadFile(snap); err == nil {
		_ = os.Remove(snap)
		if string(data) == absentMarker {
			return nil, "new file"
		}
		return data, "before this edit"
	}
	// No snapshot (the pre hook did not run): fall back to the committed version.
	cmd := exec.Command("git", "-C", filepath.Dir(file), "show", "HEAD:./"+filepath.Base(file))
	if data, err := cmd.Output(); err == nil {
		return data, "git HEAD"
	}
	return nil, ""
}

func displayPath(cwd, file string) string {
	if cwd != "" {
		if r, err := filepath.Rel(cwd, file); err == nil && !strings.HasPrefix(r, "..") {
			return filepath.ToSlash(r)
		}
	}
	return filepath.ToSlash(file)
}

func runCompare(args []string) int {
	if len(args) != 2 {
		usage()
	}
	before, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ontology-check:", err)
		return 2
	}
	after, err := os.ReadFile(args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "ontology-check:", err)
		return 2
	}
	rep := analyze(filepath.ToSlash(args[1]), before, filepath.ToSlash(args[0]), after)
	fmt.Print(rep.String())
	if rep.blocking() {
		return 1
	}
	return 0
}

// --- analysis ----------------------------------------------------------------

type report struct {
	file     string
	baseline string // where "before" came from; "" when there was none

	parseErr       error
	baselineBroken bool // the previous version did not parse

	becameInconsistent bool
	newUnsat           []unsat
	stillUnsat         int

	newErrors   []lint.Finding
	newFindings []lint.Finding // warnings and info

	placed  []placement  // classes that did not exist before
	gained  []entailment // existing classes with new unasserted superclasses
	lost    []entailment // existing classes that lost a superclass
	nGained int
	nLost   int

	newUnsupported []string
	nUnsupported   int
}

type unsat struct {
	class string
	why   []string
}

type placement struct {
	class    string
	asserted []string // superclasses stated directly
	inferred []string // superclasses the reasoner adds on top
}

type entailment struct {
	sub, super string
	why        []string
}

func (r *report) blocking() bool {
	return r.parseErr != nil || r.becameInconsistent || len(r.newUnsat) > 0 || len(r.newErrors) > 0
}

func (r *report) headline() string {
	switch {
	case r.parseErr != nil:
		return "does not parse"
	case r.becameInconsistent:
		return "the ontology became inconsistent"
	case len(r.newUnsat) > 0:
		return fmt.Sprintf("%d class(es) became unsatisfiable", len(r.newUnsat))
	case len(r.newErrors) > 0:
		return fmt.Sprintf("%d new lint error(s)", len(r.newErrors))
	}
	return "ok"
}

func parse(data []byte) (*owl.Ontology, error) {
	return owl.ParseFunctional(bytes.NewReader(data))
}

func analyze(file string, beforeSrc []byte, baseline string, afterSrc []byte) *report {
	r := &report{file: file, baseline: baseline}

	after, err := parse(afterSrc)
	if err != nil {
		r.parseErr = err
		return r
	}
	var before *owl.Ontology
	if beforeSrc != nil {
		if before, err = parse(beforeSrc); err != nil {
			r.baselineBroken = true
			before = nil
		}
	}

	rules := append(lint.Default(), standardTermsRule())
	r.diffLint(before, after, rules)

	ca := el.Classify(after)
	var cb *el.Classification
	if before != nil {
		cb = el.Classify(before)
	}
	r.diffReasoning(before, after, cb, ca)
	return r
}

func (r *report) diffLint(before, after *owl.Ontology, rules []lint.Rule) {
	seen := map[string]bool{}
	if before != nil {
		for _, f := range lint.Run(before, rules) {
			seen[f.String()] = true
		}
	}
	for _, f := range lint.Run(after, rules) {
		if seen[f.String()] {
			continue
		}
		if f.Severity == lint.Error {
			r.newErrors = append(r.newErrors, f)
		} else {
			r.newFindings = append(r.newFindings, f)
		}
	}
}

func (r *report) diffReasoning(before, after *owl.Ontology, cb, ca *el.Classification) {
	render := func(n owl.Node) string { return after.Render(n) }
	explain := func(sub, super owl.Class) []string {
		var out []string
		for _, ax := range ca.Explain(sub, super) {
			out = append(out, render(ax))
		}
		return out
	}

	if !ca.IsConsistent() && (cb == nil || cb.IsConsistent()) {
		r.becameInconsistent = true
	}

	wasUnsat := map[owl.Class]bool{}
	if cb != nil {
		for _, c := range cb.UnsatisfiableClasses() {
			wasUnsat[c] = true
		}
	}
	for _, c := range ca.UnsatisfiableClasses() {
		if wasUnsat[c] {
			r.stillUnsat++
			continue
		}
		r.newUnsat = append(r.newUnsat, unsat{class: render(c), why: explain(c, owl.Nothing)})
	}

	asserted := assertedSubsumptions(after)
	existed := map[owl.Class]bool{}
	if before != nil {
		for _, c := range cb.Classes() {
			existed[c] = true
		}
	}

	for _, c := range ca.Classes() {
		if !ca.IsSatisfiable(c) {
			continue // reported above; its superclasses are vacuous
		}
		supers := ca.SuperClassesOf(c)
		if !existed[c] {
			if before == nil && r.baseline == "" {
				continue // no baseline: every class would look new
			}
			p := placement{class: render(c)}
			for _, s := range supers {
				if asserted[pair{c, s}] {
					p.asserted = append(p.asserted, render(s))
				} else {
					p.inferred = append(p.inferred, render(s))
				}
			}
			r.placed = append(r.placed, p)
			continue
		}
		for _, s := range supers {
			if cb.IsSubsumedBy(c, s) || asserted[pair{c, s}] {
				continue
			}
			r.nGained++
			if len(r.gained) < maxListed {
				r.gained = append(r.gained, entailment{sub: render(c), super: render(s), why: explain(c, s)})
			}
		}
		for _, s := range cb.SuperClassesOf(c) {
			if ca.IsSubsumedBy(c, s) {
				continue
			}
			r.nLost++
			if len(r.lost) < maxListed {
				r.lost = append(r.lost, entailment{sub: render(c), super: render(s)})
			}
		}
	}

	r.nUnsupported = len(ca.Unsupported())
	if cb != nil {
		old := map[string]bool{}
		for _, ax := range cb.Unsupported() {
			old[owl.CanonicalKey(ax)] = true
		}
		for _, ax := range ca.Unsupported() {
			if !old[owl.CanonicalKey(ax)] {
				r.newUnsupported = append(r.newUnsupported, render(ax))
			}
		}
	}
}

type pair struct{ sub, super owl.Class }

// assertedSubsumptions collects the named-class subsumptions an ontology
// states outright, so the report can separate what the author wrote from what
// the reasoner added.
func assertedSubsumptions(o *owl.Ontology) map[pair]bool {
	out := map[pair]bool{}
	for ax := range o.All() {
		switch x := owl.Unwrap(ax).(type) {
		case owl.SubClassOf:
			sub, ok1 := x.Sub.(owl.Class)
			super, ok2 := x.Super.(owl.Class)
			if ok1 && ok2 {
				out[pair{sub, super}] = true
			}
		case owl.EquivalentClasses:
			var named []owl.Class
			for _, ce := range x {
				if c, ok := ce.(owl.Class); ok {
					named = append(named, c)
				}
			}
			for _, a := range named {
				for _, b := range named {
					if a != b {
						out[pair{a, b}] = true
					}
				}
			}
		}
	}
	return out
}

// standardVocabularies mirrors gowl lint's list: the precise vocabularies
// first, schema.org last.
var standardVocabularies = []string{
	"rdf", "rdfs", "skos", "skosxl", "dcterms", "dc",
	"foaf", "prov", "org", "time", "dcat", "schema",
}

func standardTermsRule() lint.Rule {
	refs := make([]*owl.Vocabulary, 0, len(standardVocabularies))
	for _, name := range standardVocabularies {
		if e, ok := vocab.ByPrefix(name); ok {
			refs = append(refs, e.Vocabulary())
		}
	}
	return lint.PreferStandardTerms(refs...)
}

// --- output ------------------------------------------------------------------

func (r *report) String() string {
	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }

	w("ontology-check %s", r.file)
	if r.baseline != "" {
		w(" (compared with %s)", r.baseline)
	}
	w("\n")

	if r.parseErr != nil {
		w("\nBLOCKING: the file no longer parses:\n  %v\n", r.parseErr)
		return b.String()
	}
	if r.baselineBroken {
		w("note: the previous version did not parse, so everything is reported as new\n")
	}

	if r.becameInconsistent {
		w("\nBLOCKING: the ontology is now inconsistent (owl:Thing is unsatisfiable).\n")
	}
	if len(r.newUnsat) > 0 {
		w("\nBLOCKING: %d class(es) became unsatisfiable (equivalent to owl:Nothing):\n", len(r.newUnsat))
		for i, u := range r.newUnsat {
			if i == maxListed {
				w("  ... and %d more\n", len(r.newUnsat)-maxListed)
				break
			}
			w("  %s\n", u.class)
			writeWhy(&b, u.why)
		}
	}
	if len(r.newErrors) > 0 {
		w("\nBLOCKING: new lint errors:\n")
		writeFindings(&b, r.newErrors)
	}

	if len(r.placed) > 0 {
		w("\nNew classes and where the reasoner places them:\n")
		for i, p := range r.placed {
			if i == maxListed {
				w("  ... and %d more\n", len(r.placed)-maxListed)
				break
			}
			w("  %s\n", p.class)
			if len(p.asserted) > 0 {
				w("    asserted under: %s\n", strings.Join(p.asserted, ", "))
			}
			if len(p.inferred) > 0 {
				w("    inferred under: %s\n", strings.Join(p.inferred, ", "))
			}
			if len(p.asserted)+len(p.inferred) == 0 {
				w("    no superclass (sits directly under owl:Thing)\n")
			}
		}
	}
	if r.nGained > 0 {
		w("\nNew inferred subsumptions on existing classes (%d) - check each is intended:\n", r.nGained)
		for _, e := range r.gained {
			w("  %s SubClassOf %s\n", e.sub, e.super)
			writeWhy(&b, e.why)
		}
		if r.nGained > len(r.gained) {
			w("  ... and %d more\n", r.nGained-len(r.gained))
		}
	}
	if r.nLost > 0 {
		w("\nSubsumptions that no longer hold (%d):\n", r.nLost)
		for _, e := range r.lost {
			w("  %s SubClassOf %s\n", e.sub, e.super)
		}
		if r.nLost > len(r.lost) {
			w("  ... and %d more\n", r.nLost-len(r.lost))
		}
	}
	if len(r.newFindings) > 0 {
		w("\nNew lint findings:\n")
		writeFindings(&b, r.newFindings)
	}
	if len(r.newUnsupported) > 0 {
		w("\nNew axioms outside OWL 2 EL, which the reasoner skipped:\n")
		for i, ax := range r.newUnsupported {
			if i == maxListed {
				w("  ... and %d more\n", len(r.newUnsupported)-maxListed)
				break
			}
			w("  %s\n", ax)
		}
	}

	if !r.blocking() && len(r.placed) == 0 && r.nGained == 0 && r.nLost == 0 &&
		len(r.newFindings) == 0 && len(r.newUnsupported) == 0 {
		w("coherent; no new findings and no change to the inferred hierarchy\n")
	}
	if r.stillUnsat > 0 {
		w("\nnote: %d class(es) were already unsatisfiable before this change\n", r.stillUnsat)
	}
	if r.nUnsupported > 0 {
		w("note: %d axiom(s) are outside OWL 2 EL and were not reasoned over, so a clean result is not proof\n", r.nUnsupported)
	}
	return b.String()
}

func writeWhy(b *strings.Builder, why []string) {
	if len(why) == 0 {
		return
	}
	fmt.Fprintf(b, "    because:\n")
	for i, ax := range why {
		if i == 6 {
			fmt.Fprintf(b, "      ... and %d more axioms\n", len(why)-6)
			break
		}
		fmt.Fprintf(b, "      %s\n", ax)
	}
}

func writeFindings(b *strings.Builder, fs []lint.Finding) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Severity > fs[j].Severity })
	for i, f := range fs {
		if i == maxListed {
			fmt.Fprintf(b, "  ... and %d more\n", len(fs)-maxListed)
			break
		}
		fmt.Fprintf(b, "  %s\n", f)
	}
}
