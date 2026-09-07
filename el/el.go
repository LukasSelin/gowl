// Package el classifies OWL 2 EL ontologies.
//
// It implements the completion-rule algorithm of Baader, Brandt and Lutz
// ("Pushing the EL Envelope"), the same family ELK belongs to: normalize the
// ontology to a handful of axiom shapes, then saturate a set of derived facts
// until nothing new appears. Saturation is polynomial and monotone — no
// tableau, no backtracking, no search — which is why EL scales to ontologies
// the size of SNOMED while full OWL 2 DL does not.
//
// # The supported fragment
//
// Class expressions: named classes, owl:Thing, owl:Nothing,
// ObjectIntersectionOf and ObjectSomeValuesFrom.
//
// Axioms: SubClassOf, EquivalentClasses, DisjointClasses,
// ObjectPropertyDomain, SubObjectPropertyOf, EquivalentObjectProperties,
// TransitiveObjectProperty and property chains (SubPropertyChainOf).
//
// Everything else — nominals, ObjectHasSelf, data properties, ranges, keys,
// and every non-EL constructor — is reported by [Classification.Unsupported]
// rather than silently ignored, because an axiom dropped in silence makes the
// result look complete when it is not. Results stay sound: what the classifier
// derives is entailed. Dropping axioms costs completeness, so a subsumption it
// does not derive is not proof of non-entailment when Unsupported is non-empty.
//
// Individuals are not classified. This is a TBox reasoner.
package el

import "gowl/owl"

// conceptID and roleID number the concept and role names. Working with dense
// integers rather than IRIs keeps saturation to slice and map lookups on small
// comparable keys, which matters when the fact set runs to millions.
type conceptID int32

type roleID int32

// The two poles are always present and always hold these ids.
const (
	top    conceptID = 0
	bottom conceptID = 1
)

// symbols maps between OWL entities and the internal ids, and mints the fresh
// names normalization needs. A fresh concept has no class: it exists only to
// stand for a subexpression, and never appears in a result.
type symbols struct {
	byClass map[owl.Class]conceptID
	classes []owl.Class

	byRole map[owl.ObjectProperty]roleID
	roles  []owl.ObjectProperty
}

func newSymbols() *symbols {
	s := &symbols{
		byClass: make(map[owl.Class]conceptID),
		byRole:  make(map[owl.ObjectProperty]roleID),
	}
	s.concept(owl.Thing)   // top
	s.concept(owl.Nothing) // bottom
	return s
}

func (s *symbols) concept(c owl.Class) conceptID {
	if id, ok := s.byClass[c]; ok {
		return id
	}
	id := conceptID(len(s.classes))
	s.classes = append(s.classes, c)
	s.byClass[c] = id
	return id
}

// freshConcept mints an internal name for a subexpression.
func (s *symbols) freshConcept() conceptID {
	id := conceptID(len(s.classes))
	s.classes = append(s.classes, "")
	return id
}

func (s *symbols) role(p owl.ObjectProperty) roleID {
	if id, ok := s.byRole[p]; ok {
		return id
	}
	id := roleID(len(s.roles))
	s.roles = append(s.roles, p)
	s.byRole[p] = id
	return id
}

// freshRole mints an internal role, used to binarize a long property chain.
func (s *symbols) freshRole() roleID {
	id := roleID(len(s.roles))
	s.roles = append(s.roles, "")
	return id
}

func (s *symbols) countConcepts() int { return len(s.classes) }
func (s *symbols) countRoles() int    { return len(s.roles) }

// named reports whether an id belongs to a class from the ontology rather than
// to a name normalization invented.
func (s *symbols) named(id conceptID) bool { return s.classes[id] != "" }

func (s *symbols) class(id conceptID) owl.Class { return s.classes[id] }

func (s *symbols) lookup(c owl.Class) (conceptID, bool) {
	id, ok := s.byClass[c]
	return id, ok
}
