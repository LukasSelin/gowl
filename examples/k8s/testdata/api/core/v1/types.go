// Package v1 is a miniature of Kubernetes' k8s.io/api/core/v1.
package v1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

type PodSpec struct {
	Volumes    []Volume
	Containers []Container
}

type Container struct {
	Name string
}

type Volume struct {
	Name string
	VolumeSource
}

type VolumeSource struct {
	Secret *SecretVolumeSource
}

type SecretVolumeSource struct {
	SecretName string
}

type PodTemplateSpec struct {
	metav1.ObjectMeta
	Spec PodSpec
}

// Pod always carries a pod spec.
type Pod struct {
	metav1.TypeMeta
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PodSpec
}

type PodList struct {
	metav1.TypeMeta
	metav1.ListMeta
	Items []Pod
}

// ReplicationController reaches its pod spec through a pointer, so it only may
// run pods.
type ReplicationController struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Spec ReplicationControllerSpec
}

type ReplicationControllerSpec struct {
	Template *PodTemplateSpec
}

// Event shares its name with events/v1.Event, and must stay a class of its own.
type Event struct {
	metav1.TypeMeta
	metav1.ObjectMeta
	Reason string
}

// List is a new name for metav1.List, so it is a list kind too.
type List metav1.List
