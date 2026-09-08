package rdf

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ParseTurtle reads a Turtle document. The base is used to resolve relative
// IRI references and may be empty when the document has none.
//
// The grammar covered is the whole of Turtle apart from two things published
// vocabularies do not use: the N-Triples escape forms inside prefixed names
// beyond the reserved-character escapes, and RDF-star. Anything unrecognised
// is an error rather than a silent skip, so a document that changes shape is
// noticed at generation time.
func ParseTurtle(r io.Reader, base string) (*Graph, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	p := &ttl{
		src:      string(src),
		base:     IRI(base),
		prefixes: make(map[string]IRI),
		labels:   make(map[string]Blank),
	}
	if err := p.document(); err != nil {
		line, col := p.position()
		return nil, fmt.Errorf("turtle: %d:%d: %w", line, col, err)
	}
	return NewGraph(p.triples, p.prefixes), nil
}

type ttl struct {
	src string
	pos int

	base     IRI
	prefixes map[string]IRI
	labels   map[string]Blank
	fresh    int

	triples []Triple
}

func (p *ttl) position() (line, col int) {
	line, col = 1, 1
	for i := 0; i < p.pos && i < len(p.src); i++ {
		if p.src[i] == '\n' {
			line, col = line+1, 1
		} else {
			col++
		}
	}
	return line, col
}

func (p *ttl) emit(s Term, pred IRI, o Term) {
	p.triples = append(p.triples, Triple{Subject: s, Predicate: pred, Object: o})
}

func (p *ttl) blank() Blank {
	p.fresh++
	return Blank(fmt.Sprintf("b%d", p.fresh))
}

// --- scanning ---------------------------------------------------------------

func (p *ttl) ws() {
	for p.pos < len(p.src) {
		switch c := p.src[p.pos]; {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n':
			p.pos++
		case c == '#':
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		default:
			return
		}
	}
}

func (p *ttl) eof() bool { p.ws(); return p.pos >= len(p.src) }

func (p *ttl) peek() byte {
	if p.pos < len(p.src) {
		return p.src[p.pos]
	}
	return 0
}

// accept consumes tok if it is next, after whitespace.
func (p *ttl) accept(tok string) bool {
	p.ws()
	if strings.HasPrefix(p.src[p.pos:], tok) {
		p.pos += len(tok)
		return true
	}
	return false
}

// acceptFold is accept for the case-insensitive keywords — "a", "true",
// "false", "PREFIX", "BASE" — which must end at a name boundary, so that
// "BASEBALL" is not read as "BASE" and "abc:x" is not read as "a".
func (p *ttl) acceptFold(tok string) bool {
	p.ws()
	rest := p.src[p.pos:]
	if len(rest) < len(tok) || !strings.EqualFold(rest[:len(tok)], tok) {
		return false
	}
	if len(rest) > len(tok) && (isNameChar(rest[len(tok)]) || rest[len(tok)] == ':') {
		return false
	}
	p.pos += len(tok)
	return true
}

func isDelim(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '<' || c == '#'
}

func (p *ttl) expect(tok string) error {
	if !p.accept(tok) {
		return fmt.Errorf("expected %q", tok)
	}
	return nil
}

// --- grammar ----------------------------------------------------------------

func (p *ttl) document() error {
	for !p.eof() {
		switch {
		case p.accept("@prefix"):
			if err := p.prefixID(); err != nil {
				return err
			}
			if err := p.expect("."); err != nil {
				return err
			}
		case p.accept("@base"):
			if err := p.baseDecl(); err != nil {
				return err
			}
			if err := p.expect("."); err != nil {
				return err
			}
		case p.acceptFold("PREFIX"):
			if err := p.prefixID(); err != nil {
				return err
			}
		case p.acceptFold("BASE"):
			if err := p.baseDecl(); err != nil {
				return err
			}
		default:
			if err := p.triplesStatement(); err != nil {
				return err
			}
			if err := p.expect("."); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *ttl) prefixID() error {
	p.ws()
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ':' {
		if isDelim(p.src[p.pos]) {
			return fmt.Errorf("malformed prefix declaration")
		}
		p.pos++
	}
	if p.pos >= len(p.src) {
		return fmt.Errorf("unterminated prefix declaration")
	}
	name := p.src[start:p.pos]
	p.pos++ // ':'
	ns, err := p.iriRef()
	if err != nil {
		return err
	}
	p.prefixes[name] = ns
	return nil
}

func (p *ttl) baseDecl() error {
	iri, err := p.iriRef()
	if err != nil {
		return err
	}
	p.base = iri
	return nil
}

func (p *ttl) triplesStatement() error {
	// A blank node property list may itself stand as the subject, with an
	// optional predicate-object list following it.
	if p.accept("[") {
		b := p.blank()
		if err := p.blankBody(b); err != nil {
			return err
		}
		p.ws()
		if p.peek() == '.' {
			return nil
		}
		return p.predicateObjectList(b)
	}
	subj, err := p.term()
	if err != nil {
		return err
	}
	if _, ok := subj.(Literal); ok {
		return fmt.Errorf("literal used as subject")
	}
	return p.predicateObjectList(subj)
}

func (p *ttl) predicateObjectList(subj Term) error {
	for {
		pred, err := p.verb()
		if err != nil {
			return err
		}
		if err := p.objectList(subj, pred); err != nil {
			return err
		}
		if !p.accept(";") {
			return nil
		}
		// Trailing and repeated semicolons are legal and end the list when
		// nothing follows them.
		for p.accept(";") {
		}
		p.ws()
		if c := p.peek(); c == '.' || c == ']' || c == 0 {
			return nil
		}
	}
}

func (p *ttl) objectList(subj Term, pred IRI) error {
	for {
		obj, err := p.term()
		if err != nil {
			return err
		}
		p.emit(subj, pred, obj)
		if !p.accept(",") {
			return nil
		}
	}
}

func (p *ttl) verb() (IRI, error) {
	if p.acceptFold("a") {
		return RDFType, nil
	}
	t, err := p.term()
	if err != nil {
		return "", err
	}
	iri, ok := t.(IRI)
	if !ok {
		return "", fmt.Errorf("predicate must be an IRI, got %s", t)
	}
	return iri, nil
}

// term reads one subject or object.
func (p *ttl) term() (Term, error) {
	p.ws()
	switch c := p.peek(); {
	case c == 0:
		return nil, io.ErrUnexpectedEOF
	case c == '<':
		return p.iriRef()
	case c == '"' || c == '\'':
		return p.literal()
	case c == '[':
		p.pos++
		b := p.blank()
		return b, p.blankBody(b)
	case c == '(':
		p.pos++
		return p.collection()
	case c == '_':
		return p.blankLabel()
	case c == '+' || c == '-' || c >= '0' && c <= '9':
		return p.number()
	}
	if p.acceptFold("true") {
		return Literal{Value: "true", Datatype: XSDBoolean}, nil
	}
	if p.acceptFold("false") {
		return Literal{Value: "false", Datatype: XSDBoolean}, nil
	}
	return p.prefixedName()
}

// blankBody parses the interior of a "[ ... ]" after the bracket, attaching
// what it finds to b.
func (p *ttl) blankBody(b Blank) error {
	if p.accept("]") {
		return nil
	}
	if err := p.predicateObjectList(b); err != nil {
		return err
	}
	return p.expect("]")
}

// collection parses a "( ... )" after the paren into an rdf:first/rdf:rest
// chain, returning its head.
func (p *ttl) collection() (Term, error) {
	var items []Term
	for {
		if p.accept(")") {
			break
		}
		item, err := p.term()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return RDFNil, nil
	}
	head := p.blank()
	node := head
	for i, item := range items {
		p.emit(node, RDFFirst, item)
		if i == len(items)-1 {
			p.emit(node, RDFRest, RDFNil)
			break
		}
		next := p.blank()
		p.emit(node, RDFRest, next)
		node = next
	}
	return head, nil
}

func (p *ttl) blankLabel() (Term, error) {
	if !p.accept("_:") {
		return nil, fmt.Errorf("malformed blank node")
	}
	start := p.pos
	for p.pos < len(p.src) && isNameChar(p.src[p.pos]) {
		p.pos++
	}
	label := p.src[start:p.pos]
	if label == "" {
		return nil, fmt.Errorf("empty blank node label")
	}
	b, ok := p.labels[label]
	if !ok {
		b = p.blank()
		p.labels[label] = b
	}
	return b, nil
}

func (p *ttl) iriRef() (IRI, error) {
	p.ws()
	if p.peek() != '<' {
		return "", fmt.Errorf("expected an IRI")
	}
	p.pos++
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case '>':
			p.pos++
			return p.resolve(b.String()), nil
		case '\\':
			r, err := p.escape()
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
		case '\n':
			return "", fmt.Errorf("newline inside an IRI")
		default:
			b.WriteByte(c)
			p.pos++
		}
	}
	return "", fmt.Errorf("unterminated IRI")
}

// resolve turns a possibly relative reference into an absolute IRI. It covers
// the reference forms that appear in ontology documents — empty, fragment,
// absolute path and relative path — rather than all of RFC 3986.
func (p *ttl) resolve(ref string) IRI {
	if p.base == "" || strings.Contains(ref, "://") || strings.HasPrefix(ref, "urn:") {
		return IRI(ref)
	}
	base := string(p.base)
	switch {
	case ref == "":
		return p.base
	case strings.HasPrefix(ref, "#"):
		if i := strings.IndexByte(base, '#'); i >= 0 {
			base = base[:i]
		}
		return IRI(base + ref)
	case strings.HasPrefix(ref, "//"):
		if i := strings.Index(base, ":"); i >= 0 {
			return IRI(base[:i+1] + ref)
		}
		return IRI(ref)
	case strings.HasPrefix(ref, "/"):
		if i := strings.Index(base, "://"); i >= 0 {
			if j := strings.IndexByte(base[i+3:], '/'); j >= 0 {
				return IRI(base[:i+3+j] + ref)
			}
		}
		return IRI(base + ref)
	default:
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			return IRI(base[:i+1] + ref)
		}
		return IRI(base + ref)
	}
}

func (p *ttl) prefixedName() (Term, error) {
	start := p.pos
	for p.pos < len(p.src) && p.src[p.pos] != ':' && !isDelim(p.src[p.pos]) &&
		!strings.ContainsRune(".;,()[]", rune(p.src[p.pos])) {
		p.pos++
	}
	if p.pos >= len(p.src) || p.src[p.pos] != ':' {
		p.pos = start
		return nil, fmt.Errorf("expected a term, found %q", p.snippet())
	}
	prefix := p.src[start:p.pos]
	p.pos++
	local, err := p.localName()
	if err != nil {
		return nil, err
	}
	ns, ok := p.prefixes[prefix]
	if !ok {
		return nil, fmt.Errorf("undeclared prefix %q", prefix)
	}
	return IRI(string(ns) + local), nil
}

// localName reads the part of a prefixed name after the colon. A dot is part
// of the name only when another name character follows it, so that the "." of
// a statement is not swallowed.
func (p *ttl) localName() (string, error) {
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch {
		case c == '\\':
			// Reserved characters are escaped with a backslash and stand for
			// themselves in the IRI.
			if p.pos+1 >= len(p.src) {
				return "", fmt.Errorf("trailing backslash in local name")
			}
			b.WriteByte(p.src[p.pos+1])
			p.pos += 2
		case c == '.':
			if p.pos+1 < len(p.src) && (isNameChar(p.src[p.pos+1]) || p.src[p.pos+1] == '.') {
				b.WriteByte(c)
				p.pos++
				continue
			}
			return b.String(), nil
		case c == '%':
			if p.pos+2 >= len(p.src) {
				return "", fmt.Errorf("truncated percent escape")
			}
			b.WriteString(p.src[p.pos : p.pos+3])
			p.pos += 3
		case isNameChar(c) || c == ':' || c >= 0x80:
			b.WriteByte(c)
			p.pos++
		default:
			return b.String(), nil
		}
	}
	return b.String(), nil
}

func isNameChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '-' || c >= 0x80
}

func (p *ttl) literal() (Term, error) {
	value, err := p.quoted()
	if err != nil {
		return nil, err
	}
	lit := Literal{Value: value, Datatype: XSDString}
	switch {
	case p.peek() == '@':
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && (isNameChar(p.src[p.pos]) || p.src[p.pos] == '-') {
			p.pos++
		}
		lit.Lang = p.src[start:p.pos]
		lit.Datatype = IRI(NSRDF + "langString")
	case strings.HasPrefix(p.src[p.pos:], "^^"):
		p.pos += 2
		p.ws()
		var dt Term
		if p.peek() == '<' {
			dt, err = p.iriRef()
		} else {
			dt, err = p.prefixedName()
		}
		if err != nil {
			return nil, err
		}
		iri, ok := dt.(IRI)
		if !ok {
			return nil, fmt.Errorf("datatype must be an IRI")
		}
		lit.Datatype = iri
	}
	return lit, nil
}

// quoted reads a string in any of Turtle's four quoting forms.
func (p *ttl) quoted() (string, error) {
	var delim string
	switch {
	case strings.HasPrefix(p.src[p.pos:], `"""`):
		delim = `"""`
	case strings.HasPrefix(p.src[p.pos:], "'''"):
		delim = "'''"
	case p.peek() == '"':
		delim = `"`
	case p.peek() == '\'':
		delim = "'"
	default:
		return "", fmt.Errorf("expected a string literal")
	}
	p.pos += len(delim)

	var b strings.Builder
	for p.pos < len(p.src) {
		if strings.HasPrefix(p.src[p.pos:], delim) {
			p.pos += len(delim)
			return b.String(), nil
		}
		if p.src[p.pos] == '\\' {
			r, err := p.escape()
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
			continue
		}
		if len(delim) == 1 && p.src[p.pos] == '\n' {
			return "", fmt.Errorf("newline in a single-quoted string")
		}
		b.WriteByte(p.src[p.pos])
		p.pos++
	}
	return "", fmt.Errorf("unterminated string literal")
}

// escape consumes a backslash escape and returns the rune it denotes.
func (p *ttl) escape() (rune, error) {
	if p.pos+1 >= len(p.src) {
		return 0, fmt.Errorf("trailing backslash")
	}
	c := p.src[p.pos+1]
	p.pos += 2
	switch c {
	case 't':
		return '\t', nil
	case 'b':
		return '\b', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 'f':
		return '\f', nil
	case '"', '\'', '\\':
		return rune(c), nil
	case 'u', 'U':
		n := 4
		if c == 'U' {
			n = 8
		}
		if p.pos+n > len(p.src) {
			return 0, fmt.Errorf("truncated unicode escape")
		}
		v, err := strconv.ParseUint(p.src[p.pos:p.pos+n], 16, 32)
		if err != nil {
			return 0, fmt.Errorf("bad unicode escape: %w", err)
		}
		p.pos += n
		return rune(v), nil
	}
	return 0, fmt.Errorf("unknown escape %q", string(c))
}

func (p *ttl) number() (Term, error) {
	start := p.pos
	if c := p.peek(); c == '+' || c == '-' {
		p.pos++
	}
	digits := func() int {
		n := 0
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.pos++
			n++
		}
		return n
	}
	intDigits := digits()
	dt := XSDInteger
	if p.peek() == '.' && p.pos+1 < len(p.src) && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9' {
		p.pos++
		digits()
		dt = XSDDecimal
	}
	if c := p.peek(); c == 'e' || c == 'E' {
		p.pos++
		if c := p.peek(); c == '+' || c == '-' {
			p.pos++
		}
		if digits() == 0 {
			return nil, fmt.Errorf("malformed exponent")
		}
		dt = XSDDouble
	}
	if intDigits == 0 && dt == XSDInteger {
		return nil, fmt.Errorf("malformed number")
	}
	return Literal{Value: p.src[start:p.pos], Datatype: dt}, nil
}

func (p *ttl) snippet() string {
	end := min(p.pos+20, len(p.src))
	return strings.TrimSpace(p.src[p.pos:end])
}
