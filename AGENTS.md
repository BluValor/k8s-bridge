# k8s-bridge — Agent Instructions

## 1. Project Overview, Module Map & Runtime Lifecycle

- **What this repository does:** `k8s-bridge` is a Kubernetes operator and Slurm plugin suite that shares compute resources between Slurm (deployed via the Slinky Slurm operator) and Kubernetes. It intercepts Slurm jobs submitted to bridged partitions and runs dynamic `slurmd` worker pods managed by Kubernetes `JobSet` and `Kueue`.
- **Module Map:**
  - **Slurm Lua Plugins (`hack/cli_filter.lua`, `hack/job_submit.lua`):** Client-side CLI flag validation against `ALLOWED_OPTIONS` and server-side job submission/modification interception on `WORKLOAD_MIXING_PARTITIONS`.
  - **`WorkloadMixing` & `SlurmClient`/`ClientMap` Sync Modules (`internal/controller/workloadmixing/`, `internal/controller/slurmclientsync/`, `internal/clientmap/`):** Validates Slinky `Controller` and `RestApi` references on `WorkloadMixing` CRs (`status.conditions`), mints and refreshes 15-minute HS256 JWT tokens (`slurmjwt`), and caches authenticated `slurmrestd` clients in `ClientMap`.
  - **Job Polling, Translation, Reconciliation & Teardown (`internal/controller/slurmjob/`):** `SlurmPoller` (`poller.go`), `SlurmJobReconciler` (`slurmjob_controller.go`), `JobSetTranslator` (`translator.go`), and `JobTeardownReconciler` (`teardown_reconciler.go`).
  - **Prometheus Metrics (`internal/metrics/metrics.go`):** Controller-runtime counters, gauges, and histograms (`k8s_bridge_*`) tracking Slurm REST polling, translation latency, active jobs/partitions, and dropped events.
- **Runtime Workload Lifecycle:**
  1. **Submission Interception:** Both Lua plugins run on every submission and enforce when a `WORKLOAD_MIXING_PARTITIONS` partition is targeted (static-only partition jobs are untouched and never translated): `hack/cli_filter.lua` (on login pods) validates CLI flags against `ALLOWED_OPTIONS`, and `hack/job_submit.lua` (in `slurmctld`) places bridged jobs on hold (`priority=0`, comment `"k8s-bridge-auto-hold"`) while rejecting multi-partition requests that include a bridged partition.
  2. **Polling & Deduplication:** `SlurmPoller` (`internal/controller/slurmjob/poller.go`) polls `slurmrestd` every `--poll-interval` (`10s`) using JWT-authenticated clients from `ClientMap` (`internal/clientmap/`) and emits admission or teardown events deduplicated by workqueue key `slurm-job-<wmName>-<jobID>`.
  3. **Translation & Quota Admission:** `SlurmJobReconciler` (`internal/controller/slurmjob/slurmjob_controller.go`) and `JobSetTranslator` (`translator.go`) translate held jobs into suspended `JobSet` objects (`slurm-job-<jobID>`) labeled with `kueue.x-k8s.io/queue-name` and optional `kueue.x-k8s.io/priority-class`.
  4. **Dynamic Node Registration & Release:** `SlurmJobReconciler.releaseHold` updates the Slurm job via REST (`Hold=false`, `Constraints="...&k8s-id-<jobID>"`). When Kueue admits the `JobSet`, worker pods start `slurmd -Z` with `Features=k8s-id-<jobID>,<dynamicNodeFeature>` and register with `slurmctld`.
  5. **Preemption & Teardown:** Kueue preemption suspends the `JobSet` and evicts pods, causing Slurm to requeue the job if `Requeue=1` (or terminate `NODE_FAIL` if `--no-requeue`). On job completion or failure, `JobTeardownReconciler` (`teardown_reconciler.go`) or `SlurmJobReconciler` deletes the dynamic Slurm nodes, removes the `k8s-bridge.x-k8s.io/slurm-node-cleanup` finalizer (`finalizeJobSet`), and deletes the `JobSet`.

---

## 2. Start Here — Persona Triage, Lifecycle Order & Environment Matrix

Determine which workflow track applies before running commands or editing files:

### 2.1 Two-Track Procedural Lifecycle
- **Track A — Software Contributor Lifecycle (Code, Plugins, Packaging, Tests & PRs):**
  `dev-api-and-codegen` (schemas/RBAC/packaging) -> `dev-controller-and-translator` / `dev-slurm-lua` -> `test-unit-and-lint` -> `test-e2e-cluster` -> `dev-api-and-codegen` (pre-PR parity audit)
- **Track B — Cluster Operator Lifecycle (Infra, Deployment, Config & Day-2 Ops):**
  `infra-provision-cluster` (if cloud) -> `infra-deploy-stack` -> `infra-configure-crs` -> `test-e2e-cluster` (optional validation) -> `ops-diagnose-workload` (read-only triage) -> `ops-cleanup-state` (remediation)

### 2.2 Pre-Flight Context Check (For Cluster / Cloud Operations)
Before mutating any cluster or cloud environment, use read-only commands (`kubectl config current-context`) or ask the user to verify:
1. **Target Environment** (see matrix below)
2. **Kubernetes Context & Namespace** (`kubectl config current-context`, default Slurm namespace `slurm`, operator namespace `k8s-bridge-system`)
3. **Container Image Registry & Tag** (`IMG=<registry>/k8s-bridge:<tag>`)

| Environment | Acceleration & Workload Profile | Node Provisioning Skill | Environment-Specific Configuration & Notes |
|---|---|---|---|
| **Local Kind** | CPU only / inner-loop dev & E2E | *(Managed by Kind harness)* | Default CPU partition (`k8s-partition`), Kind E2E harness (`test-e2e-cluster`), no cloud quotas |
| **Cloud CPU** | Autoscaled CPU node pools (GKE `1.31+`) | `infra-provision-cluster` (CCC) | GKE Node Auto-Provisioning + `ComputeClass` (`cost-optimized`), CPU `ResourceFlavor` (`infra-configure-crs`) |
| **Cloud GPU** | Hardware GPUs (GKE `1.35.2-gke.1842000+`) | `infra-provision-cluster` (DWS) | DWS Flex-start (`--flex-start`), typed/untyped GPU `mappedResources` (`infra-configure-crs`) |

*(Base stack deployment via `infra-deploy-stack` and verification via `test-e2e-cluster` apply to all environments.)*

---

## 3. Global Guardrails & Safety Gates

### 3.1 Code & Repository Guardrails
- **Generated Files (Do Not Edit):**
  - `api/v1alpha1/zz_generated.deepcopy.go` -> regenerate via `make generate`
  - `config/crd/bases/k8s-bridge.x-k8s.io_workloadmixings.yaml`, `helm/crds/k8s-bridge.x-k8s.io_workloadmixings.yaml`, and `config/rbac/role.yaml` -> regenerate via `make manifests`
  - `PROJECT` -> managed by `kubebuilder` CLI
- **Required Post-Edit Checks:**
  - After API/CRD/RBAC marker or chart edits: run `make manifests generate` and sync `helm/templates/clusterrole.yaml` if RBAC changed (`dev-api-and-codegen`).
  - After Go or Lua code changes: run `make fmt vet lint test` (`test-unit-and-lint`).
  - Before submitting a pull request or after manifest/doc edits: run `make version-sync manifests generate fmt`, verify `git diff --exit-code`, and audit parity (`dev-api-and-codegen`).

### 3.2 Cluster & Cloud Operational Guardrails
- **Confirmation Gate (Billable & Destructive Actions):** Present the planned commands and obtain user confirmation before:
  1. Creating, resizing, or deleting cloud clusters or node pools (`infra-provision-cluster`).
  2. Installing, upgrading, or uninstalling cluster-wide CRDs, webhooks, or Helm releases (`infra-deploy-stack`, `infra-configure-crs`).
  3. Cancelling active Slurm jobs, draining nodes, deleting Kubernetes resources (`ops-cleanup-state`), or mutating admin/tenant policies (`ClusterQueue`, `ResourceQuota`, `LimitRange`, Slurm partition limits/states) and other tenants' workloads.
- **Pod Termination Safety:** Do not delete dynamic `slurmd` pods with `--force --grace-period=0`. Force-deletion bypasses container `PreStop` hooks (`scontrol update ... state=DOWN && scontrol delete`) and leaves stale nodes in `slurmctld` that fail subsequent jobs with `BadConstraints`.
- **Non-Root Verification:** `kubectl exec` into `deploy/slurm-login-slinky` runs as `root` (UID 0), and `job_submit.lua` bypasses `slurm_job_modify` checks for `root` and `slurm`. Test Slurm submissions and modifications as a non-root user (`runuser -u <user> -- ...`).

---

## 4. Repository Layout

```text
.
├── api/v1alpha1/        # WorkloadMixing CRD Go types (canonical schema source)
├── cmd/                 # Manager binary entrypoint (cmd/main.go) and CLI flags
├── config/              # Kustomize bases (crd, rbac, manager, prometheus, network-policy, samples)
├── docs/                # User guides and scenario example manifests (docs/examples/)
├── hack/                # Slurm Lua plugins (job_submit.lua, cli_filter.lua) and helper scripts
├── helm/                # Local Helm chart (Chart.yaml, values.yaml, crds/, templates/)
├── internal/            # Go packages (clientmap, controller/{slurmclientsync,slurmjob,workloadmixing}, metrics, utils, version)
├── skills/              # On-demand Agent Skills (skills/<category>-<name>/SKILL.md)
└── test/                # Ginkgo E2E suites (test/e2e/, test/e2e/gpu/), infra scripts, and mocks (test/utils/)
```

---

## 5. Quick Command Reference

| Action | Command |
|---|---|
| Regenerate CRDs, RBAC & sync `helm/crds/` | `make manifests` |
| Regenerate DeepCopy methods | `make generate` |
| Format, vet, and run linters | `make fmt vet lint` |
| Run unit & `envtest` controller tests | `make test` |
| Build manager binary / container image | `make build` / `make docker-build IMG=<image>` |

*(To execute E2E tests, update Helm charts, or deploy clusters, load the corresponding skill below.)*

---

## 6. Skill Routing Table

**The 1-Sentence Trigger + Pointer Rule:** When a task matches a trigger below, read the target `SKILL.md` file before taking action (all `skills/...`, `hack/...`, and `docs/...` paths are relative to the repository root containing `Makefile` and `PROJECT`). Do not guess procedures from memory.

**Subagent Delegation Protocol:** Include in every subagent prompt: (1) Target Environment and Kubernetes context/namespace, (2) the `skills/<skill-name>/SKILL.md` path to read first before running commands or editing files, and (3) whether access is read-only or allows state mutation after user confirmation.

### Code Development (`dev-*`)
| Task / Trigger | Canonical Skill |
|---|---|
| Modifying `WorkloadMixing` Go types in `api/`, Kubebuilder/RBAC markers, Kustomize bases, Helm chart templates/values, or auditing pre-PR parity across `config/`, `helm/`, and `docs/examples/` | [skills/dev-api-and-codegen/SKILL.md](skills/dev-api-and-codegen/SKILL.md) |
| Modifying Go reconcilers, `SlurmPoller`, `JobSetTranslator`, `ClientMap`, JWT sync, or Prometheus metrics in `internal/` | [skills/dev-controller-and-translator/SKILL.md](skills/dev-controller-and-translator/SKILL.md) |
| Modifying or testing the Slurm Lua plugins (`hack/job_submit.lua` and `hack/cli_filter.lua`) | [skills/dev-slurm-lua/SKILL.md](skills/dev-slurm-lua/SKILL.md) |

### Testing & Verification (`test-*`)
| Task / Trigger | Canonical Skill |
|---|---|
| Writing or running Go unit tests, `envtest` controller specs, Slurm REST client mocks, or linters (`make fmt vet lint test`) | [skills/test-unit-and-lint/SKILL.md](skills/test-unit-and-lint/SKILL.md) |
| Running, writing, or debugging Ginkgo E2E test suites on local Kind CPU clusters or GKE GPU clusters | [skills/test-e2e-cluster/SKILL.md](skills/test-e2e-cluster/SKILL.md) |

### Infrastructure & Cluster Configuration (`infra-*`)
| Task / Trigger | Canonical Skill |
|---|---|
| Creating, updating, or deleting GKE clusters and autoscaling CPU (CCC) or GPU (DWS Flex-start) node pools, or labeling GPU nodes | [skills/infra-provision-cluster/SKILL.md](skills/infra-provision-cluster/SKILL.md) |
| Installing, upgrading, or tearing down the base operators and Helm charts (`cert-manager`, `slurm-operator`, `JobSet`, `Kueue`, `k8s-bridge`) without CR configuration | [skills/infra-deploy-stack/SKILL.md](skills/infra-deploy-stack/SKILL.md) |
| Configuring, applying, or removing Kueue CRs, GKE `ComputeClass` CRs, Slinky `slurm` partitions/Lua plugins, `WorkloadMixing` CRs (CPU/GPU/priority), and non-root users | [skills/infra-configure-crs/SKILL.md](skills/infra-configure-crs/SKILL.md) |

### Day-2 Operations & Troubleshooting (`ops-*`)
| Task / Trigger | Canonical Skill |
|---|---|
| Read-only diagnosis of rejected, held, suspended, pending, preempted, or cancelled jobs and diagnostic snapshot collection | [skills/ops-diagnose-workload/SKILL.md](skills/ops-diagnose-workload/SKILL.md) |
| Draining and cleaning up wedged Slurm jobs, orphaned `JobSet` objects, dynamic `slurmd` pods, and stale Slurm nodes | [skills/ops-cleanup-state/SKILL.md](skills/ops-cleanup-state/SKILL.md) |

---

## 7. Core Architectural & Ownership Invariants

- **State Ownership (Single Source of Truth):**
  - **Slurm (`slurmctld` / `slurmrestd`):** Owns job submission, user/admin hold states, job state transitions, and task execution on dynamic `slurmd` nodes.
  - **Kubernetes & Kueue:** Own cluster resource quotas, `Workload` admission gating, priority preemption decisions, and `JobSet` pod lifecycle.
  - **`k8s-bridge`:** Stateless translator and synchronizer maintaining a 1:1 mapping between Slurm jobs on bridged partitions and Kubernetes `JobSet` (`slurm-job-<jobID>`) objects in the same namespace.
- **Configuration Parity Invariant:** Match every bridged partition name across three places: `slurmctld` `WORKLOAD_MIXING_PARTITIONS` env var, `slurm.conf` `CliFilterParameters=workload_mixing_partitions=...`, and `WorkloadMixing.spec.partitionsMapping[].partition`. Match `dynamicNodeFeature` with the `NodeSet` `Feature` in `slurm.conf`.
- **Schema Ownership Invariant:** Go types in `api/v1alpha1/workloadmixing_types.go` are the single source of truth for CRD schemas; `config/crd/bases/` and `helm/crds/` are generated artifacts updated via `make manifests`.
