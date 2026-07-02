/*
Copyright 2026 fastverk.

The `WorkerPool` CRD (fastverk.savvifi.com/v1) — a pool of Buildbarn workers of a
single toolchain flavor, attached to an RbeCluster. It is the platform-side model
of "which toolchains the RBE offers": each pool runs a specific runner image (the
action-environment rootfs — clang, gcc, CUDA, Lean, …) and advertises a matching
`container-image` platform property to the scheduler. Consumers pick a pool by
setting their `//platforms:rbe` exec-property's container-image to the pool's.

This decouples worker toolchains from the RbeCluster's control-plane components
(frontend/scheduler/storage): add a WorkerPool to offer a new toolchain without
touching the cluster. (The consumer-side bazel exec-toolchain that MATCHES a pool
stays in each consuming repo — that half is tenant-based, not modeled here.)
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkerPoolSpec is the desired state of a WorkerPool.
type WorkerPoolSpec struct {
	// RbeClusterRef is the name of the RbeCluster (same namespace) whose scheduler
	// + storage this pool serves. IMMUTABLE — a pool belongs to one cluster.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="rbeClusterRef is immutable"
	RbeClusterRef string `json:"rbeClusterRef"`

	// RunnerImage is the action-environment ("runner") rootfs actions execute in —
	// the toolchain flavor. MUST carry the toolchains the pool's actions need.
	// +kubebuilder:validation:MinLength=1
	RunnerImage string `json:"runnerImage"`

	// ContainerImage is advertised to the scheduler for action routing + the RBE
	// action-cache key. MUST equal docker://<runnerImage> AND the consumer's
	// //platforms:rbe container-image exec-property, or actions never match this
	// pool. Prefer a digest pin.
	// +kubebuilder:validation:MinLength=1
	ContainerImage string `json:"containerImage"`

	// Replicas is the worker Deployment replica count.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Concurrency is action slots per worker pod (total live slots = replicas ×
	// concurrency).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=4
	// +optional
	Concurrency int32 `json:"concurrency,omitempty"`

	// WorkerImage overrides the bb-worker binary image (defaults to the operator's
	// RBE_WORKER_IMAGE env / a pinned upstream tag).
	// +optional
	WorkerImage string `json:"workerImage,omitempty"`

	// NodeSelector pins the pool to a node group (e.g. a GPU pool). Defaults to the
	// rbe-worker pool.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Tolerations for the pool's nodes (e.g. the rbe-worker taint).
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// Resources for each worker pod.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

// WorkerPoolPhase is the high-level lifecycle phase reflected in status.phase.
// +kubebuilder:validation:Enum=Pending;Deploying;Ready;Degraded;Failed;Deleting
type WorkerPoolPhase string

const (
	WorkerPoolPhasePending   WorkerPoolPhase = "Pending"
	WorkerPoolPhaseDeploying WorkerPoolPhase = "Deploying"
	WorkerPoolPhaseReady     WorkerPoolPhase = "Ready"
	WorkerPoolPhaseDegraded  WorkerPoolPhase = "Degraded"
	WorkerPoolPhaseFailed    WorkerPoolPhase = "Failed"
	WorkerPoolPhaseDeleting  WorkerPoolPhase = "Deleting"
)

// Condition types surfaced on status.conditions.
const (
	// WorkerPoolConditionRegistered is True once the pool's workers register with
	// the scheduler (best-effort: reflected from Deployment availability).
	WorkerPoolConditionRegistered = "Registered"
	// WorkerPoolConditionReady is True when the desired replicas are available.
	WorkerPoolConditionReady = "Ready"
)

// WorkerPoolStatus is the observed state of a WorkerPool.
type WorkerPoolStatus struct {
	// +optional
	Phase WorkerPoolPhase `json:"phase,omitempty"`
	// ReadyReplicas is the number of available worker pods.
	// +optional
	ReadyReplicas int32 `json:"readyReplicas,omitempty"`
	// AdvertisedPlatform echoes the container-image the pool advertises (the value
	// a consumer must set on //platforms:rbe to target this pool).
	// +optional
	AdvertisedPlatform string `json:"advertisedPlatform,omitempty"`
	// ObservedGeneration is the .metadata.generation the status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Namespaced,shortName=wp;workerpools,categories=fastverk
// +kubebuilder:printcolumn:name="Cluster",type=string,JSONPath=`.spec.rbeClusterRef`
// +kubebuilder:printcolumn:name="Platform",type=string,JSONPath=`.spec.containerImage`
// +kubebuilder:printcolumn:name="Ready",type=integer,JSONPath=`.status.readyReplicas`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// WorkerPool is the Schema for the workerpools API.
type WorkerPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   WorkerPoolSpec   `json:"spec,omitempty"`
	Status WorkerPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkerPoolList contains a list of WorkerPool.
type WorkerPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkerPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&WorkerPool{}, &WorkerPoolList{})
}
