package owl

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Tokens of OWL 2 Functional-Style Syntax.
type tokKind uint8

const (
	tEOF    tokKind = iota
	tOpen           // (
	tClose          // )
	tEq             // =
	tIRI            // <http://...>, val is the inner text
	tPName          // rdfs:label or :Pizza or "rdfs:", val is the text as written
	tName           // a bare keyword such as SubClassOf
	tBNode          // _:id, val is the id
	tString         // "...", val is the unescaped text
	tLang           // @en, val is the tag without @
	tCaret          // ^^
	tInt            // a non-negative integer, val is the digits
)

func (k tokKind) String() string {
	switch k {
	case tEOF:
		return "end of input"
	case tOpen:
		return "'('"
	case tClose:
		return "')'"
	case tEq:
		return "'='"
	case tIRI:
		return "an IRI"
	case tPName:
		return "an abbreviated IRI"
	case tName:
		return "a keyword"
	case tBNode:
		return "an anonymous individual"
	case tString:
		return "a quoted string"
	case tLang:
		return "a language tag"
	case tCaret:
		return "'^^'"
	case tInt:
		return "an integer"
	}
	return "an unknown token"
}

type token struct {
	kind tokKind
	val  string
	line int
}

func (t token) describe() string {
	if t.kind == tEOF {
		return "end of input"
	}
	return fmt.Sprintf("%s (%q)", t.kind, t.val)
}

// parseError carries a line number so failures point at the offending input.
type parseError struct {
	line int
	msg  string
}

func (e *parseError) Error() string { return fmt.Sprintf("owl: line %d: %s", e.line, e.msg) }

func failf(line int, format string, args ...any) {
	panic(&parseError{line: line, msg: fmt.Sprintf(format, args...)})
}

// isNameByte reports whether c may appear inside a keyword or abbreviated IRI.
// Everything else terminates such a token.
func isNameByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == '-' || c == '.' || c == ':' || c == '%' || c == '~' || c == '+':
		return true
	case c >= utf8.RuneSelf:
		// Local names may contain non-ASCII characters.
		return true
	}
	return false
}

// lex tokenizes an entire functional-syntax document. It panics with a
// *parseError on malformed input; callers recover in [ParseFunctionalString].
func lex(src string) []token {
	var toks []token
	line := 1
	i := 0

	emit := func(k tokKind, v string) { toks = append(toks, token{kind: k, val: v, line: line}) }

	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == '(':
			emit(tOpen, "(")
			i++
		case c == ')':
			emit(tClose, ")")
			i++
		case c == '=':
			emit(tEq, "=")
			i++
		case c == '<':
			j := strings.IndexByte(src[i:], '>')
			if j < 0 {
				failf(line, "unterminated IRI")
			}
			body := src[i+1 : i+j]
			if strings.ContainsAny(body, " \t\r\n<") {
				failf(line, "malformed IRI <%s>", body)
			}
			emit(tIRI, body)
			i += j + 1
		case c == '"':
			s, adv, nl := lexString(src[i:], line)
			emit(tString, s)
			i += adv
			line += nl
		case c == '@':
			j := i + 1
			for j < len(src) && (isAlphaNum(src[j]) || src[j] == '-') {
				j++
			}
			if j == i+1 {
				failf(line, "empty language tag")
			}
			emit(tLang, src[i+1:j])
			i = j
		case c == '^':
			if i+1 >= len(src) || src[i+1] != '^' {
				failf(line, "expected '^^'")
			}
			emit(tCaret, "^^")
			i += 2
		case c == '_' && i+1 < len(src) && src[i+1] == ':':
			j := i + 2
			for j < len(src) && isNameByte(src[j]) {
				j++
			}
			emit(tBNode, src[i+2:j])
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && src[j] >= '0' && src[j] <= '9' {
				j++
			}
			emit(tInt, src[i:j])
			i = j
		case isNameByte(c):
			j := i
			for j < len(src) && isNameByte(src[j]) {
				j++
			}
			text := src[i:j]
			if strings.ContainsRune(text, ':') {
				emit(tPName, text)
			} else {
				emit(tName, text)
			}
			i = j
		default:
			failf(line, "unexpected character %q", string(rune(c)))
		}
	}
	toks = append(toks, token{kind: tEOF, line: line})
	return toks
}

func isAlphaNum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// lexString reads a quoted string starting at src[0] == '"'. It returns the
// unescaped text, how many bytes were consumed, and how many newlines the
// literal spanned.
func lexString(src string, line int) (text string, adv int, newlines int) {
	var b strings.Builder
	i := 1
	for i < len(src) {
		switch src[i] {
		case '"':
			return b.String(), i + 1, newlines
		case '\\':
			if i+1 >= len(src) {
				failf(line, "unterminated escape in string")
			}
			switch src[i+1] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 'r':
				b.WriteByte('\r')
			case 't':
				b.WriteByte('\t')
			default:
				failf(line, "unknown escape %q in string", `\`+string(src[i+1]))
			}
			i += 2
		case '\n':
			newlines++
			b.WriteByte('\n')
			i++
		default:
			b.WriteByte(src[i])
			i++
		}
	}
	failf(line, "unterminated string")
	return "", 0, 0
}
