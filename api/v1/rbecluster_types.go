/*
Copyright 2026 fastverk.

The `RbeCluster` CRD (fastverk.savvifi.com/v1) — a Buildbarn remote-build-execution
cluster as a first-class Kubernetes object. The operator reconciles the CR into a
buildbarn install by DELEGATING to a Helm-runner Job (it renders the CR spec into
buildbarn chart values and runs `helm upgrade --install`), then proves the cluster
can execute actions end-to-end via a periodic smoke-build probe Job. Spec fields
mirror repos/buildbarn/chart/values.yaml so the CR is the single source of truth
for RBE topology + scaling.
*/

package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RbeFrontendSpec configures the gRPC frontend (the internet-facing entrypoint).
type RbeFrontendSpec struct {
	// Replicas is the frontend Deployment replica count.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=3
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// TLS terminates gRPC-TLS at the frontend (pair with a TCP-passthrough NLB so
	// h2/ALPN negotiates end-to-end). Clients then dial grpcs://<host>.
	// +optional
	TLS RbeTLSSpec `json:"tls,omitempty"`

	// JWT enables JWT authn on the frontend (Cognito M2M / client-credentials
	// access tokens). Buildbarn's frontend has NO native authz, so this is what
	// protects an internet-facing RBE endpoint.
	// +optional
	JWT RbeJWTSpec `json:"jwt,omitempty"`
}

// RbeTLSSpec is the frontend gRPC-TLS config.
type RbeTLSSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// SecretName is the TLS key pair Secret (cert-manager-issued or supplied).
	// +kubebuilder:default="frontend-tls"
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

// RbeJWTSpec is the frontend JWT-authn config (drives the buildbarn chart's
// frontend.auth.jwt block + the jwks-sync sidecar).
type RbeJWTSpec struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Issuer is the Cognito access-token issuer:
	// https://cognito-idp.<region>.amazonaws.com/<userPoolId>.
	// +optional
	Issuer string `json:"issuer,omitempty"`
	// RequiredScope is the resourceServer/scope the token must carry.
	// +kubebuilder:default="fastverk-api/rbe:build"
	// +optional
	RequiredScope string `json:"requiredScope,omitempty"`
	// JwksURL is <issuer>/.well-known/jwks.json (the jwks-sync sidecar polls it).
	// +optional
	JwksURL string `json:"jwksUrl,omitempty"`
}

// RbeSchedulerSpec configures the scheduler (the action queue / worker router).
type RbeSchedulerSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// Instance is the REAPI instance name actions target (e.g. "ubuntu22-04").
	// +kubebuilder:default="ubuntu22-04"
	// +optional
	Instance string `json:"instance,omitempty"`
	// DefaultExecutionTimeout is the per-action execution timeout (e.g. "1800s").
	// +kubebuilder:default="1800s"
	// +optional
	DefaultExecutionTimeout string `json:"defaultExecutionTimeout,omitempty"`
	// MaximumExecutionTimeout caps per-action runtime (e.g. "7200s").
	// +kubebuilder:default="7200s"
	// +optional
	MaximumExecutionTimeout string `json:"maximumExecutionTimeout,omitempty"`
}

// RbeWorkerSpec configures the worker pool (where actions execute).
type RbeWorkerSpec struct {
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=3
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// Concurrency is action slots per worker pod (total live slots = replicas ×
	// concurrency). Size against the node's cores+RAM.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=4
	// +optional
	Concurrency int32 `json:"concurrency,omitempty"`
	// RunnerImage is the action-environment ("runner") rootfs actions execute in.
	// MUST carry the toolchains actions need.
	// +kubebuilder:default="ghcr.io/catthehacker/ubuntu:act-22.04"
	// +optional
	RunnerImage string `json:"runnerImage,omitempty"`
	// ContainerImage is advertised to the scheduler for action routing + the RBE
	// action-cache key. MUST equal docker://<runnerImage> AND the consumer's
	// //platforms:rbe container-image exec-property, or actions never match.
	// +kubebuilder:default="docker://ghcr.io/catthehacker/ubuntu:act-22.04"
	// +optional
	ContainerImage string `json:"containerImage,omitempty"`

	// CPURequest is the per-worker-pod CPU request (buildbarn chart worker.resources.requests.cpu).
	// +kubebuilder:default="2"
	// +optional
	CPURequest string `json:"cpuRequest,omitempty"`
	// MemoryRequest is the per-worker-pod memory request. Heavy Rust links
	// (aws-lc-sys/ring) peak >7Gi; combined with Concurrency=1 this reserves
	// enough headroom to keep a single action from OOM-killing the node.
	// +kubebuilder:default="4Gi"
	// +optional
	MemoryRequest string `json:"memoryRequest,omitempty"`
	// MemoryLimit caps per-worker-pod memory. Empty = no limit (the chart default).
	// Set it (e.g. "14Gi" on a 16Gi node at Concurrency=1) to make heavy links
	// deterministic rather than oversubscribing the node.
	// +optional
	MemoryLimit string `json:"memoryLimit,omitempty"`
	// NodeSelector steers worker pods onto a labeled node pool (e.g. a
	// memory-optimized r6i group: {"workload":"rbe-worker"}). Empty = default scheduling.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// RbeStorageSpec configures the sharded CAS/AC blobstore (EBS-backed).
type RbeStorageSpec struct {
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	// +optional
	Replicas int32 `json:"replicas,omitempty"`
	// CasSize is the per-replica CAS EBS PVC size (e.g. "33Gi"); cas dominates.
	// +kubebuilder:default="33Gi"
	// +optional
	CasSize string `json:"casSize,omitempty"`
	// AcSize is the per-replica AC EBS PVC size (e.g. "1Gi").
	// +kubebuilder:default="1Gi"
	// +optional
	AcSize string `json:"acSize,omitempty"`
}

// RbeProbeSpec configures the periodic RBE smoke build the operator runs to prove
// the cluster can execute actions end-to-end (a canned REAPI action against the
// frontend, using a freshly minted rbe:build token).
type RbeProbeSpec struct {
	// Enabled turns on the periodic probe CronJob (on-demand probes are always
	// available via the RunSmokeBuild control-plane RPC).
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Schedule is the probe cadence (cron). Default every 15m.
	// +kubebuilder:default="*/15 * * * *"
	// +optional
	Schedule string `json:"schedule,omitempty"`
	// Image is the probe image — a small client that runs a REAPI Execute against
	// the frontend and reports {ok, latency, cacheHit}. Defaults to the operator
	// env RBE_PROBE_IMAGE.
	// +optional
	Image string `json:"image,omitempty"`
}

// RbeClusterSpec is the desired state of an RbeCluster.
type RbeClusterSpec struct {
	// Host is the public gRPC endpoint hostname for the frontend (e.g.
	// "rbe.fastverk.com"). external-dns publishes it; clients dial grpcs://<host>.
	// +optional
	Host string `json:"host,omitempty"`

	// ChartRepo/ChartVersion pin the buildbarn Helm chart the delegated Helm-runner
	// Job installs. Empty falls back to the operator env (RBE_CHART_REPO / _VERSION).
	// +optional
	ChartRepo string `json:"chartRepo,omitempty"`
	// +optional
	ChartVersion string `json:"chartVersion,omitempty"`

	// +optional
	Frontend RbeFrontendSpec `json:"frontend,omitempty"`
	// +optional
	Scheduler RbeSchedulerSpec `json:"scheduler,omitempty"`
	// +optional
	Worker RbeWorkerSpec `json:"worker,omitempty"`
	// +optional
	Storage RbeStorageSpec `json:"storage,omitempty"`
	// +optional
	Probe RbeProbeSpec `json:"probe,omitempty"`
}

// RbeClusterPhase is the high-level lifecycle phase reflected in status.phase.
// +kubebuilder:validation:Enum=Pending;Installing;Ready;Degraded;Failed;Deleting
type RbeClusterPhase string

const (
	RbePhasePending    RbeClusterPhase = "Pending"
	RbePhaseInstalling RbeClusterPhase = "Installing"
	RbePhaseReady      RbeClusterPhase = "Ready"
	RbePhaseDegraded   RbeClusterPhase = "Degraded"
	RbePhaseFailed     RbeClusterPhase = "Failed"
	RbePhaseDeleting   RbeClusterPhase = "Deleting"
)

// Condition types surfaced on status.conditions.
const (
	// RbeConditionInstalled is True once the delegated Helm-runner Job has applied
	// the buildbarn release for the current spec hash.
	RbeConditionInstalled = "Installed"
	// RbeConditionReady is True when the frontend is reachable and workers are up.
	RbeConditionReady = "Ready"
	// RbeConditionProbed is True when the most recent smoke build succeeded.
	RbeConditionProbed = "Probed"
)

// RbeWorkerStatus is the observed worker-pool utilization (reflected from the
// scheduler / Prometheus by the control plane).
type RbeWorkerStatus struct {
	// +optional
	Total int64 `json:"total,omitempty"`
	// +optional
	Idle int64 `json:"idle,omitempty"`
	// +optional
	Executing int64 `json:"executing,omitempty"`
}

// RbeProbeStatus is the result of the most recent smoke build.
type RbeProbeStatus struct {
	// +optional
	OK bool `json:"ok,omitempty"`
	// LatencyMs is the wall-clock execution time of the probe action.
	// +optional
	LatencyMs int64 `json:"latencyMs,omitempty"`
	// CacheHit is true when the action result was served from the AC.
	// +optional
	CacheHit bool `json:"cacheHit,omitempty"`
	// At is when the probe last ran.
	// +optional
	At *metav1.Time `json:"at,omitempty"`
	// Message carries the failure reason when OK is false.
	// +optional
	Message string `json:"message,omitempty"`
}

// RbeClusterStatus is the observed state of an RbeCluster.
type RbeClusterStatus struct {
	// +optional
	Phase RbeClusterPhase `json:"phase,omitempty"`
	// Endpoint is the resolved client endpoint (grpcs://<host> or the in-cluster
	// frontend Service when no host is set).
	// +optional
	Endpoint string `json:"endpoint,omitempty"`
	// Workers is the observed worker-pool utilization.
	// +optional
	Workers RbeWorkerStatus `json:"workers,omitempty"`
	// QueueDepth is the scheduler's queued-action count.
	// +optional
	QueueDepth int64 `json:"queueDepth,omitempty"`
	// LastProbe is the result of the most recent smoke build.
	// +optional
	LastProbe *RbeProbeStatus `json:"lastProbe,omitempty"`
	// LastProbeTrigger is the opaque token of the most recently handled on-demand
	// probe (the `fastverk.savvifi.com/probe-now` annotation). When the annotation
	// differs, the controller kicks a fresh smoke-build Job. This is how the
	// control-plane RunSmokeBuild RPC drives an on-demand RBE test.
	// +optional
	LastProbeTrigger string `json:"lastProbeTrigger,omitempty"`
	// ObservedHash is the spec hash of the currently-applied buildbarn release; a
	// change spawns a fresh Helm-runner Job (re-install).
	// +optional
	ObservedHash string `json:"observedHash,omitempty"`
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
// +kubebuilder:resource:scope=Namespaced,shortName=rbe,categories=fastverk
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Endpoint",type=string,JSONPath=`.status.endpoint`
// +kubebuilder:printcolumn:name="Workers",type=integer,JSONPath=`.status.workers.total`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// RbeCluster is the Schema for the rbeclusters API.
type RbeCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RbeClusterSpec   `json:"spec,omitempty"`
	Status RbeClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RbeClusterList contains a list of RbeCluster.
type RbeClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RbeCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RbeCluster{}, &RbeClusterList{})
}
