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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RbeControlPlanePlacement pins a Buildbarn CONTROL-PLANE singleton (frontend,
// scheduler, storage) to specific capacity and gives it real resource requests.
//
// Why this exists as its own type rather than more loose fields: the buildbarn chart
// used to apply ONE global nodeSelector/tolerations to every pod, so the singletons
// inevitably landed on the same tainted SPOT pool as the scale-out workers, as
// BestEffort pods. bb-scheduler holds the entire RBE action queue in NON-DURABLE
// process RAM, so losing that pod fails every concurrent build at once — and
// BestEffort is precisely what the kubelet evicts FIRST when a node packed with
// memory-hungry build actions comes under pressure. Splitting the control plane onto
// stable on-demand capacity, with requests that make it Burstable rather than
// BestEffort, is the fix. Requires buildbarn chart >= 0.2.0, which added the
// per-component values these render into.
//
// All three fields are optional; unset means "inherit the chart-global placement and
// emit no resources", i.e. exactly the pre-0.2.0 behavior.
type RbeControlPlanePlacement struct {
	// NodeSelector pins the component to a labeled node pool, e.g.
	// {"workload":"rbe-control"} for a small dedicated on-demand pool.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations let the component land on a tainted pool. Pair with NodeSelector:
	// the toleration grants access, the selector compels placement.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// Resources sets requests/limits on the component's main container. SET THIS —
	// leaving it empty is what makes the pod BestEffort and first to be evicted.
	// +optional
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`
}

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

	// Placement + resources for this control-plane singleton.
	RbeControlPlanePlacement `json:",inline"`
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

	// Placement + resources for this control-plane singleton.
	RbeControlPlanePlacement `json:",inline"`
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
	// No default: the worker image is deployment-specific, and a default naming
	// one account's registry would be a hosted-platform symbol in a package that
	// is deliberately host-agnostic. Set it explicitly on the RbeCluster.
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
	// Drain configures the preStop hook that tells the scheduler this worker is
	// leaving BEFORE it stops answering, so the handover stops landing new actions
	// on a pod that is about to die.
	//
	// ⛔ WITHOUT IT, A SCALE-DOWN SILENTLY EATS BUILDS. buildbarn#19 measured it on a
	// 3-worker fleet with live alternatives available throughout: 12 actions submitted
	// while one worker was terminating, FIVE landed on the dying worker — 42% — each
	// claimed by a pod with seconds to live and then SIGKILLed. Downstream that surfaces
	// as `UNAVAILABLE: Worker {…} disappeared while task was executing`, which reads as a
	// network fault rather than a scheduling one.
	//
	// ⚠ THIS IS NOT terminationGracePeriodSeconds, AND RAISING THAT IS THE WRONG FIX —
	// buildbarn's chart says so directly. The grace period lets a worker FINISH what it
	// holds, is paid on EVERY termination, and buys nothing past ~120s on spot (the
	// instance goes regardless). This stops it being GIVEN MORE, which costs nothing.
	// +optional
	Drain RbeWorkerDrainSpec `json:"drain,omitempty"`
	// BuildDirectory selects how a worker materializes an action's input root.
	//
	// ⭐ THIS IS THE LARGEST SINGLE LEVER ON BUILD WALL TIME, and it attacks demand
	// rather than supply. Measured on savvifi/aion run 31396048925 (a GREEN build):
	// input fetch 83.0% of remote action time against execute 2.1% -- a fetch:exec
	// ratio of 40 : 1. `native` materializes the ENTIRE input root on local disk before
	// the action runs, every file, whether it is read or not; `virtual` fetches blobs
	// lazily on read, so a tree nobody reads is never moved.
	//
	// ⚠ THE WIN IS CONCENTRATED IN A TAIL, SO MEASURE IT THERE. Per-action fetch time
	// is p50 1.9s but p99 634s (max 1018s), and half of all fetch time sits in the
	// slowest 5.4% of actions -- closures of 5-6 GB. Judging `virtual` on the MEAN will
	// understate it badly. One recorded case: 2,912s of transfer for 0.4s of work.
	// +optional
	BuildDirectory RbeWorkerBuildDirectorySpec `json:"buildDirectory,omitempty"`
	// Autoscaling, when enabled, has the operator manage a KEDA ScaledObject that
	// scales the worker Deployment on the buildbarn scheduler's queue backlog, so RBE
	// capacity tracks build demand (and scales down — even to zero — when idle). The
	// static Replicas becomes just the helm floor (set to MinReplicas). Node capacity
	// still caps the ceiling; a node autoscaler (Karpenter/CAS on the worker nodegroup)
	// is the complementary half.
	// +optional
	Autoscaling WorkerAutoscaling `json:"autoscaling,omitempty"`
}

// WorkerAutoscaling configures KEDA-driven worker autoscaling on buildbarn queue
// depth. The trigger is a Prometheus query for the backlog (tasks accepted but not
// completed = queued + executing); the operator scales replicas ≈ ceil(backlog /
// TargetBacklogPerReplica).
type WorkerAutoscaling struct {
	// Enabled turns on the managed KEDA ScaledObject for the worker Deployment.
	Enabled bool `json:"enabled,omitempty"`
	// MinReplicas is the worker floor. 0 = scale-to-zero when the queue is empty
	// (cheapest, but the first build after idle waits for a cold worker start).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	MinReplicas int32 `json:"minReplicas,omitempty"`
	// MaxReplicas is the worker ceiling. Cannot exceed what the worker nodegroup can
	// hold without node autoscaling.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=2
	// +optional
	MaxReplicas int32 `json:"maxReplicas,omitempty"`
	// TargetBacklogPerReplica is the queued+executing tasks each worker should carry;
	// replicas ≈ ceil(backlog / this). Empty ⇒ the worker Concurrency.
	// +kubebuilder:validation:Minimum=1
	// +optional
	TargetBacklogPerReplica int32 `json:"targetBacklogPerReplica,omitempty"`
	// PrometheusAddress is the Prometheus the KEDA trigger queries.
	// +kubebuilder:default="http://prometheus-server.monitoring.svc.cluster.local:80"
	// +optional
	PrometheusAddress string `json:"prometheusAddress,omitempty"`
	// CooldownSeconds is how long to wait after the backlog drains before scaling down.
	// +kubebuilder:default=300
	// +optional
	CooldownSeconds int32 `json:"cooldownSeconds,omitempty"`
	// Wake is the second trigger, and it is what makes MinReplicas: 0 usable.
	//
	// The backlog trigger CANNOT wake a pool from zero. With no workers the scheduler
	// REJECTS a task rather than enqueueing it, so tasks_scheduled_total never
	// increments, the Prometheus query stays empty, and KEDA never scales up. A closed
	// loop, measured 2026-07-26: bazel got FAILED_PRECONDITION "No workers exist", the
	// query returned 0, and the ScaledObject sat Active=False at 0/0.
	//
	// Wake watches a signal that exists BEFORE any of our own infrastructure knows a
	// build is coming, and polls a different system, so the two triggers have
	// independent failure domains. KEDA takes the max desired replicas across both.
	// +optional
	Wake *WorkerWake `json:"wake,omitempty"`
}

// WorkerWake configures the scale-from-zero trigger.
type WorkerWake struct {
	// GitHub scales on QUEUED GitHub Actions jobs — ONE ENTRY PER ORG.
	//
	// ⛔ A LIST, BECAUSE A SINGLE ENTRY SILENTLY STRANDS EVERY OTHER ORG. The scaler counts
	// queued jobs for exactly one `owner`, so with one entry and minReplicas 0 a job from
	// any other org queues against an idle fleet and HANGS: the scheduler rejects rather
	// than enqueues, tasks_scheduled_total never increments, the backlog query stays empty,
	// and nothing wakes. That is the documented deadlock the floor was raised to 1 to avoid,
	// arriving through a different door — and it reports nothing, because a fleet with no
	// work to do and a fleet that cannot see its work look identical.
	//
	// ⭐ One TriggerAuthentication serves them all. The orgs share a GitHub App, so they
	// share a private key; only the ids differ, and those are trigger metadata rather than
	// auth params (see WorkerWakeGitHub.ApplicationID). Adding an org is a CR edit, not a
	// new Secret.
	//
	// ⚠ Owners must be unique. KEDA derives each trigger's metric name from the scaler type
	// and the owner, so two entries with the same owner collide on one metric name and the
	// HPA reads whichever KEDA registered last.
	// +optional
	// +listType=map
	// +listMapKey=owner
	GitHub []WorkerWakeGitHub `json:"github,omitempty"`

	// ArcListener scales on ARC's OWN queue depth instead of polling GitHub.
	//
	// ⭐ PREFER THIS OVER GitHub ABOVE. The ARC listener holds a long-poll session with the
	// Actions service, so the queue depth is PUSHED to it; it publishes that as
	// `gha_assigned_jobs`. Reading it costs ZERO GitHub API calls.
	//
	// ⛔ WHAT IT REPLACES, AND WHY THAT MATTERS. GitHub exposes no org-wide queue depth, so
	// the github-runner scaler has to ENUMERATE: list the installation's repos, then GET
	// /actions/runs on each, every pollingInterval. Measured on this estate:
	//
	//     72 repos x 120 polls/hr  =  ~8,640 API calls/hr   vs a 5,000/hr floor
	//
	// exhausted roughly every 35 minutes. And a scaler that errors takes the ScaledObject's
	// WHOLE scalers cache with it, so rate-limiting one org disarms every other trigger on
	// the pool -- including the backlog trigger that does the real sizing. The workaround
	// was to scope the trigger to named repos, which just moves the failure: a repo left off
	// the list queues against an idle fleet and hangs.
	//
	// ⭐ This has none of that. One trigger covers every org with a scale set, forever, with
	// no credential and no per-repo list to keep in step.
	// +optional
	ArcListener *WorkerWakeArcListener `json:"arcListener,omitempty"`
}

// WorkerWakeArcListener scales the pool on ARC's published queue depth.
//
// Uses the autoscaling block's own PrometheusAddress -- the metric lands in the same
// Prometheus as the backlog metric, so a second address would only be a way to get one of
// them wrong.
type WorkerWakeArcListener struct {
	// Enabled turns the trigger on.
	Enabled bool `json:"enabled,omitempty"`
	// Threshold is assigned jobs per worker replica.
	//
	// 1 is the honest default: one job assigned to a runner scale set is at least one
	// build's worth of parallel RBE actions, and the backlog trigger takes over sizing the
	// moment real actions arrive. This trigger's job is only to get OFF ZERO.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	Threshold int32 `json:"threshold,omitempty"`
}

// WorkerWakeGitHub is a KEDA github-runner trigger.
type WorkerWakeGitHub struct {
	// Enabled turns the trigger on. Off by default: it needs credentials, and a
	// half-configured trigger reports failure only on the ScaledObject's Ready
	// condition, which is exactly where a broken ScaledObject already hides.
	Enabled bool `json:"enabled,omitempty"`
	// Owner is the GitHub org (or user) whose queued jobs are counted.
	// +kubebuilder:validation:MinLength=1
	Owner string `json:"owner"`
	// RunnerScope is org, repo or ent. `org` matches org-scoped runner sets.
	// +kubebuilder:validation:Enum=org;repo;ent
	// +kubebuilder:default=org
	// +optional
	RunnerScope string `json:"runnerScope,omitempty"`
	// Repos optionally restricts counting to specific repositories. Empty = whole scope.
	// +optional
	Repos []string `json:"repos,omitempty"`
	// Labels optionally counts only jobs whose runs-on carries these labels.
	// +optional
	Labels []string `json:"labels,omitempty"`
	// TargetWorkflowQueueLength is queued jobs per worker replica. 1 is the honest
	// default: one queued CI job is at least one build's worth of parallel actions, and
	// the backlog trigger takes over sizing the moment real actions arrive.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=1
	// +optional
	TargetWorkflowQueueLength int32 `json:"targetWorkflowQueueLength,omitempty"`
	// ApplicationID is the GitHub App's numeric App ID.
	//
	// ⛔ THIS IS NOT A SECRET AND IT DOES NOT GO IN THE TriggerAuthentication. KEDA's
	// github-runner scaler reads applicationID and installationID from the trigger
	// METADATA and only appKey from authParams. Supplying them via a
	// TriggerAuthentication instead fails as
	//
	//     error resolving auth params:
	//     error parsing GitHub Runner metadata: error parsing applicationID:
	//     no applicationID given
	//
	// ⚠ whose "resolving auth params" wording sends you to the TriggerAuthentication --
	// the one place the value must NOT be. Measured against KEDA 2.16.1; a
	// TriggerAuthentication naming every parameter correctly still produced this.
	//
	// Neither id is a credential: App IDs are public via /apps/{slug} and an
	// installation id is a handle, not an authorization. The private key is the secret,
	// and it stays in AuthenticationRef.
	// +kubebuilder:validation:Minimum=1
	ApplicationID int64 `json:"applicationID"`
	// InstallationID is this app's installation on Owner. See ApplicationID: metadata,
	// not authParams.
	// +kubebuilder:validation:Minimum=1
	InstallationID int64 `json:"installationID"`
	// AuthenticationRef names a KEDA TriggerAuthentication holding the GitHub App
	// PRIVATE KEY, under the parameter `appKey` -- and nothing else. See ApplicationID.
	//
	// ⚠ It is resolved in the ScaledObject's OWN namespace, not KEDA's. The
	// TriggerAuthentication and the Secret it points at must both live beside the
	// RbeCluster.
	// +kubebuilder:validation:MinLength=1
	AuthenticationRef string `json:"authenticationRef"`
}

// RbeStorageSpec configures the sharded CAS/AC blobstore (EBS-backed).
// RbeWorkerDrainSpec is the worker preStop drain (buildbarn chart >= 0.4.6,
// worker.drain.preStop).
//
// ⚠ IT DOES NOT MAKE WORKERS STICKY, AND MUST NOT. Workers deliberately carry no PDB
// and no karpenter.sh/do-not-disrupt so they stay freely consolidatable, and a lost
// worker's action is retried. This changes the HANDOVER, not the disruptability.
type RbeWorkerDrainSpec struct {
	// Enabled turns the preStop hook on.
	//
	// ⛔ ENABLING THIS WITHOUT Image IS A FLEET-WIDE OUTAGE, not a no-op: every worker
	// pod fails to start on ImagePullBackOff. That is why the chart defaults it off and
	// why Image has no default here — a default naming one account's registry would be
	// a hosted-platform symbol in a host-agnostic package, the same reason
	// ContainerImage has none.
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// Image is the rbe-drain image, staged by an initContainer onto the shared volume
	// and exec'd by the hook from inside bb-worker.
	//
	// ⚠ PIN BY DIGEST, AND MAKE SURE THAT DIGEST IS TAGGED. ECR lifecycle policies in
	// this estate expire untagged images beyond one, so a digest-pinned UNTAGGED image
	// is a single unrelated push from being expired out from under a live fleet — the
	// failure that cost roma-cache 28 hours, and fastverk-operator and badge before it.
	// +optional
	Image string `json:"image,omitempty"`
	// SchedulerAddress is the scheduler's BuildQueueState surface.
	//
	// ⛔ PORT 8984, NOT 8983 (workers) OR 8982 (clients). It was declared in
	// scheduler.jsonnet from the start and published on no Service until chart 0.4.6, so
	// every call to it failed at connect. Empty = the chart's in-namespace default.
	// +optional
	SchedulerAddress string `json:"schedulerAddress,omitempty"`
	// Timeout bounds the hook.
	//
	// ⚠ SPENT FROM terminationGracePeriodSeconds, so this is a BUDGET, not a limit to be
	// generous with: every second here is a second the in-flight action does not get. Its
	// real job is to bound the case where the scheduler is unreachable — where draining is
	// pointless anyway. Empty = the chart default (5s).
	// +optional
	Timeout string `json:"timeout,omitempty"`
}

type RbeWorkerBuildDirectorySpec struct {
	// Type is `native` (materialize the whole input root up front) or `virtual`
	// (FUSE/NFS input root, blobs fetched lazily on read). Empty = the chart default,
	// which is `native` -- the historical behavior, so an unset field changes nothing.
	//
	// ⛔ `virtual` IS THE HIGHEST-RISK KNOB IN THIS SPEC. It changes how every action
	// sees its filesystem: actions that mmap, that walk the whole tree, or that assume
	// local-disk semantics can behave differently. Land it ALONE, never in the same
	// pass as a CAS sharding change, or a broken build cannot be attributed.
	//
	// ⚠ IT ALSO REQUIRES PRIVILEGE THE CHART OTHERWISE NEVER GRANTS: bb-worker needs
	// /dev/fuse and SYS_ADMIN, emitted ONLY when this is `virtual` so a default install
	// gains nothing. The `runner` sidecar runs allowPrivilegeEscalation:false as uid
	// 65534 and shares /worker, so it must still SEE the mount -- mountPropagation on
	// that shared volume is the coupling point and the likeliest thing to get wrong.
	//
	// ⚠ Its benefit is read through the FileSystemAccessCache, which learns each
	// action's read set. Resharding storage EMPTIES the FSAC, so measuring `virtual`
	// straight after a shard change understates it and blames the wrong knob. Put a
	// warm-up build between them.
	// +kubebuilder:validation:Enum=native;virtual
	// +optional
	Type string `json:"type,omitempty"`
}

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

	// StorageClassName is the StorageClass backing the CAS/AC/FSAC volumes. Empty leaves
	// the chart default (`gp3`, which the chart also creates, backed by ebs.csi.aws.com).
	//
	// ⚠ REQUIRED ON ANY NON-AWS CLUSTER, alongside platform: generic. kind ships
	// `standard` (rancher.io/local-path) and minikube ships `standard`
	// (k8s.io/minikube-hostpath); neither can serve a claim bound to an EBS class, and the
	// failure is a PVC that stays Pending with the reason only in an event nobody reads.
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`

	// VolumeAttributesClassName names a VolumeAttributesClass applied to the CAS
	// volume (k8s 1.31+, EBS CSI 1.35+), e.g. "cas-fast" for 16000 IOPS / 1000 MB/s.
	//
	// ⛔ THIS IS THE ONLY WAY TO CHANGE CAS THROUGHPUT WITHOUT A COLD CACHE. A
	// StorageClass's parameters are immutable, so raising iops/throughput there means
	// a new class, new volumes, and a CAS rebuild measured at ~1.6h on the fastverk
	// cluster. Empty leaves gp3's 3000 IOPS / 125 MB/s baseline — which was the live
	// bottleneck for an unknown period after storage moved onto m5n.2xlarge for its
	// network and nobody re-measured. A throttled volume raises no error; it presents
	// as "the RBE is slow", which sends you to the scheduler and the worker pool.
	// +optional
	VolumeAttributesClassName string `json:"volumeAttributesClassName,omitempty"`

	// Placement + resources for this control-plane singleton.
	RbeControlPlanePlacement `json:",inline"`
}

// RbeBrowserSpec configures bb-browser, the action-inspection UI.
//
// ⚠ IT IS A DEBUGGING TOOL, NOT PART OF THE BUILD PATH. No action depends on it and no
// client dials it, so it is the one component whose placement is a pure cost decision —
// which is exactly why it must be expressible here. At 3 replicas with no placement of
// its own it inherited the chart-global `workload: rbe-worker` selector and pinned three
// 8-vCPU nodes against `consolidationPolicy: WhenEmpty`; two of them ran a browser pod
// and nothing else, so the worker pool could never consolidate back to its floor. That
// cost roughly $500/month to show a UI nobody had open.
type RbeBrowserSpec struct {
	// Enabled turns the browser Deployment on. Default true — it is genuinely useful
	// when an action fails opaquely, and the fix for the incident above was placement
	// and replica count, not removal.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Replicas. Default 1, and there is no reason to raise it: it serves interactive
	// human traffic with no availability requirement.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:default=1
	// +optional
	Replicas int32 `json:"replicas,omitempty"`

	// Placement + resources. SET NodeSelector to the control pool — the whole point of
	// this type is to keep the browser off worker nodes.
	RbeControlPlanePlacement `json:",inline"`
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
	// Platform selects the deployment profile. This is deliberately a PROFILE and not a
	// pile of individual knobs.
	//
	// ⛔ WITHOUT IT THE OPERATOR CANNOT INSTALL ANYWHERE BUT AWS. The buildbarn chart
	// defaults to `nodeSelector: {workload: rbe-worker}` and creates its own gp3
	// StorageClass with the ebs.csi.aws.com provisioner. On any other cluster every pod
	// sits Pending and every PVC sits unbound — measured on kind 2026-08-03, all five
	// components Pending, with no error naming the cause.
	//
	// ⚠ AND IT CANNOT BE EXPRESSED PER-COMPONENT. The chart's placement helper is
	// `$component.nodeSelector | default $global.nodeSelector`, and Helm's `default`
	// treats an EMPTY MAP as absent — so setting a component's nodeSelector to {} falls
	// straight back to the AWS default. Only an explicit null at the chart-global level
	// clears it, which is why this is a top-level profile rather than a field on each
	// component.
	//
	// "aws" (default) keeps the existing behaviour exactly. "generic" clears the
	// chart-global placement and does not create a StorageClass, for kind, minikube, or
	// any cluster that is not the fastverk-shaped one — which is what
	// build-runner-seam.md's "runs in ANY cluster" promise requires.
	// +kubebuilder:validation:Enum=aws;generic
	// +kubebuilder:default=aws
	// +optional
	Platform string `json:"platform,omitempty"`

	// +optional
	Browser RbeBrowserSpec `json:"browser,omitempty"`
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
