# E2E Environment Variables & Test Suite Reference

## 1. Local Kind CPU E2E Variables (`Makefile` & `test/e2e/`)

| Variable | Default in `Makefile` / Code | Description |
|---|---|---|
| `KIND` | `./bin/kind` | Path to the `kind` binary (`v0.27.0`). |
| `KIND_CLUSTER` | `k8s-bridge-test-e2e` | Name of the Kind cluster created by `make setup-test-e2e`. |
| `KIND_NODE_IMAGE` | `kindest/node:v1.32.2` | Kind node container image. |
| `CERT_MANAGER_INSTALL_SKIP` | *(unset)* | Set to `true` to skip installing `cert-manager` in `BeforeSuite`. |
| `JOBSET_INSTALL_SKIP` | *(unset)* | Set to `true` to skip installing `JobSet` in `BeforeSuite`. |
| `KUEUE_INSTALL_SKIP` | *(unset)* | Set to `true` to skip installing `Kueue` in `BeforeSuite`. |
| `KUBECTL_KUBERC` | *(unset)* | Set to `true` to allow `kubectl` `kuberc` configuration during E2E runs. |

---

## 2. Cloud GPU E2E Variables (`.env`, `Makefile`, `test/e2e/infra/`)

`Makefile` includes `-include .env` at the top. Configure these variables in `.env` or on the command line:

| Variable | `Makefile` Default | Script Default (`gke-gpu-cluster-setup.sh`) | Notes / Required Value |
|---|---|---|---|
| `INFRA_PROVIDER` | `gke` | `gke` | Cloud provider selector (`test/e2e/infra/gpu-cluster-setup.sh`). |
| `GCP_PROJECT` | *(none)* | `gcloud config get-value project` | GCP project ID used by `test/e2e/infra/gke-gpu-cluster-setup.sh` and `gke-gpu-cluster-cleanup.sh`. |
| `GCP_ZONE` | `us-central1-a` | `us-central1-a` | GCP compute zone for the cluster and GPU nodes. |
| `GPU_CLUSTER_NAME` | `k8s-bridge-gpu-e2e` | `k8s-bridge-gpu-e2e` | GKE cluster name. |
| `GPU_ACCELERATOR_TYPE` | `nvidia-tesla-t4` | `nvidia-l4` | **Discrepancy:** Set `GPU_ACCELERATOR_TYPE=nvidia-l4` in `.env` to prevent `Makefile` from overriding the script with `nvidia-tesla-t4`. |
| `GPU_ACCELERATOR_COUNT` | `1` | `1` | GPUs attached per node. |
| `GPU_NODE_COUNT` | `1` | `1` | Number of GPU nodes provisioned in the node pool. |
| `GPU_TEST_TIMEOUT` | `45m` | N/A | Timeout passed to `go test -tags=e2e_gpu ./test/e2e/gpu/`. |

---

## 3. E2E Test Files Overview

| File | Build Tag | Coverage |
|---|---|---|
| `test/e2e/e2e_suite_test.go` | `e2e` | Builds `example.com/k8s-bridge:v0.0.1`, loads into Kind, installs `cert-manager`, `JobSet`, `Kueue`, Slinky Operator, and `test/e2e/testdata/slurm_cluster.yaml`. |
| `test/e2e/e2e_test.go` | `e2e` | Core job submission, auto-hold, `JobSet` translation, dynamic `slurmd` execution, explicit hold/release, and teardown. |
| `test/e2e/partition_priority_mapping_test.go` | `e2e` | Multi-partition rejection, priority mapping, and Kueue preemption (`JobRequeue=1`). |
| `test/e2e/slurm_flag_rejection_test.go` | `e2e` | Tests `hack/cli_filter.lua` allowed options (`--test-only`) and rejected flags across `sbatch`, `srun`, and `salloc`. |
| `test/e2e/jobset_restart_policy_test.go` | `e2e` | Tests `JobSet` `failurePolicy.maxRestarts=0` when `sbatch --no-requeue` is used vs `maxRestarts=3` default. |
| `test/e2e/metrics_helper_test.go` | `e2e` | Scrapes authenticated HTTPS metrics (`/metrics`) using a ServiceAccount token and `curl` pod. |
| `test/e2e/gpu/gpu_test.go` | `e2e_gpu` | Tests typed (`--gres=gpu:nvidia_l4:1`), untyped (`--gpus=1`), per-task, and per-job GPU translation on GKE. |
