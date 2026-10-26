---
name: dev-controller-and-translator
description: >-
  Implements or modifies Go operator reconciliation loops in
  internal/controller/, Slurm REST polling and event deduplication, JobSet
  translation, ClientMap and JWT token management, and Prometheus metric
  instruments. Use when changing how Slurm jobs are polled, translated into
  JobSets, unheld, preempted, or torn down in Go source code. Don't use for
  Slurm Lua submission plugins (use dev-slurm-lua), CRD schema or Helm
  template changes (use dev-api-and-codegen), deploying or upgrading the
  operator chart in a cluster (use infra-deploy-stack), or writing unit/E2E
  test suites (use test-unit-and-lint or test-e2e-cluster).
---

# Controller, Poller, and JobSet Translator Workflow (`dev-controller-and-translator`)

## 1. Preconditions & Inputs

Before editing Go controller or translator packages, verify:
- **Working directory:** Repository root (`k8s-bridge`).
- **Component package map:**
  - `cmd/main.go`: Manager entrypoint, CLI flags (`--poll-interval`, `--orphan-grace-period`, `--debug`, plus `zap` flags via `defaultZapOptions`), and runnable registration.
  - `internal/clientmap/clientmap.go`: Thread-safe map (`ClientMap`) storing `slurmclient.Client` instances and JWT expiration timestamps per Slinky `Controller` (`types.NamespacedName`).
  - `internal/controller/slurmclientsync/`: `SlurmClientReconciler` (`slurmclient_controller.go`) and HS256 JWT minting (`slurmjwt/token.go`).
  - `internal/controller/workloadmixing/`: `WorkloadMixingReconciler` (`workloadmixing_controller.go`).
  - `internal/controller/slurmjob/`:
    - `poller.go`: `SlurmPoller` (leader-elected runnable polling `slurmrestd`).
    - `slurmjob_controller.go`: `SlurmJobReconciler` (reconciles new and active jobs).
    - `translator.go`: `JobSetTranslator` (converts `V0044JobInfo` into `JobSet` with `FinalizerJobSet`).
    - `teardown_reconciler.go`: `JobTeardownReconciler` (finalizes and deletes dynamic Slurm nodes and `JobSet` objects).
    - `slurm_utils.go`: `JobSet` finalizer and node cleanup helpers (`finalizeJobSet`, `removeJobSetFinalizer`, `isJobSetFinalized`, `isSlurmControllerGone`), Slurm 404/422 error classification (`isSlurmNotFound`), `OwnerReference` helpers, and terminal state checks.
  - `internal/metrics/metrics.go`: Prometheus metric definitions and recording helpers.
- Read [references/reconciler-and-client-conventions.md](references/reconciler-and-client-conventions.md) for `V0044JobInfo` pointer/struct dereferencing rules, `slurmclient.Client` call signatures, and `JobSet` label and finalizer constants.

---

## 2. Guardrails

- **Do not dereference `V0044JobInfo` pointer or `NoValStruct` fields without nil and validity checks:**
  - Scalar fields (`JobId`, `Hold`, `Name`, `Partition`, `Comment`, `Features`, `StateReason`) are pointers; use `ptr.Deref(field, defaultVal)`.
  - Numeric `NoValStruct` fields (`NodeCount`, `Tasks`, `TasksPerNode`, `CpusPerTask`, `Priority`, `MemoryPerNode`, `MemoryPerCpu`) require checking `field != nil && ptr.Deref(field.Set, false) && !ptr.Deref(field.Infinite, false)` before reading `*field.Number`.
- **Preserve workqueue key deduplication (`slurm-job-<wmName>-<jobID>`):** Both `SlurmPoller` admission events (`GenericEvent`) and `JobSet` informer events (`mapJobSetToRequest`) must map to `reconcile.Request{NamespacedName: {Namespace: wmNamespace, Name: "slurm-job-<wmName>-<jobID>"}}`.
- **Keep channel sends non-blocking in `SlurmPoller`:** Do not block on `admissionEventsChan` or `teardownEventsChan` (capacity `1024`). On a full channel, increment `k8s_bridge_dropped_events_total` and let the next poll tick retry.
- **Finalize `JobSet` (`finalizeJobSet`) before calling `r.Delete(ctx, js)` during teardown:** Every translated `JobSet` carries finalizer `k8s-bridge.x-k8s.io/slurm-node-cleanup` (`FinalizerJobSet`). In `JobTeardownReconciler.handleTeardown` and `SlurmJobReconciler.handleTerminalJobSet`, call `finalizeJobSet` (which deletes each `<jobset-name>-slurmd-<jobIndex>-0` node in Slurm and patches off `FinalizerJobSet` using `client.MergeFromWithOptimisticLock{}`) **before** calling `r.Delete(ctx, js)` so `Delete` does not bump `resourceVersion` ahead of the finalizer patch.

---

## 3. Step-by-Step Procedure

> **Component Selection:** Execute only the step(s) below that match the controller, translator, or metrics package you are modifying.

### Step 1: Modify `SlurmClientReconciler` or `WorkloadMixingReconciler`
1. **Slurm REST Client & JWT Sync (`internal/controller/slurmclientsync/`):**
   - `SlurmClientReconciler` watches Slinky `Controller`, `RestApi`, and `corev1.Secret` (`MaxConcurrentReconciles: 1`).
   - It mints an HS256 JWT for user `"slurm"` (issuer `"slurm-operator"`) with a 15-minute lifetime and requeues after 12 minutes (`StalenessBuffer = 3 * time.Minute`).
   - Disable client caching for `V0041ControllerPing`, `V0044JobInfo`, and `V0044Node`.
   - Callers (`SlurmPoller`, `SlurmJobReconciler`, `JobTeardownReconciler`) read clients via `ClientMap.Get` without synchronous token refreshes.
2. **`WorkloadMixingReconciler` (`internal/controller/workloadmixing/workloadmixing_controller.go`):**
   - Verify both the referenced `Controller` (`spec.controllerRef.name`) and its `RestApi` exist in the `WorkloadMixing` namespace.
   - Patch `status.conditions` (`Available=True` with reason `Reconciled`, or `Available=False` with reason `ControllerNotFound` / `RestApiNotFound`) using `r.Status().Patch(ctx, wm, client.MergeFrom(original))`.

### Step 2: Modify `SlurmPoller`, `SlurmJobReconciler`, or `JobTeardownReconciler`
1. **`SlurmPoller` (`internal/controller/slurmjob/poller.go`):**
   - Group `WorkloadMixing` CRs by `ControllerRef` so `cli.List` (`V0044JobInfoList`, 5s timeout) runs once per Slurm controller per tick.
   - Emit `SlurmJobAdmissionEvent` for `PENDING` jobs on mapped partitions.
   - Audit existing `JobSet` objects (skipping any `JobSet` where `isJobSetFinalized(js)` is `true`): emit `SlurmJobTeardownEvent` if the Slurm job is in a terminal state or missing from `slurmrestd` longer than `--orphan-grace-period` (`1m`, tracked in `firstSeenMissing`).
2. **`SlurmJobReconciler` (`internal/controller/slurmjob/slurmjob_controller.go`):**
   - For new jobs (`reconcileNewJob`): skip if `isJobExplicitlyHeld(job)` is `true` (`JobHeldAdmin`, or `JobHeldUser` without `"k8s-bridge-auto-hold"`). Otherwise call `Translator.Translate`, set the `WorkloadMixing` `OwnerReference`, create the `JobSet` (treating `apierrors.IsAlreadyExists` as idempotent), and call `releaseHold` (`Hold=false`, `Constraints` with `k8s-id-<jobID>`, updated `Comment`).
   - If `Translator.Translate` returns an error, emit Warning event `JobTranslationFailed` and cancel the Slurm job with comment `"Translation failed: <err>"`.
   - For active jobs (`reconcileActiveJob` via `activeJobStatus`): if `StatusShouldUnhold` (`jobInfo.Hold` is true, not explicitly held, and `existingJS.Spec.Suspend` is false), call `releaseHold`. If `StatusJobSetMissing`, cancel the Slurm job with comment `"JobSet missing"`. If `StatusJobSetDeleting` (`isJobSetDeleting(existingJS)`), cancel the non-terminal Slurm job (`"JobSet missing"`) and call `finalizeJobSet`. If `StatusJobSetCompleted` or `StatusJobSetFailed`, cancel the non-terminal Slurm job (`"JobSet completed"` / `"JobSet failed"`), call `finalizeJobSet`, and delete the `JobSet`. If `WorkloadMixing`, the Slinky `Controller`, or the Slurm job is already gone while `existingJS` is deleting (`FinalizeDeletingJobSet`), release the finalizer (`releaseFinalizerForDeletingJobSet`) so `JobSet` deletion never deadlocks.

### Step 3: Modify `JobSetTranslator` (`internal/controller/slurmjob/translator.go`)
When changing how a Slurm job translates into a `JobSet`:
1. **Partition & Replicas (`resolveReplicas`):** Require a single mapped partition. Resolve `replicas` from `node=<N>` in `job.TresReqStr` first, falling back to `job.NodeCount.Number` (`tasksPerNode = job.TasksPerNode.Number` or `ceil(Tasks / replicas)`).
2. **Resource Calculation (`resolvePodResources`):**
   - Resolve GPUs from `TresPerNode`, `TresPerTask * tasksPerNode`, or `TresPerJob / replicas` (must divide with remainder 0; reject mixed GPU types). Match `(ResourceName: "gpu", ResourceType: <type>)` in `wmSpec.MappedResources` (`GRESGPU` is currently hardcoded).
   - Calculate CPU (`CpusPerTask * tasksPerNode` [note: `slurmrestd` v0.0.44 sets `CpusPerTask=1` when `--ntasks` is set], else `gpuRequest * CpusPerTres`, else `1 * tasksPerNode`) and Memory (`max(MemoryPerNode, MemoryPerCpu * cpu, MemoryPerTres * gpuRequest, 2048 * cpu)` MiB floor).
   - Overwrite `slurmd` container `Requests` (CPU, Memory, GPU) and GPU `Limits` while preserving `podTemplate` CPU/memory `Limits` and non-Slurm resources.
3. **Container & `JobSet` Wiring (`buildJobSet`):**
   - Mount `controller.Spec.SlurmKeyRef` at `/etc/slurm/slurm.key` (`0600`).
   - Inject `JOBSET_NAME`, `REPLICATED_JOB_NAME`, `JOB_INDEX`, `POD_INDEX`, and `SLURM_NODE_NAME` env vars.
   - Append `slurmd` args: `--conf-server <fqdn>:6817 -N $(SLURM_NODE_NAME) -Z --conf 'Features=k8s-id-<jobID>,<dynamicNodeFeature> Gres=...'`.
   - Inject default `PreStop` hook, `startupProbe`, and `livenessProbe` unless already defined in `podTemplate`.
   - Set `JobSet` name `slurm-job-<jobID>`, `Finalizers: []string{"k8s-bridge.x-k8s.io/slurm-node-cleanup"}` (`FinalizerJobSet`), `spec.suspend = true`, `FailurePolicy.MaxRestarts` (`3` default, or `0` when `job.Requeue` is `false` via `--no-requeue`), `CompletionMode: Indexed`, `Parallelism: 1`, `Completions: 1`, and `Replicas: <replicas>`.

### Step 4: Add or Update Prometheus Metrics (`internal/metrics/metrics.go`)
Register new metrics with controller-runtime's `metrics.Registry` in `internal/metrics/metrics.go` and expose recording helpers called by the reconcilers or poller.

---

## 4. Verification

1. **Automated verification (default for agents):** Run code generation, formatting, static analysis, and the unit/`envtest` suite (if `config/rbac/role.yaml` changes, sync `helm/templates/clusterrole.yaml`):

```bash
make manifests generate fmt vet lint test
```

2. **Host-to-cluster debug mode (only when requested for live cluster debugging):** Port-forward `svc/slurm-restapi` on `6820` and run the manager with `--debug`:

```bash
kubectl -n slurm port-forward svc/slurm-restapi 6820:6820 &
go run ./cmd/main.go --debug --metrics-bind-address=0
```

---

## 5. Handoffs & Related Skills

- **To write unit tests or `slurmclient` mocks for your changes:** Read [skills/test-unit-and-lint/SKILL.md](../test-unit-and-lint/SKILL.md).
- **To deploy or upgrade the operator chart in a cluster:** Read [skills/infra-deploy-stack/SKILL.md](../infra-deploy-stack/SKILL.md).
- **To run the full-stack Kind or GKE GPU E2E suite:** Read [skills/test-e2e-cluster/SKILL.md](../test-e2e-cluster/SKILL.md).
- **If you modified CRD types or `+kubebuilder:rbac` markers:** Read [skills/dev-api-and-codegen/SKILL.md](../dev-api-and-codegen/SKILL.md).
