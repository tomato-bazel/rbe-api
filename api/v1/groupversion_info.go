// Package v1 contains the API schema for the tbzl RBE control plane — the
// RbeCluster and WorkerPool CRDs describing a Buildbarn remote-build-execution
// cluster and its worker pools.
//
// WHY THIS PACKAGE IS ITS OWN PUBLIC MODULE. Two things need these types and they
// live in different places and different orgs:
//
//   - the operator that RECONCILES them (tomato-bazel/tbzl-build-operator, private)
//   - the platforms that EMBED them — fastverk's FastverkInstance carries an
//     RbeClusterSpec inline and creates child RbeCluster CRs
//
// A private module cannot satisfy the second: CI on the consuming side cannot fetch
// it, which has already caused one merged change to be reverted elsewhere in this
// fleet. Publishing only the schema keeps one definition of the contract while the
// implementation stays private. Nothing here is sensitive — a CRD schema is applied
// into clusters and is readable by anyone with cluster access.
//
// THE GROUP IS DELIBERATELY STILL fastverk.savvifi.com/v1. Re-homing the types is a
// pure code move; re-homing the API GROUP is a live migration on the CRDs that run
// the build fleet, and the two must not be confused. That migration is additive when
// it happens: publish the new group alongside the old, have the controller reconcile
// both, confirm the reconciled output is byte-identical, cut over, verify by effect,
// and only then retire the old group. Changing the constant below without that ladder
// orphans every existing CR.
//
// +kubebuilder:object:generate=true
// +groupName=fastverk.savvifi.com
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group/version used to register these objects.
	GroupVersion = schema.GroupVersion{Group: "fastverk.savvifi.com", Version: "v1"}

	// SchemeBuilder registers the group/version's Go types with a Scheme.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the group/version's types to a Scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
