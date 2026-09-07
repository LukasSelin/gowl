package owl_test

import (
	"strings"
	"testing"

	"gowl/owl"
)

// profileDoc wraps axioms in a minimal document with the default prefix bound.
func profileDoc(t *testing.T, axioms ...string) *owl.Ontology {
	t.Helper()
	src := "Prefix(:=<http://example.org/o#>)\nOntology(\n    " +
		strings.Join(axioms, "\n    ") + "\n)"
	return mustParse(t, src)
}

func inProfile(t *testing.T, o *owl.Ontology, p owl.Profile) bool {
	t.Helper()
	return len(owl.CheckProfile(o, p)) == 0
}

func TestELAccepts(t *testing.T) {
	o := profileDoc(t,
		"SubClassOf(:A ObjectIntersectionOf(:B ObjectSomeValuesFrom(:p :C)))",
		"SubObjectPropertyOf(ObjectPropertyChain(:p :q) :r)",
		"TransitiveObjectProperty(:p)",
		"ReflexiveObjectProperty(:q)",
		"DisjointClasses(:A :B)",
		"ObjectPropertyRange(:p :C)",
		"NegativeObjectPropertyAssertion(:p :a :b)",
		"HasKey(:A (:p) ())",
		"ClassAssertion(ObjectHasSelf(:p) :a)",
	)
	if v := owl.CheckProfile(o, owl.ProfileEL); len(v) != 0 {
		t.Errorf("expected EL, got violations: %v", v)
	}
}

func TestELRejects(t *testing.T) {
	cases := []struct {
		name, axiom, want string
	}{
		{"union", "SubClassOf(:A ObjectUnionOf(:B :C))", "ObjectUnionOf is not an EL class expression"},
		{"complement", "SubClassOf(:A ObjectComplementOf(:B))", "ObjectComplementOf is not an EL class expression"},
		{"universal", "SubClassOf(:A ObjectAllValuesFrom(:p :B))", "ObjectAllValuesFrom is not an EL class expression"},
		{"cardinality", "SubClassOf(:A ObjectMinCardinality(2 :p))", "ObjectMinCardinality is not an EL class expression"},
		{"inverse", "SubClassOf(:A ObjectSomeValuesFrom(ObjectInverseOf(:p) :B))", "EL has no inverse properties"},
		{"functional", "FunctionalObjectProperty(:p)", "FunctionalObjectProperty is not an EL axiom"},
		{"symmetric", "SymmetricObjectProperty(:p)", "SymmetricObjectProperty is not an EL axiom"},
		{"disjoint union", "DisjointUnion(:A :B :C)", "DisjointUnion is not an EL axiom"},
		{"inverse properties", "InverseObjectProperties(:p :q)", "InverseObjectProperties is not an EL axiom"},
		{"multi oneOf", "SubClassOf(:A ObjectOneOf(:a :b))", "exactly one individual"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := owl.CheckProfile(profileDoc(t, tc.axiom), owl.ProfileEL)
			if len(v) == 0 {
				t.Fatalf("%s should not be in EL", tc.axiom)
			}
			if !strings.Contains(v[0].Reason, tc.want) {
				t.Errorf("reason %q should mention %q", v[0].Reason, tc.want)
			}
		})
	}
}

func TestQLPositionSensitivity(t *testing.T) {
	// An unqualified existential is fine on the left.
	ok := profileDoc(t, "SubClassOf(ObjectSomeValuesFrom(:p owl:Thing) :B)")
	if !inProfile(t, ok, owl.ProfileQL) {
		t.Errorf("should be QL: %v", owl.CheckProfile(ok, owl.ProfileQL))
	}
	// A qualified one is not.
	bad := profileDoc(t, "SubClassOf(ObjectSomeValuesFrom(:p :C) :B)")
	v := owl.CheckProfile(bad, owl.ProfileQL)
	if len(v) == 0 || !strings.Contains(v[0].Reason, "owl:Thing") {
		t.Errorf("qualified existential in subclass position should violate QL, got %v", v)
	}
	// But it is allowed on the right.
	right := profileDoc(t, "SubClassOf(:B ObjectSomeValuesFrom(:p :C))")
	if !inProfile(t, right, owl.ProfileQL) {
		t.Errorf("should be QL: %v", owl.CheckProfile(right, owl.ProfileQL))
	}
}

func TestQLRejects(t *testing.T) {
	for _, tc := range []struct{ name, axiom string }{
		{"chain", "SubObjectPropertyOf(ObjectPropertyChain(:p :q) :r)"},
		{"transitive", "TransitiveObjectProperty(:p)"},
		{"functional", "FunctionalObjectProperty(:p)"},
		{"key", "HasKey(:A (:p) ())"},
		{"same individual", "SameIndividual(:a :b)"},
		{"complex class assertion", "ClassAssertion(ObjectSomeValuesFrom(:p :C) :a)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if inProfile(t, profileDoc(t, tc.axiom), owl.ProfileQL) {
				t.Errorf("%s should not be in QL", tc.axiom)
			}
		})
	}
}

func TestRLPositionSensitivity(t *testing.T) {
	// Union is legal on the left, not on the right.
	left := profileDoc(t, "SubClassOf(ObjectUnionOf(:A :B) :C)")
	if !inProfile(t, left, owl.ProfileRL) {
		t.Errorf("union in subclass position should be RL: %v", owl.CheckProfile(left, owl.ProfileRL))
	}
	right := profileDoc(t, "SubClassOf(:C ObjectUnionOf(:A :B))")
	if inProfile(t, right, owl.ProfileRL) {
		t.Error("union in superclass position should not be RL")
	}

	// Max cardinality is capped at 1.
	if !inProfile(t, profileDoc(t, "SubClassOf(:A ObjectMaxCardinality(1 :p))"), owl.ProfileRL) {
		t.Error("max cardinality 1 should be RL")
	}
	if inProfile(t, profileDoc(t, "SubClassOf(:A ObjectMaxCardinality(2 :p))"), owl.ProfileRL) {
		t.Error("max cardinality 2 should not be RL")
	}
}

func TestNonSimplePropertiesPropagate(t *testing.T) {
	o := profileDoc(t,
		"TransitiveObjectProperty(:partOf)",
		"SubObjectPropertyOf(:partOf :relatedTo)",
		"SubObjectPropertyOf(ObjectPropertyChain(:a :b) :composed)",
		"Declaration(ObjectProperty(:simple))",
	)
	nonSimple := owl.NonSimpleProperties(o)

	for _, p := range []string{"partOf", "relatedTo", "composed"} {
		if !nonSimple[owl.ObjectProperty("http://example.org/o#"+p)] {
			t.Errorf("%s should be non-simple", p)
		}
	}
	if nonSimple[owl.ObjectProperty("http://example.org/o#simple")] {
		t.Error("simple should be simple")
	}
}

func TestDLSimplePropertyRestriction(t *testing.T) {
	// A transitive property in a cardinality restriction is the classic DL
	// violation.
	o := profileDoc(t,
		"TransitiveObjectProperty(:partOf)",
		"SubClassOf(:A ObjectMaxCardinality(1 :partOf))",
	)
	v := owl.CheckProfile(o, owl.ProfileDL)
	if len(v) == 0 {
		t.Fatal("a transitive property in a cardinality restriction should violate DL")
	}
	if !strings.Contains(v[0].Reason, "not simple") {
		t.Errorf("reason = %q", v[0].Reason)
	}

	// The same restriction on a simple property is fine.
	ok := profileDoc(t, "SubClassOf(:A ObjectMaxCardinality(1 :hasPart))")
	if !inProfile(t, ok, owl.ProfileDL) {
		t.Errorf("simple property should pass DL: %v", owl.CheckProfile(ok, owl.ProfileDL))
	}
}

func TestDLNonSimpleInheritedThroughChain(t *testing.T) {
	// composed is non-simple because of the chain; asserting it functional is a
	// DL violation even though nothing says composed is transitive.
	o := profileDoc(t,
		"SubObjectPropertyOf(ObjectPropertyChain(:a :b) :composed)",
		"FunctionalObjectProperty(:composed)",
	)
	if inProfile(t, o, owl.ProfileDL) {
		t.Error("a chain-implied property may not be functional in DL")
	}
}

func TestProfilesOfRealisticOntology(t *testing.T) {
	o, _ := pizza()
	got := owl.Profiles(o)

	// The pizza ontology uses ObjectComplementOf and ObjectAllValuesFrom, so it
	// is outside EL and QL, but nothing in it breaks the DL simple-property
	// restriction.
	for _, p := range got {
		if p == owl.ProfileEL || p == owl.ProfileQL {
			t.Errorf("pizza should not be in %s", p)
		}
	}
	var hasDL bool
	for _, p := range got {
		if p == owl.ProfileDL {
			hasDL = true
		}
	}
	if !hasDL {
		t.Errorf("pizza should pass the DL check, got %v", got)
	}
}

func TestCheckProfileSeesThroughAnnotatedAxioms(t *testing.T) {
	o := profileDoc(t, `SubClassOf(Annotation(rdfs:comment "x") :A ObjectUnionOf(:B :C))`)
	if inProfile(t, o, owl.ProfileEL) {
		t.Error("an annotated axiom should still be checked against the profile")
	}
}
