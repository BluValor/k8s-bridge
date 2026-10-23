---
name: test-unit-and-lint
description: >-
  Writes and runs table-driven Go unit tests, envtest controller integration
  tests, Slurm REST client mocks, and static analysis linters via make test,
  make fmt, make vet, and make lint. Use when adding or debugging package-level
  tests, mocking Slurm REST API responses, or fixing lint and formatting
  failures. Don't use for running full-cluster end-to-end Ginkgo tests on Kind
  or cloud GPU clusters (use test-e2e-cluster).
---

# Unit Testing, `envtest`, and Linting Workflow (`test-unit-and-lint`)

## 1. Preconditions & Inputs

Before writing or running unit/`envtest` tests and linters, verify:
- **Working directory:** Repository root (`k8s-bridge`).
- **Required tools:** `go` (`1.26+`), `make`.
  - `make test` downloads `setup-envtest` and Kubernetes `envtest` binaries (`kube-apiserver` and `etcd`) into `./bin/k8s/`.
  - `make lint` downloads `golangci-lint` (`v2.12.2`) into `./bin/`.
- **Test file locations:**
  - `cmd/main_test.go`
  - `internal/controller/slurmjob/translator_test.go`, `poller_test.go`, `slurmjob_controller_test.go`, `slurmjob_controller_helper_test.go`, `teardown_reconciler_test.go`, `slurm_utils_test.go`
  - `internal/controller/workloadmixing/workloadmixing_controller_test.go`
  - `internal/controller/slurmclientsync/slurmclient_controller_test.go`, `slurmjwt/token_test.go`
  - `internal/clientmap/clientmap_test.go`
  - `test/utils/slurmmock.go`: Shared mock `slurmclient.Client` implementation for job list/get/update/delete tests.

---

## 2. Guardrails

- **Select the Slurm client mock for the target component:**
  1. Use `&utils.SlurmClient{}` (`test/utils/slurmmock.go`) when testing `SlurmPoller`, `SlurmJobReconciler`, or `JobTeardownReconciler`. It tracks `V0044JobInfo`, `V0044JobInfoList`, and `V0044Node` deletions (`SetPendingJobs`, `GetUpdatedJobs`, `GetDeletedJobs`, `GetDeletedNodes`, `MutateOnUpdate`, `GetError`, `UpdateError`, `DeleteNodeError`). It does **not** track `V0041ControllerPing` or inject `List` or job-`Delete` errors.
  2. Use `slurmclientfake.NewClientBuilder().WithInterceptorFuncs(...).Build()` (`github.com/SlinkyProject/slurm-client/pkg/client/fake`) when testing `V0041ControllerPing` in `SlurmClientReconciler` or custom `List`/job-`Delete` error interceptors.
- **Register all required schemes on fake Kubernetes clients:** Register `k8sbridgev1alpha1.AddToScheme`, `jobsetv1alpha2.AddToScheme`, `corev1.AddToScheme`, and `slinkyv1beta1.AddToScheme` when constructing `fake.NewClientBuilder().WithScheme(scheme)`.
- **Populate `ClientMap` before calling `Reconcile`:** Reconcilers and `SlurmPoller` look up the Slurm client in `ClientMap` by the `Controller` `types.NamespacedName` (`Namespace: wm.Namespace, Name: wm.Spec.ControllerRef.Name`).
- **Format and lint after every test or code edit:** Run `make fmt vet lint` before finishing.

---

## 3. Step-by-Step Procedure

### Step 1: Write Table-Driven Unit Tests or Fake-Client Reconciler Tests
1. Read [examples/mock-client-pattern.go](examples/mock-client-pattern.go) for the scheme setup, `fake.NewClientBuilder()` initialization, `&utils.SlurmClient{}` configuration, and `ClientMap` wiring pattern.
2. When constructing test `slurmclienttypes.V0044JobInfo` objects:
   - Set pointer fields with `ptr.To(...)` (`JobId: ptr.To(int32(123))`, `Hold: ptr.To(true)`, `Partition: ptr.To("k8s-partition")`, `Comment: ptr.To("k8s-bridge-auto-hold")`).
   - Set numeric `NoValStruct` fields with `Set: ptr.To(true)` and `Number: ptr.To(int32(1))`:

```go
NodeCount: &slurmapi.V0044Uint32NoValStruct{
	Set:    ptr.To(true),
	Number: ptr.To(int32(2)),
},
```

3. For `SlurmJobReconciler.Reconcile`, pass the deduplicated request name `"slurm-job-<wmName>-<jobID>"`:

```go
req := ctrl.Request{
	NamespacedName: types.NamespacedName{
		Namespace: "slurm",
		Name:      "slurm-job-default-123",
	},
}
```

### Step 2: Write or Update `envtest` Controller Integration Specs
1. In `internal/controller/workloadmixing/suite_test.go`, `envtest.Environment` loads `CRDDirectoryPaths` from `config/crd/bases` (`filepath.Join("..", "..", "..", "config", "crd", "bases")`) and discovers `envtest` binaries in `./bin/k8s/` via `getFirstFoundEnvTestBinaryDir()`.
2. If you added new CRD fields in `api/v1alpha1/`, run `make manifests generate` first so `config/crd/bases/` is up to date before running `envtest`.

### Step 3: Run Targeted Package Tests for Fast Iteration
To run a single package or test function without running the entire repository suite:

```bash
go test -v ./internal/controller/slurmjob/... -run TestTranslate
```

To run `envtest` specs in `internal/controller/workloadmixing` (`ENVTEST_K8S_VERSION` in `Makefile` resolves to `1.36` from `k8s.io/api` `v0.36.0`):

```bash
make setup-envtest
KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.36 --bin-dir $(pwd)/bin -p path)" \
  go test -v ./internal/controller/workloadmixing/...
```

### Step 4: Run Full Unit Suite and Static Analysis Linters
Run the full unit test suite (all non-`/e2e` packages with coverage output to `cover.out`) and linters:

```bash
make fmt vet lint test
```

If `make lint` reports formatting or auto-fixable issues, run:

```bash
make lint-fix
```

---

## 4. Verification

1. Verify `make fmt vet lint` exits with code `0` and zero warnings.
2. Verify `make test` exits with code `0` and all packages (`internal/clientmap`, `internal/controller/slurmclientsync`, `internal/controller/slurmjob`, `internal/controller/workloadmixing`, `internal/metrics`, `internal/utils/...`) report `ok`.
3. Check `git status` to confirm no untracked test artifacts remain.

---

## 5. Handoffs & Related Skills

- **To run full-cluster Ginkgo E2E tests on Kind or GKE GPU clusters:** Read [skills/test-e2e-cluster/SKILL.md](../test-e2e-cluster/SKILL.md).
- **To modify controller, poller, or translator logic under test:** Read [skills/dev-controller-and-translator/SKILL.md](../dev-controller-and-translator/SKILL.md).
- **To regenerate CRDs or DeepCopy methods after API test changes:** Read [skills/dev-api-and-codegen/SKILL.md](../dev-api-and-codegen/SKILL.md).
