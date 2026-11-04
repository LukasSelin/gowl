// Code generated for the examples/k8s test. DO NOT EDIT.

package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// Generated would run pods, but generated files are skipped.
type Generated struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Spec PodSpec
}
