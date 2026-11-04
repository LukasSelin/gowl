package main

import (
	"slices"
	"testing"

	"gowl/el"
	"gowl/owl"
)

// The fixture under testdata is a miniature of k8s.io/api and apimachinery's
// meta/v1, laid out under the real module paths, so the real questions.ofn
// runs against it unchanged.
var miniature = []string{"testdata/api", "testdata/apimachinery/pkg/apis/meta/v1"}

func TestAnswersOnMiniature(t *testing.T) {
	r, err := Build(miniature, "questions.ofn")
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range r.Warnings {
		t.Errorf("unexpected warning: %s", w)
	}
	if r.Generated != 1 {
		t.Errorf("skipped %d generated files, want 1", r.Generated)
	}

	violations, answers := Answers(r)
	if len(violations) > 0 {
		t.Errorf("violations = %v, want none", names(r, violations))
	}
	want := map[string][]string{
		":ObjectKind":             {"appsv1:Deployment", "corev1:Event", "corev1:Pod", "corev1:ReplicationController", "eventsv1:Event"},
		":ListKind":               {"corev1:List", "corev1:PodList", "metav1:List"},
		":RunsPods":               {"appsv1:Deployment", "corev1:Pod"},
		":MayRunPods":             {"appsv1:Deployment", "corev1:Pod", "corev1:ReplicationController"},
		":MountsSecrets":          {"appsv1:Deployment", "corev1:Pod", "corev1:ReplicationController"},
		":SelectsByLabel":         {"appsv1:Deployment"},
		":UsesStandardConditions": {"appsv1:Deployment"},
	}
	if len(answers) != len(want) {
		t.Fatalf("got %d answers, want %d", len(answers), len(want))
	}
	for _, a := range answers {
		q := names(r, []owl.Class{a.Question})[0]
		if got := names(r, a.Members); !slices.Equal(got, want[q]) {
			t.Errorf("%s = %v, want %v", q, got, want[q])
		}
	}
}

// Every axiom the generator writes is one the EL reasoner uses, and the
// document parses back.
func TestOutputIsEL(t *testing.T) {
	r, err := Build(miniature, "questions.ofn")
	if err != nil {
		t.Fatal(err)
	}
	if u := el.Classify(r.Ontology).Unsupported(); len(u) > 0 {
		t.Errorf("%d axioms outside OWL 2 EL, first %v", len(u), u[0])
	}
	if _, err := owl.ParseFunctionalString(r.Ontology.Functional()); err != nil {
		t.Fatalf("generated document does not parse back: %v", err)
	}
}

// The questions' DisjointClasses rule turns a type that is both an object kind
// and a list kind into an unsatisfiable class.
func TestRuleViolation(t *testing.T) {
	r, err := Build([]string{"testdata/broken", "testdata/apimachinery/pkg/apis/meta/v1"}, "questions.ofn")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) == 0 {
		t.Error("no warning for the questions' types missing from this code (corev1:PodSpec and others)")
	}
	violations, _ := Answers(r)
	if got := names(r, violations); !slices.Equal(got, []string{"broken:Both"}) {
		t.Errorf("violations = %v, want [broken:Both]", got)
	}
}

func names(r *Result, cs []owl.Class) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		s, ok := r.Ontology.Prefixes.Compact(owl.IRI(c))
		if !ok {
			s = string(c)
		}
		out[i] = s
	}
	slices.Sort(out)
	return out
}
