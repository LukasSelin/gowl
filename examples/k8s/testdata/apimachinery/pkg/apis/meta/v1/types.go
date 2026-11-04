// Package v1 is a miniature of Kubernetes' k8s.io/apimachinery/pkg/apis/meta/v1,
// with just enough of it for the examples/k8s questions to have answers.
package v1

type TypeMeta struct {
	Kind       string
	APIVersion string
}

type ObjectMeta struct {
	Name   string
	Labels map[string]string
}

type ListMeta struct {
	Continue string
}

type LabelSelector struct {
	MatchLabels map[string]string
}

type Condition struct {
	Type string
}

type List struct {
	TypeMeta
	ListMeta
	Items []string
}
