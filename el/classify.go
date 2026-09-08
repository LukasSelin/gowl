package el

import "gowl/owl"

// The completion rules. Each derives a new fact from facts already derived and
// one normalized axiom, and nothing is ever retracted, so the fact set only
// grows and saturation terminates when no rule fires.
//
//	CR1  B ∈ S(A), B ⊑ C            ⟹  C ∈ S(A)
//	CR2  B₁,B₂ ∈ S(A), B₁ ⊓ B₂ ⊑ C  ⟹  C ∈ S(A)
//	CR3  B ∈ S(A), B ⊑ ∃r.C         ⟹  (A,C) ∈ R(r)
//	CR4  (A,B) ∈ R(r), C ∈ S(B), ∃r.C ⊑ D  ⟹  D ∈ S(A)
//	CR5  (A,B) ∈ R(r), ⊥ ∈ S(B)     ⟹  ⊥ ∈ S(A)
//	CR6  (A,B) ∈ R(r), r ⊑ s        ⟹  (A,B) ∈ R(s)
//	CR7  (A,B) ∈ R(r₁), (B,C) ∈ R(r₂), r₁ ∘ r₂ ⊑ r₃  ⟹  (A,C) ∈ R(r₃)
const (
	ruleInit = "init"
	ruleCR1  = "CR1"
	ruleCR2  = "CR2"
	ruleCR3  = "CR3"
	ruleCR4  = "CR4"
	ruleCR5  = "CR5"
	ruleCR6  = "CR6"
	ruleCR7  = "CR7"
)

// factKind distinguishes the two kinds of derived fact.
type factKind uint8

const (
	kSub factKind = iota
	kLink
)

// factKey identifies a derived fact. It is comparable, so it doubles as the
// key of the derivation map.
type factKey struct {
	kind factKind
	r    roleID
	a, b conceptID
}

func subFact(a, b conceptID) factKey { return factKey{kind: kSub, a: a, b: b} }
func linkFact(r roleID, a, b conceptID) factKey {
	return factKey{kind: kLink, r: r, a: a, b: b}
}

// derivation records why a fact holds: the rule that produced it, the ontology
// axiom it used, and the facts it built on. Recording this as facts are
// derived is what makes [Classification.Explain] possible; reconstructing it
// afterwards would mean running the whole saturation again backwards.
type derivation struct {
	rule  string
	axiom owl.Axiom
	from  []factKey
}

// incLink is an edge arriving at a concept, kept so that a newly derived
// subsumer can be pushed back to the concepts that reach it.
type incLink struct {
	r    roleID
	from conceptID
}

// conjEntry indexes a binary conjunction axiom under one of its operands,
// carrying the other so CR2 is a single lookup.
type conjEntry struct {
	partner conceptID
	axiom   *nf
}

// exKey indexes ∃r.A ⊑ B under the pair that CR4 has in hand.
type exKey struct {
	r roleID
	a conceptID
}

// Option configures [Classify].
type Option func(*config)

type config struct{ explanations bool }

// WithoutExplanations skips recording derivations. Saturation still produces
// the same subsumptions, but [Classification.Explain] returns nothing. Worth
// it only when the fact set is large enough for the provenance to dominate
// memory.
func WithoutExplanations() Option {
	return func(c *config) { c.explanations = false }
}

// Classification is the saturated result of classifying an ontology. It is
// computed once by [Classify] and is read-only thereafter.
type Classification struct {
	sym  *symbols
	norm *normalizer

	// subs is membership, subsList the same sets in derivation order. Both
	// exist because rules need a fast contains test and a stable iteration
	// order: iterating a map would make explanations differ between runs.
	subs     []map[conceptID]bool
	subsList [][]conceptID

	succ     []map[conceptID][]conceptID
	incoming [][]incLink
	linkSeen map[factKey]bool

	subOf    map[conceptID][]*nf
	conjOf   map[conceptID][]conjEntry
	existsOf map[conceptID][]*nf
	exLeftOf map[exKey][]*nf

	roleSupers  [][]roleSub
	chainFirst  [][]roleChain
	chainSecond [][]roleChain

	why   map[factKey]derivation
	queue []factKey

	// Lazily built by the query side.
	subsumedBy [][]conceptID
}

// Classify saturates an ontology and returns the result. It always succeeds:
// axioms outside the supported fragment are reported by
// [Classification.Unsupported] rather than raised as an error, since a partial
// classification of a mostly-EL ontology is usually what the caller wants.
func Classify(o *owl.Ontology, opts ...Option) *Classification {
	cfg := &config{explanations: true}
	for _, opt := range opts {
		opt(cfg)
	}

	n := normalize(o)
	c := &Classification{
		sym:      n.sym,
		norm:     n,
		linkSeen: make(map[factKey]bool),
		subOf:    make(map[conceptID][]*nf),
		conjOf:   make(map[conceptID][]conjEntry),
		existsOf: make(map[conceptID][]*nf),
		exLeftOf: make(map[exKey][]*nf),
	}
	if cfg.explanations {
		c.why = make(map[factKey]derivation)
	}
	c.index()
	c.saturate()
	return c
}

// index groups the normalized axioms by the position a rule looks them up on.
func (c *Classification) index() {
	n := c.norm
	for i := range n.axioms {
		ax := &n.axioms[i]
		switch ax.kind {
		case nfSub:
			c.subOf[ax.a] = append(c.subOf[ax.a], ax)
		case nfConj:
			c.conjOf[ax.a] = append(c.conjOf[ax.a], conjEntry{partner: ax.a2, axiom: ax})
			c.conjOf[ax.a2] = append(c.conjOf[ax.a2], conjEntry{partner: ax.a, axiom: ax})
		case nfExists:
			c.existsOf[ax.a] = append(c.existsOf[ax.a], ax)
		case nfExLeft:
			k := exKey{r: ax.r, a: ax.a}
			c.exLeftOf[k] = append(c.exLeftOf[k], ax)
		}
	}

	roles := c.sym.countRoles()
	c.roleSupers = make([][]roleSub, roles)
	c.chainFirst = make([][]roleChain, roles)
	c.chainSecond = make([][]roleChain, roles)
	for _, rs := range n.roleSubs {
		c.roleSupers[rs.sub] = append(c.roleSupers[rs.sub], rs)
	}
	for _, ch := range n.chains {
		c.chainFirst[ch.first] = append(c.chainFirst[ch.first], ch)
		c.chainSecond[ch.second] = append(c.chainSecond[ch.second], ch)
	}

	concepts := c.sym.countConcepts()
	c.subs = make([]map[conceptID]bool, concepts)
	c.subsList = make([][]conceptID, concepts)
	c.incoming = make([][]incLink, concepts)
	for i := range c.subs {
		c.subs[i] = make(map[conceptID]bool, 4)
	}
	c.succ = make([]map[conceptID][]conceptID, roles)
	for i := range c.succ {
		c.succ[i] = make(map[conceptID][]conceptID)
	}
}

func (c *Classification) addSub(a, b conceptID, d derivation) {
	if c.subs[a][b] {
		return
	}
	c.subs[a][b] = true
	c.subsList[a] = append(c.subsList[a], b)
	k := subFact(a, b)
	if c.why != nil {
		c.why[k] = d
	}
	c.queue = append(c.queue, k)
}

func (c *Classification) addLink(r roleID, a, b conceptID, d derivation) {
	k := linkFact(r, a, b)
	if c.linkSeen[k] {
		return
	}
	c.linkSeen[k] = true
	c.succ[r][a] = append(c.succ[r][a], b)
	c.incoming[b] = append(c.incoming[b], incLink{r: r, from: a})
	if c.why != nil {
		c.why[k] = d
	}
	c.queue = append(c.queue, k)
}

func (c *Classification) saturate() {
	// Every concept starts out below itself and below ⊤.
	init := derivation{rule: ruleInit}
	for a := conceptID(0); int(a) < c.sym.countConcepts(); a++ {
		c.addSub(a, a, init)
		c.addSub(a, top, init)
	}

	for len(c.queue) > 0 {
		k := c.queue[len(c.queue)-1]
		c.queue = c.queue[:len(c.queue)-1]
		if k.kind == kSub {
			c.processSub(k)
		} else {
			c.processLink(k)
		}
	}
}

// processSub fires every rule that has "b ∈ S(a)" as a premise.
func (c *Classification) processSub(k factKey) {
	a, b := k.a, k.b

	for _, ax := range c.subOf[b] { // CR1
		c.addSub(a, ax.b, derivation{rule: ruleCR1, axiom: ax.source, from: []factKey{k}})
	}

	for _, e := range c.conjOf[b] { // CR2
		if c.subs[a][e.partner] {
			c.addSub(a, e.axiom.b, derivation{
				rule: ruleCR2, axiom: e.axiom.source,
				from: []factKey{k, subFact(a, e.partner)},
			})
		}
	}

	for _, ax := range c.existsOf[b] { // CR3
		c.addLink(ax.r, a, ax.b, derivation{rule: ruleCR3, axiom: ax.source, from: []factKey{k}})
	}

	// CR4 and CR5 push back along edges arriving at a. Indexing incoming edges
	// per concept is what keeps this proportional to the edges that exist
	// rather than to the number of roles.
	for i := 0; i < len(c.incoming[a]); i++ {
		in := c.incoming[a][i]
		for _, ax := range c.exLeftOf[exKey{r: in.r, a: b}] { // CR4
			c.addSub(in.from, ax.b, derivation{
				rule: ruleCR4, axiom: ax.source,
				from: []factKey{k, linkFact(in.r, in.from, a)},
			})
		}
		if b == bottom { // CR5
			c.addSub(in.from, bottom, derivation{
				rule: ruleCR5,
				from: []factKey{k, linkFact(in.r, in.from, a)},
			})
		}
	}
}

// processLink fires every rule that has "(a,b) ∈ R(r)" as a premise.
func (c *Classification) processLink(k factKey) {
	r, a, b := k.r, k.a, k.b

	// CR4 against the subsumers b already has. The index loop is deliberate:
	// addSub can append to this same list when a and b coincide.
	for i := 0; i < len(c.subsList[b]); i++ {
		y := c.subsList[b][i]
		for _, ax := range c.exLeftOf[exKey{r: r, a: y}] {
			c.addSub(a, ax.b, derivation{
				rule: ruleCR4, axiom: ax.source,
				from: []factKey{k, subFact(b, y)},
			})
		}
		if y == bottom { // CR5
			c.addSub(a, bottom, derivation{
				rule: ruleCR5,
				from: []factKey{k, subFact(b, bottom)},
			})
		}
	}

	for _, rs := range c.roleSupers[r] { // CR6
		c.addLink(rs.super, a, b, derivation{rule: ruleCR6, axiom: rs.source, from: []factKey{k}})
	}

	for _, ch := range c.chainFirst[r] { // CR7, this link first
		for _, cc := range c.succ[ch.second][b] {
			c.addLink(ch.super, a, cc, derivation{
				rule: ruleCR7, axiom: ch.source,
				from: []factKey{k, linkFact(ch.second, b, cc)},
			})
		}
	}

	for _, ch := range c.chainSecond[r] { // CR7, this link second
		for i := 0; i < len(c.incoming[a]); i++ {
			in := c.incoming[a][i]
			if in.r != ch.first {
				continue
			}
			c.addLink(ch.super, in.from, b, derivation{
				rule: ruleCR7, axiom: ch.source,
				from: []factKey{linkFact(ch.first, in.from, a), k},
			})
		}
	}
}
