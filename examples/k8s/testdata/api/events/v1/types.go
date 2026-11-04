// Package v1 is a miniature of Kubernetes' k8s.io/api/events/v1.
package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type Event struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Note string
}
