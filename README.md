# rbe-api

Go types and CRD schemas for the **tbzl RBE control plane**: `RbeCluster` (a Buildbarn
remote-build-execution cluster) and `WorkerPool` (a pool of workers of one shape).

```go
import rbev1 "github.com/tomato-bazel/rbe-api/api/v1"
```

## Why this is public, and separate

Two different things need these types, and they live in different repos and different orgs:

| | |
|---|---|
| **reconciles** them | `tomato-bazel/tbzl-build-operator` (private) |
| **embeds** them | fastverk's `FastverkInstance` carries an `RbeClusterSpec` inline and creates child `RbeCluster` CRs |

A private module cannot serve the second. CI on the consuming side cannot fetch it — that
failure has already reverted one merged change elsewhere in this fleet, with
`fatal: could not read Username for 'https://github.com'`. Publishing **only the schema**
keeps a single definition of the contract while every line of implementation stays private.

Nothing here is sensitive: a CRD schema is applied into clusters and readable by anyone with
cluster access. The one deployment-specific value that *was* baked in — a default worker image
naming a particular AWS account's registry — has been removed, both because it does not belong
in a public package and because it is a hosted-platform symbol in an API that is deliberately
host-agnostic. Set `containerImage` explicitly on the `RbeCluster`.

## The group is still `fastverk.savvifi.com/v1`

Deliberately. Re-homing the *types* is a pure code move. Re-homing the *API group* is a live
migration on the CRDs that run a build fleet, and conflating the two orphans every existing CR.

When the group does move, it moves additively:

1. publish the new group alongside the old; the controller reconciles both
2. dual-write, and confirm the reconciled output is **byte-identical**
3. cut over
4. verify by effect — worker slots, queue depth, a real build
5. only then retire the old group

## Provenance

Carved from `fastverk/deploy` `operator/api/v1/` **with history**, so `git blame` still explains
every field. The generated CRDs were diffed against the originals to prove the move was
faithful: `workerpools` came out byte-identical, and `rbeclusters` differs only by the removed
default described above.

## Regenerating

```sh
controller-gen object:headerFile=hack/boilerplate.go.txt paths=./api/...
controller-gen crd paths=./api/... output:crd:artifacts:config=config/crd
```
