# STACKIT Service Operator for Compute

A Kubernetes operator that manages the lifecycle of [STACKIT](https://www.stackit.de/)
Compute Engine resources through four custom resources: `Server`, `Volume`,
`Image`, and `Network`. It's scoped to Compute Engine (IaaS) — broader
STACKIT networking (routing, VPCs) beyond these four resources is out of
scope for this version.

Built on the [official STACKIT Go SDK](https://github.com/stackitcloud/stackit-sdk-go)
(`services/iaas/v2api`).

## API

`compute.sostackit.dev/v1alpha1` defines `Server`, `Volume`, `Image`, and
`Network` — see [api/v1alpha1](api/v1alpha1) for the types and
[config/samples](config/samples) for examples. Each has a matching
controller under [internal/controller](internal/controller) that follows
the same pattern:

- Creates the resource in STACKIT when its status ID is empty; a finalizer
  (e.g. `compute.sostackit.dev/server-finalizer`) guarantees deletion
  follows `kubectl delete`.
- Mirrors STACKIT's observed status onto `.status` and sets a `Ready`
  condition summarizing reconciliation state.
- Recreates the resource if it disappears from STACKIT out of band (owned
  resources only — see [Existing resources](#existing-resources-bring-your-own)
  below).
- Reconciles a limited set of drift (see each type's `_controller.go` for
  exactly which fields): e.g. Server reconciles `spec.machineType` (resize),
  `spec.powerState` (start/stop), and `spec.name`/`spec.labels` (update)
  once in a steady state (`ACTIVE`/`INACTIVE`).

### Existing resources ("bring your own")

`Volume`, `Image`, and `Network` each support a `spec.existingId` field. If
set, the operator treats the resource as **not owned**: it only observes the
STACKIT object at that ID (via `GET`) and never creates, updates, or
deletes it, and never adds a finalizer — deleting the Kubernetes object is a
no-op against STACKIT. Leave `existingId` unset for the operator to own the
resource's full lifecycle instead. Changing `existingId` after a resource
has already been created or adopted is unsupported (there is no webhook to
guard against it).

### Referencing Volume/Image/Network from Server

`Server` can reference an `Image`/`Network`/`Volume` resource by name
instead of a raw STACKIT ID:

```yaml
spec:
  imageRef:
    name: ubuntu-22-04      # instead of imageId: "<uuid>"
  networkRef:
    name: prod-network      # instead of networkId: "<uuid>"
  bootVolumeRef:
    name: web01-boot        # boots from an existing Volume instead of
                             # creating a new boot volume from the image
```

A ref is resolved to the referenced resource's `status.<x>Id` at server
creation time; if that resource isn't Ready yet, the Server just waits and
retries (no error). Setting both a ref and its raw-ID counterpart (e.g. both
`imageId` and `imageRef`) is a validation error surfaced as
`Ready=False/InvalidReference` — only one of each pair is allowed. See
[config/samples/compute_v1alpha1_server_with_refs.yaml](config/samples/compute_v1alpha1_server_with_refs.yaml)
for referencing already-existing resources, or
[config/samples/compute_v1alpha1_full_stack.yaml](config/samples/compute_v1alpha1_full_stack.yaml)
for a Network/Image/Volume/Server created together in one file.

`bootVolumeRef` fixes a specific gap: without it, a server's boot volume is
created implicitly as part of `CreateServerPayload` — a real STACKIT volume
whose state (size, status) was previously invisible to Kubernetes and never
reconciled. Using `bootVolumeRef` makes the boot volume a first-class
`Volume` resource with its own status and drift reconciliation (e.g.
resize), created and observed independently of the Server that boots from
it.

### Images and the upload-bytes gap

Creating an `Image` only registers its metadata in STACKIT and returns an
upload URL (`status.uploadUrl`); STACKIT does not make the image available
until its bytes are PUT to that URL, which this operator has no declarative
way to do. A created (not adopted) `Image` therefore stays
`Ready=False/AwaitingUpload` until the bytes are uploaded out-of-band and a
later reconcile observes `status.state == AVAILABLE`. In practice, most
`Image` usage is expected to be `spec.existingId` (adopt an
already-prepared image) rather than creating one through this operator.

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
`STACKIT_PRIVATE_KEY_PATH` accordingly. `spec.projectId` and `spec.region`
are set per-resource (`Server`, `Volume`, `Image`, `Network`), so one
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
Kubernetes client and the STACKIT SDK's own `DefaultAPIServiceMock` — no
network or external binaries required, for all four resource types
including owned and `existingId`-adopted reconcile paths. `make
test-integration` additionally downloads envtest (a real `kube-apiserver` +
`etcd`) on first run and drives the actual controller-runtime manager
through full lifecycles for `Server`, `Volume`, `Image`, and `Network`
(create → ready → delete; `Server` also covers power off) against stateful
in-memory STACKIT fakes, plus an adopt-mode scenario confirming an adopted
`Volume`'s underlying STACKIT resource survives CR deletion — to catch
issues the fake-client tests can't (finalizer/status subresource semantics,
requeue timing, watch-triggered reconciles). Cross-controller behavior
(e.g. a Server waiting on a not-yet-ready `imageRef`/`networkRef`) is
covered at the fake-client unit level only, not against envtest.

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
  API group domain (`compute.sostackit.dev`) are placeholders — rename them
  to match wherever this repo actually lives before publishing.
- `go.sum` is not checked in; run `go mod tidy` once you have network access
  to populate it.
