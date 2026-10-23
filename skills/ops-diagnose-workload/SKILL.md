---
name: ops-diagnose-workload
description: >-
  Diagnoses rejected, held (JobHeldUser / JobHeldAdmin), suspended (Workload not
  admitted), pending (BadConstraints / Pod Pending), preempted, or cancelled
  Slurm workloads and captures diagnostic state snapshots across Slurm,
  k8s-bridge, Kueue, JobSet, and dynamic slurmd pods. Use when troubleshooting
  why a submitted Slurm job is stuck or failed, or when collecting a cluster
  diagnostic dump. Don't use for destructive state cleanup, pod deletion, or
  draining orphaned nodes (use ops-cleanup-state).
---

# Read-Only Workload Diagnosis & Snapshot Collection (`ops-diagnose-workload`)

## 1. Preconditions & Inputs

Before starting diagnosis, verify or ask the user for:
1. **Slurm `JOB_ID`:** The numeric Slurm job ID (if the job was accepted by `slurmctld`), or the rejected `srun`/`sbatch`/`salloc` command line.
2. **Namespaces:**
   - Slurm and `WorkloadMixing` namespace (`NAMESPACE`, default `slurm`).
   - Operator namespace (`BRIDGE_NAMESPACE`, default `k8s-bridge-system`).
   - Login deployment (`LOGIN_DEPLOY`, default `deploy/slurm-login-slinky`, or `deploy/slurm-login` in Kind E2E clusters).
3. Read [references/symptom-decision-table.md](references/symptom-decision-table.md) for the symptom-to-root-cause lookup table and error strings.

---

## 2. Guardrails

- **Read-only mode:** Do **not** run `scancel`, `scontrol delete`, `kubectl delete`, `kubectl patch`, or `helm upgrade/uninstall` during this workflow. If remediation requires cancelling jobs, deleting `JobSet` objects, or removing stale dynamic nodes, report the root cause first and hand off to `ops-cleanup-state` or `infra-configure-crs`.
- **Do not patch `JobSet.spec.suspend`:** Kueue's mutating webhook resets `JobSet.spec.suspend` edits unless the corresponding Kueue `Workload` (`spec.active`) is modified.

---

## 3. Step-by-Step Procedure

### Step 1: Collect a One-Shot Diagnostic Snapshot
Run the non-destructive snapshot script [scripts/collect-diagnostic-snapshot.sh](scripts/collect-diagnostic-snapshot.sh) (which appends `2>&1 || true` to each collection command so missing resources do not abort the dump):

```bash
# If a Slurm JOB_ID exists:
bash skills/ops-diagnose-workload/scripts/collect-diagnostic-snapshot.sh <JOB_ID> slurm k8s-bridge-system

# If diagnosing a pre-submission CLI flag rejection (no JOB_ID):
bash skills/ops-diagnose-workload/scripts/collect-diagnostic-snapshot.sh "" slurm k8s-bridge-system
```

### Step 2: Classify the Rejection or Slurm Job State
If the job was rejected at submission (no `JOB_ID`), match the CLI error in Branch 1. Otherwise, inspect `JobState`, `Reason`, `Priority`, `Features`, and `Comment` from `scontrol show job <JOB_ID>` (in Section 4 of the snapshot output) and follow the matching branch (see [references/symptom-decision-table.md](references/symptom-decision-table.md)):

1. **Rejected at submission (no `JOB_ID` assigned):**
   - `Validation error: job uses an option that is not supported by k8s-bridge (--<opt>)` or `(heterogeneous job pack_offset=<d>)`: CLI flag is not in `ALLOWED_OPTIONS` inside `hack/cli_filter.lua` (e.g. `--exclusive`, `--nodelist`, `--constraint`, `--requeue`, `--array`, `--het-group`), or the submission is a heterogeneous job (`pack_offset > 0`).
   - Client `Batch job submission failed: Unspecified error` (`slurmctld` log in snapshot §6: `Multi-partition request '<parts>' contains workload mixing partition.`): Rejected by `hack/job_submit.lua` (`--partition` combined a bridged partition with another partition).
2. **`JobState=PENDING` with `Priority > 0` (Not Held), or `JobState=CANCELLED` with `Comment="JobSet missing"` (`Reason!=JobHeldUser`; bridge logs `"JobSet missing for active Slurm job, cancelling job"`):**
   - Root cause: The target partition is missing from `WORKLOAD_MIXING_PARTITIONS` on `slurmctld` (`slurm-controller-0`), so `job_submit.lua` never held the job. The job stays `PENDING` if unmapped in `WorkloadMixing`, or is cancelled within ~10s (`Comment="JobSet missing"`) if mapped in `WorkloadMixing` (also caused by `root` `scontrol release` on an untranslated job; see table).
3. **`JobState=PENDING` with `Reason=JobHeldUser` and `Comment` lacks `k8s-bridge-auto-hold` (or `Reason=JobHeldAdmin`):**
   - Root cause: The job was submitted with `--hold` or held by an administrator (`isJobExplicitlyHeld == true`). `k8s-bridge` defers translation until the user releases the hold (`scontrol release <JOB_ID>`; exits 1 if 0 dynamic nodes exist, but sets `k8s-bridge-auto-hold` — see table row for `JobHeldAdmin`).
4. **`JobState=PENDING` with `Reason=JobHeldUser` and `Comment` contains `k8s-bridge-auto-hold`:**
   - Root cause: `k8s-bridge` has not translated the job yet. Check:
     - `kubectl -n slurm get workloadmixing -o yaml` (`Available=True`? Is the partition listed in `spec.partitionsMapping`?)
     - `kubectl -n k8s-bridge-system logs deploy/k8s-bridge-controller-manager -c manager --tail=100` (check for `slurmrestd` connection or JWT errors).
5. **`JobState=CANCELLED` with `Comment="Translation failed: ..."` (or `Multi-partition jobs with multiple mapped partitions are not supported` / `JobSet failed`):**
   - Root cause: `JobSetTranslator` or `SlurmJobReconciler` rejected the job (e.g., `missing GRES mapping for gpu:<type>`, `multiple heterogeneous gpu requests are not supported`, or `TresPerJob gpu count (<n>) is not cleanly divisible by NodeCount (<m>)`), or `JobSet` pods exited non-zero (`NotIn [0]`). Run:

```bash
kubectl -n slurm get events --field-selector reason=JobTranslationFailed
```

6. **`JobState=PENDING` (often `Reason=PartitionConfig` or `BadConstraints` before dynamic nodes register), `Comment` has `Unheld by k8s-bridge`, and `JobSet` `slurm-job-<JOB_ID>` has `SUSPENDED=true`:**
   - Root cause: Kueue has not admitted the `Workload` (quota/cohort exhaustion, `LocalQueue <name> doesn't exist` in `slurm`, or `ClusterQueue` `stopPolicy`/`namespaceSelector`), or no `Workload` exists (missing `WorkloadPriorityClass` or `JobSet` installed after Kueue without restart). Inspect the `Workload` (and `kubectl get workloads,localqueue -A`, or `kueue-controller-manager` logs if no `Workload` exists):

```bash
kubectl -n slurm describe workload -l "kueue.x-k8s.io/job-uid=$(kubectl -n slurm get jobset slurm-job-<JOB_ID> -o jsonpath='{.metadata.uid}' 2>/dev/null)"
```

7. **`JobState=PENDING`, `JobSet` has `SUSPENDED=false`, and `slurmd` pods are `Pending`, `ImagePullBackOff`, or `CrashLoopBackOff`:**
   - Root cause: Insufficient node resources/tolerations, unmatchable `ResourceFlavor.spec.nodeLabels` (e.g. `cloud.google.com/compute-class: cost-optimized` on non-NAP clusters), `placeholder-image` (when `podTemplate` was omitted), or missing `slurm.key` Secret. **If pods fail scheduling with `Insufficient cpu/memory/gpu`, check whether one pod exceeds any single node's `allocatable` capacity (Kueue admits on aggregate quota) and reshape the job or report; never create/resize node pools or enable NAP without user confirmation.** Inspect pod events and logs:

```bash
kubectl -n slurm describe pod -l k8s-bridge.x-k8s.io/slurm-job-id=<JOB_ID>
kubectl -n slurm logs -l k8s-bridge.x-k8s.io/slurm-job-id=<JOB_ID> -c slurmd
```

8. **`JobState=PENDING` (`Reason=BadConstraints`, `PartitionConfig`, or `Nodes required for job are DOWN`) while this job's `slurmd` pods are `Running` and registered in `scontrol show nodes` (`PartitionConfig` for up to ~1m during startup is normal):**
   - Root cause: `WorkloadMixing.spec.partitionsMapping[].dynamicNodeFeature` does not match the `NodeSet` `Feature` in `slurm.conf` (`TotalNodes=0` in `scontrol show partition`), or a stale dynamic node from a previous force-deleted pod is registered in `slurmctld`. Check `scontrol show nodes`.

### Step 3: Inspect `k8s-bridge` Prometheus Metrics (Optional)
The `kube-apiserver` service proxy strips `Authorization` headers, causing `401` errors against secured `/metrics` (`WithAuthenticationAndAuthorization`). Query `/metrics` via a self-cleaning subshell (creates no pods, leaves no lingering processes; on non-GKE clusters, replace `gcloud auth print-access-token` with `kubectl create token <sa-with-metrics-reader>`):

```bash
(
  SVC="svc/k8s-bridge-metrics-service" # or svc/k8s-bridge-controller-manager-metrics-service for Kustomize
  PORT="metrics"                       # or https for Kustomize
  timeout 15 kubectl -n k8s-bridge-system port-forward "${SVC}" "18443:${PORT}" >/dev/null 2>&1 &
  PF=$!; trap "kill ${PF} 2>/dev/null || true" EXIT
  sleep 2
  curl -sfk -H "Authorization: Bearer $(gcloud auth print-access-token)" https://127.0.0.1:18443/metrics | grep "^k8s_bridge_"
)
```

---

## 4. Verification

Confirm that your diagnosis output provides:
1. The observed state across Slurm (`JobState`, `Reason`, `Comment`), Kubernetes (`JobSet` `SUSPENDED`/`READY`, Pod phase), and Kueue (`Workload` `Admitted` condition).
2. The single root-cause category from [references/symptom-decision-table.md](references/symptom-decision-table.md).
3. The recommended next skill or command for the user to approve.

---

## 5. Handoffs & Related Skills

- **To clean up wedged Slurm jobs, orphaned `JobSet` objects, or stale dynamic Slurm nodes:** Read [skills/ops-cleanup-state/SKILL.md](../ops-cleanup-state/SKILL.md).
- **To fix partition parity, `dynamicNodeFeature`, GPU `mappedResources`, or user UIDs:** Read [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md).
- **To fix a controller or translator bug identified in logs:** Read [skills/dev-controller-and-translator/SKILL.md](../dev-controller-and-translator/SKILL.md).
