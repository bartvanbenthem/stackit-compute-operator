# STACKIT Service Operator for Compute

A Kubernetes operator that manages the lifecycle of [STACKIT](https://www.stackit.de/)
Compute Engine and Kubernetes Engine resources through four custom
resources: `Server`, `Volume`, `Network` (Compute Engine / IaaS), and
`Cluster` (STACKIT Kubernetes Engine / SKE). Broader STACKIT networking
(routing, VPCs) beyond these four resources is out of scope for this
version. Images are referenced by STACKIT UUID only (`Server.spec.imageId`)
rather than modeled as a resource: their lifecycle (upload, availability) is
entirely STACKIT's to manage and can't be driven declaratively from
Kubernetes.

Built on the [official STACKIT Go SDK](https://github.com/stackitcloud/stackit-sdk-go)
(`services/iaas/v2api` and `services/ske/v2api`).

## API

`compute.sostackit.dev/v1alpha1` defines `Server`, `Volume`, `Network`, and
`Cluster` — see [api/v1alpha1](api/v1alpha1) for the types
and [config/samples](config/samples) for examples. Each has a matching
controller under [internal/controller](internal/controller) that follows
the same pattern:

- Creates the resource in STACKIT when its status ID is empty; a finalizer
  (e.g. `compute.sostackit.dev/server-finalizer`) guarantees deletion
  follows `kubectl delete`.
- Mirrors STACKIT's observed status onto `.status` and sets a `Ready`
  condition summarizing reconciliation state.
- Recreates the resource if it disappears from STACKIT out of band (owned
  resources only, see [Existing resources](#existing-resources-bring-your-own)
  below).
- Reconciles a limited set of drift (see each type's `_controller.go` for
  exactly which fields): e.g. Server reconciles `spec.machineType` (resize),
  `spec.powerState` (start/stop), and `spec.name`/`spec.labels` (update)
  once in a steady state (`ACTIVE`/`INACTIVE`).

### Existing resources ("bring your own")

`Volume` and `Network` each support a `spec.existingId` field. If
set, the operator treats the resource as **not owned**: it only observes the
STACKIT object at that ID (via `GET`) and never creates, updates, or
deletes it, and never adds a finalizer, deleting the Kubernetes object is a
no-op against STACKIT. Leave `existingId` unset for the operator to own the
resource's full lifecycle instead. Changing `existingId` after a resource
has already been created or adopted is unsupported (there is no webhook to
guard against it).

`Cluster` supports the same bring-your-own pattern through
`spec.existingClusterName` instead of `spec.existingId`: SKE has no
server-assigned UUID of its own, the cluster name doubles as its only
identifier, so the adopt field takes a name rather than a UUID. Semantics
are otherwise identical (observe-only, no finalizer, `spec.kubernetesVersion`
and `spec.nodePools` are ignored and may be left unset).

### Referencing Volume/Network from Server

`Server` can reference a `Network`/`Volume` resource by name instead of a
raw STACKIT ID:

```yaml
spec:
  imageId: "<uuid>"        # images are always referenced by raw STACKIT UUID
  networkRef:
    name: prod-network      # instead of networkId: "<uuid>"
  bootVolumeRef:
    name: web01-boot        # boots from an existing Volume instead of
                             # creating a new boot volume from the image
```

Images have no corresponding CRD: their lifecycle (upload, availability) is
entirely STACKIT's to manage, so `spec.imageId` always takes a raw STACKIT
UUID rather than a reference to a Kubernetes object.

A ref is resolved to the referenced resource's `status.<x>Id` at server
creation time; if that resource isn't Ready yet, the Server just waits and
retries (no error). Setting both a ref and its raw-ID counterpart (e.g. both
`networkId` and `networkRef`) is a validation error surfaced as
`Ready=False/InvalidReference`, only one of each pair is allowed. See
[config/samples/compute_v1alpha1_server_with_refs.yaml](config/samples/compute_v1alpha1_server_with_refs.yaml)
for referencing already-existing resources, or
[config/samples/full_stack-test.yaml](config/samples/full_stack-test.yaml)
for a Network/Volume/Server created together in one file.

`bootVolumeRef` fixes a specific gap: without it, a server's boot volume is
created implicitly as part of `CreateServerPayload`, a real STACKIT volume
whose state (size, status) was previously invisible to Kubernetes and never
reconciled. Using `bootVolumeRef` makes the boot volume a first-class
`Volume` resource with its own status and drift reconciliation (e.g.
resize), created and observed independently of the Server that boots from
it.

### Clusters (STACKIT Kubernetes Engine)

`Cluster` reconciles against a different STACKIT API (SKE, not IaaS) with a
different resource model, so it differs from `Server`/`Volume`/`Network`
in a few ways:

- SKE identifies a cluster purely by name (there's no separate UUID), and
  its create/update endpoint (`CreateOrUpdateCluster`) is a single
  idempotent upsert used for both. This operator uses it for creation *and*
  for correcting drift, resubmitting the whole desired
  `kubernetesVersion`/`nodePools`/`maintenance` on any detected change
  rather than a partial patch.
- `spec.nodePools` configures worker node pools (machine type/image, size
  bounds, availability zones, volume); at least one pool must set
  `allowSystemComponents: true`, matching SKE's own requirement.
- `spec.maintenance` is optional and all-or-nothing: set
  `autoUpdateKubernetesVersion`/`autoUpdateMachineImageVersion` and a
  `start`/`end` time window together, or omit the whole section to keep
  SKE's own default maintenance window.
- `status.state` mirrors SKE's aggregated cluster state (e.g.
  `STATE_HEALTHY`, `STATE_CREATING`, `STATE_UNHEALTHY`, `STATE_HIBERNATED`);
  the `Ready` condition is `True` for both `STATE_HEALTHY` and
  `STATE_HIBERNATED` (a hibernated cluster is a valid steady state, not an
  error).
- This operator doesn't manage kubeconfig retrieval, hibernation
  scheduling, credential rotation, or the cluster's `access`/`extensions`
  settings; see [config/samples/compute_v1alpha1_cluster.yaml](config/samples/compute_v1alpha1_cluster.yaml)
  for the fields it does manage.

## Authentication

The operator uses the SDK's default credential resolution, no STACKIT
config is written by this code. Provide a service account key and its
private key (STACKIT's "Key Flow") as a Kubernetes Secret in the operator's
namespace:

```bash
kubectl create secret generic stackit-credentials \
  --namespace stackit-compute-operator-system \
  --from-file=service-account-key.json=./service-account-key.json \
  --from-file=private-key.pem=./private-key.pem
```

[config/manager/manager.yaml](config/manager/manager.yaml) mounts that
secret and sets `STACKIT_SERVICE_ACCOUNT_KEY_PATH` /
`STACKIT_PRIVATE_KEY_PATH` accordingly. The same credentials authenticate
both the IaaS and SKE API clients. `spec.projectId` and `spec.region` are
set per-resource (`Server`, `Volume`, `Network`, `Cluster`), so one
operator instance can manage resources across multiple STACKIT
projects/regions as long as the service account has access.

## Development

```bash
go mod tidy          # resolve dependencies (needs network access)
make build            # compile ./bin/manager
make test             # go vet + go test (fast unit tests, no external binaries)
make test-integration # runs internal/controller's envtest-backed integration test
make install          # apply the CRD
make run              # run the manager locally against your current kubeconfig
```

`make test` covers payload construction ([internal/stackit](internal/stackit)) and
reconcile logic ([internal/controller](internal/controller)) against a fake
Kubernetes client and each STACKIT SDK's own `DefaultAPIServiceMock` (IaaS's
and SKE's), no network or external binaries required, for all four resource
types including owned, `existingId`-adopted, and (for `Cluster`)
`existingClusterName`-adopted reconcile paths. `make
test-integration` additionally downloads envtest (a real `kube-apiserver` +
`etcd`) on first run and drives the actual controller-runtime manager
through full lifecycles for `Server`, `Volume`, `Network`, and `Cluster`
(create → ready → delete; `Server` also covers power off) against stateful
in-memory STACKIT fakes, plus adopt-mode scenarios confirming an adopted
`Volume`'s and `Cluster`'s underlying STACKIT resource survives CR deletion,
to catch issues the fake-client tests can't (finalizer/status subresource
semantics, requeue timing, watch-triggered reconciles). Cross-controller
behavior (e.g. a Server waiting on a not-yet-ready `networkRef`) is covered
at the fake-client unit level only, not against envtest.

To build and deploy the container image:

```bash
make docker-build docker-push IMG=<registry>/stackit-compute-operator:tag
make deploy IMG=<registry>/stackit-compute-operator:tag
```

`make manifests` / `make generate` regenerate the CRD YAML
(`config/crd/bases`), `config/rbac/role.yaml`, and
`zz_generated.deepcopy.go` from the Go type markers in
[api/v1alpha1](api/v1alpha1) and the `+kubebuilder:rbac` markers on each
`*_controller.go`; both require
[`controller-gen`](https://book.kubebuilder.io/reference/controller-gen) to
be installed. Re-run after changing any `api/v1alpha1/*_types.go` file or
any controller's RBAC markers - if `controller-gen` isn't available, these
generated files must be hand-edited to match instead.

## Notes

- The Go module path (`github.com/bartvanbenthem/stackit-compute-operator`) and
  API group domain (`compute.sostackit.dev`) are placeholders, rename them
  to match wherever this repo actually lives before publishing.
- `go.sum` is not checked in; run `go mod tidy` once you have network access
  to populate it.
