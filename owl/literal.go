package owl

import (
	"strconv"
	"time"
)

// Literal is a typed or language-tagged data value. When Lang is non-empty the
// literal is language-tagged and Datatype is implicitly rdf:langString.
type Literal struct {
	Value    string
	Datatype Datatype
	Lang     string
}

func (l Literal) String() string { return Functional(l) }

func (Literal) isAnnotationValue() {}

// Str returns an xsd:string literal.
func Str(s string) Literal { return Literal{Value: s, Datatype: XSDString} }

// LangStr returns a language-tagged literal, e.g. LangStr("Pizza", "en").
func LangStr(s, lang string) Literal {
	return Literal{Value: s, Datatype: RDFLangString, Lang: lang}
}

// Int returns an xsd:integer literal.
func Int(n int) Literal { return Literal{Value: strconv.Itoa(n), Datatype: XSDInteger} }

// Float returns an xsd:double literal.
func Float(f float64) Literal {
	return Literal{Value: strconv.FormatFloat(f, 'g', -1, 64), Datatype: XSDDouble}
}

// Bool returns an xsd:boolean literal.
func Bool(b bool) Literal { return Literal{Value: strconv.FormatBool(b), Datatype: XSDBoolean} }

// Time returns an xsd:dateTime literal in RFC 3339 form.
func Time(t time.Time) Literal {
	return Literal{Value: t.Format(time.RFC3339), Datatype: XSDDateTime}
}

// Typed returns a literal with an explicit datatype.
func Typed(value string, dt Datatype) Literal { return Literal{Value: value, Datatype: dt} }
