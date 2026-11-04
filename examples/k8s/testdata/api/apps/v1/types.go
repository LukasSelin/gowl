// Package v1 is a miniature of Kubernetes' k8s.io/api/apps/v1. It imports
// core/v1 as v1, the way the real package does.
package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Deployment runs pods through DeploymentSpec.Template, a value field.
type Deployment struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Spec   DeploymentSpec
	Status DeploymentStatus
}

type DeploymentSpec struct {
	Selector *metav1.LabelSelector
	Template v1.PodTemplateSpec
}

type DeploymentStatus struct {
	Conditions []metav1.Condition
}
