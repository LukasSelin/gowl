// Package broken breaks the questions' rule that nothing is both an object kind
// and a list kind.
package broken

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type Both struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	metav1.ListMeta
}
