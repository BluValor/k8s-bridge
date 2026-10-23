---
name: test-e2e-cluster
description: >-
  Runs, writes, and debugs full-stack end-to-end (E2E) Ginkgo test suites and
  cluster test harnesses against a local Kind CPU cluster or a GKE GPU
  Kubernetes cluster. Use when executing make test-e2e, make test-e2e-gpu, or
  make run-test-e2e-gpu, adding new E2E test specs in test/e2e/, or debugging
  E2E test failures. Don't use for local unit/envtest tests (use
  test-unit-and-lint) or interactive operator or partition deployment (use
  infra-deploy-stack or infra-configure-crs).
---

# End-to-End (E2E) Cluster Testing Workflow (`test-e2e-cluster`)

## 1. Preconditions & Inputs

Before running or modifying E2E suites, verify:
- **Test mode:**
  - **Local Kind CPU E2E (`test/e2e/`):** Requires `docker`, `kubectl`, and `go` (`make` installs `kind` `v0.27.0` into `./bin/kind`, and `BeforeSuite` applies vendored manifests from `test/e2e/testdata/`).
  - **GKE GPU E2E (`test/e2e/gpu/`):** Requires `gcloud`, `kubectl`, `docker`, `go`, and a `.env` file or environment variables configuring `GCP_PROJECT`, `GCP_ZONE`, and `GPU_ACCELERATOR_TYPE`.
- Read [references/e2e-env-variables.md](references/e2e-env-variables.md) for all `.env` and Makefile variables, including the `GPU_ACCELERATOR_TYPE` discrepancy between `Makefile` and `gke-gpu-cluster-setup.sh`.

---

## 2. Guardrails

- **Confirmation gate for GKE GPU cluster creation:** `make test-e2e-gpu` and `make setup-infra-gpu` create billable GKE clusters and GPU node pools via `test/e2e/infra/gpu-cluster-setup.sh`. Obtain user confirmation before running them.
- **Set `GPU_ACCELERATOR_TYPE=nvidia-l4` in `.env` for GKE GPU tests:** `Makefile` defaults `GPU_ACCELERATOR_TYPE ?= nvidia-tesla-t4` (`n1-standard-4`), whereas `test/e2e/infra/gke-gpu-cluster-setup.sh`, `test/e2e/testdata/workloadmixing_gpu.yaml`, and `test/e2e/gpu/gpu_test.go` require `nvidia-l4` (`g2-standard-4` with `nvidia.com/gpu.product: NVIDIA-L4`). Set `GPU_ACCELERATOR_TYPE=nvidia-l4` in `.env` before running `make test-e2e-gpu`.
- **Do not delete dynamic `slurmd` pods with `--force --grace-period=0` in E2E cleanup:** Force-deleting `slurmd` pods skips `PreStop` node deregistration and allows containers to re-register stale dynamic nodes in `slurmctld`, causing subsequent specs to fail with `BadConstraints`. Use `--grace-period=5 --wait=true` (`test/utils/slurm.go` `ResetSlurmAndK8sState`).
- **Reload Lua `ConfigMap` objects when iterating on `hack/*.lua` in an existing Kind cluster:** In `BeforeSuite`, `SetupSlinkySlurm` (`test/utils/slurm.go`) creates `ConfigMap/slurm-job-submit-lua` and `ConfigMap/slurm-cli-filter-lua` from `hack/job_submit.lua` and `hack/cli_filter.lua`. `test/e2e/testdata/slurm_cluster.yaml` mounts these `ConfigMap` objects, while `test/e2e/testdata/slurm_login.yaml` defines `slurm-login` with test user `foo` (UID `2000`).

---

## 3. Step-by-Step Procedure

Select the step matching your target E2E task (do not run Steps 1, 2, and 3 together):

### Step 1: Run Full Local Kind CPU E2E Suite (`make test-e2e`)
*(Run this step when executing the complete local CPU E2E suite from scratch.)*
Run `make test-e2e` to create Kind cluster `k8s-bridge-test-e2e` (`kindest/node:v1.32.2`), build and load `example.com/k8s-bridge:v0.0.1`, and install `cert-manager`, `JobSet`, `Kueue`, Slinky Operator, `test/e2e/testdata/slurm_cluster.yaml`, and `slurm_login.yaml`. The target runs all specs in `./test/e2e/` and deletes the Kind cluster afterward:

```bash
make test-e2e
```

### Step 2: Run Focused Ginkgo Specs Against an Existing Kind Cluster
*(Run this step when iterating on a single CPU E2E spec without recreating the Kind cluster on each run.)*

```bash
# 1. Create the Kind cluster if it does not exist
make setup-test-e2e

# 2. Run a focused spec (BeforeSuite installs dependencies if not yet installed)
KIND=./bin/kind KIND_CLUSTER=k8s-bridge-test-e2e \
  go test -tags=e2e ./test/e2e/ -v -ginkgo.v -timeout 20m \
  -ginkgo.focus="should reject unsupported flags"

# 3. Tear down the Kind cluster when finished
make cleanup-test-e2e
```

### Step 3: Run GKE GPU E2E Suite (`make test-e2e-gpu` vs `make run-test-e2e-gpu`)
*(Run this step only when testing GPU scheduling on GKE after user confirmation.)*
1. Create or verify `.env` in the repository root (see [references/e2e-env-variables.md](references/e2e-env-variables.md)):

```bash
cat <<'EOF' > .env
GCP_PROJECT=<PROJECT_ID>
GCP_ZONE=us-central1-a
GPU_CLUSTER_NAME=k8s-bridge-gpu-e2e
GPU_ACCELERATOR_TYPE=nvidia-l4
GPU_ACCELERATOR_COUNT=1
GPU_NODE_COUNT=1
EOF
```

2. **Full lifecycle (provision GKE cluster + run tests + tear down cluster):** After user confirmation, run:

```bash
make test-e2e-gpu
```

3. **Existing cluster execution (skip cluster creation and teardown):** If the GKE GPU cluster is already running and configured in `kubectl config current-context`, run:

```bash
make run-test-e2e-gpu
```

### Step 4: Write New E2E Test Specs (`test/e2e/`)
*(Follow this step when adding or modifying test cases in `test/e2e/` or `test/e2e/gpu/`.)*
Use the E2E helpers in `test/utils/slurm.go` and `test/utils/jobset.go`:
1. **`utils.NewBatchJobBuilder(slurmNamespace)` (`test/utils/slurm.go`):** Builds and submits `sbatch` jobs from the login pod as `utils.DefaultSlurmTestUser` (`foo`, UID `2000`, provisioned on `slurmd` via a `postStart` hook). Configure flags with `.WithPartition(...)`, `.WithNodes(...)`, `.WithGres(...)`, `.WithWrap(...)`, or `.WithArgs(...)`.
2. **`utils.NewSlurmJobVerifier(slurmNamespace, jobID)` (`test/utils/slurm.go`) & `test/utils/jobset.go`:** `NewSlurmJobVerifier` polls `scontrol show job` state (`QuerySlurmJobState`), while `utils.JobSet`, `utils.VerifyJobSetNotFound`, `utils.VerifyJobSetCreatedWithGPU`, and `utils.ExpectJobSetNotRecreated` in `test/utils/jobset.go` assert on Kubernetes `JobSet` objects.
3. **`utils.ResetSlurmAndK8sState(slurmNamespace)` (`test/utils/slurm.go`):** Call in `BeforeEach` to cancel jobs, delete `JobSet` objects, delete worker pods with `--grace-period=5`, and remove stale dynamic Slurm nodes.

---

## 4. Verification

1. Verify the Ginkgo runner prints `SUCCESS! -- <N> Passed | 0 Failed` and exits with code `0`.
2. If `make test-e2e` or `make test-e2e-gpu` ran, verify the temporary test cluster was cleaned up (`./bin/kind get clusters` does not list `k8s-bridge-test-e2e`).
3. Run `make fmt vet lint` after editing any files under `test/`.

---

## 5. Handoffs & Related Skills

- **If an E2E spec fails with a stuck or cancelled Slurm job:** Read [skills/ops-diagnose-workload/SKILL.md](../ops-diagnose-workload/SKILL.md) to inspect `slurmctld`, `k8s-bridge`, and Kueue state.
- **To run fast local unit and `envtest` suites:** Read [skills/test-unit-and-lint/SKILL.md](../test-unit-and-lint/SKILL.md).
- **To install or upgrade the base operator stack in a cluster:** Read [skills/infra-deploy-stack/SKILL.md](../infra-deploy-stack/SKILL.md).
