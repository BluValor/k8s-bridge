---
name: ops-cleanup-state
description: >-
  Drains and cleans up wedged Slurm jobs, orphaned Kubernetes JobSets,
  Kueue Workloads, dynamic slurmd worker pods, and stale dynamic Slurm node
  registrations (DOWN or orphaned slurm-job-* nodes). Use when resetting cluster
  state between runs, removing stuck dynamic Slurm nodes that cause
  BadConstraints, or cancelling wedged workloads. Don't use for read-only
  root-cause diagnosis of stuck jobs (use ops-diagnose-workload) or tearing down
  the Slurm release, CRs, operator stack, and cloud cluster (use
  infra-configure-crs, infra-deploy-stack, and infra-provision-cluster).
---

# State Cleanup and Stale Node Deregistration (`ops-cleanup-state`)

## 1. Preconditions & Inputs

Before executing any cleanup commands, verify or ask the user for:
1. **Scope of Cleanup:**
   - **Targeted cleanup (`JOB_ID`):** Clean up a single wedged Slurm job ID (`slurm-job-<JOB_ID>` and its dynamic nodes `slurm-job-<JOB_ID>-slurmd-*`).
   - **Full namespace reset (`ALL`):** Cancel all Slurm jobs, delete all `JobSet` objects and dynamic `slurmd` pods, and deregister all `slurm-job-*` dynamic nodes in the namespace (mirrors `ResetSlurmAndK8sState` in `test/utils/slurm.go`).
2. **Target Namespace (`NAMESPACE`):** Default `slurm`.
3. **Login / Controller Pods:** `slurm-controller-0` (`-c slurmctld`) and `deploy/slurm-login-slinky` (`-c login`, or `deploy/slurm-login` in Kind E2E).

---

## 2. Guardrails

- **Confirmation gate:** Present the target cluster context (`kubectl config current-context`), namespace, target job IDs or node names, and commands to the user, and obtain user confirmation before running any deletion or cancellation command.
- **Do not delete dynamic `slurmd` pods with `--force --grace-period=0`:** Force-deleting a running `slurmd` pod skips its container `PreStop` hook (`scontrol update nodename=$SLURM_NODE_NAME state=DOWN ... && scontrol delete nodename=$SLURM_NODE_NAME`). The terminating container can then re-register a stale dynamic node with `slurmctld`, causing subsequent jobs to fail with `BadConstraints`. Use `--grace-period=5 --wait=true`.
- **Follow the cleanup sequence (Jobs -> `JobSet` -> Pods -> Stale Slurm Nodes):** Deleting Slurm nodes while the `slurmd` pod is still alive allows `slurmd` to re-register the node before the pod terminates. Terminate the `JobSet` and `slurmd` pods **before** scrubbing remaining `slurm-job-*` nodes in `slurmctld`.
- **Allow self-healing before manual cleanup:** When `k8s-bridge` is running, orphaned `JobSets` auto-delete after `--orphan-grace-period` (default 1m); clean up manually only if the state persists past that window or `k8s-bridge` is not running.
- **Mark dynamic Slurm nodes `DOWN` before running `scontrol delete`:** `slurmctld` refuses to delete a dynamic node that is in `ALLOCATED`, `MIXED`, or `COMPLETING` state. Run `scontrol update nodename=<node> state=DOWN reason=cleanup` before `scontrol delete nodename=<node>`.

---

## 3. Step-by-Step Procedure

> **Scope Selection:** In each step below, run **only Option A** (for a single `<JOB_ID>`) **or Option B** (for a full namespace reset). Do not execute both commands in a code block.

### Step 1: Cancel Target Slurm Job(s)
After user confirmation, cancel the target Slurm job (or all jobs for a full reset):

```bash
# Option A: Single job ID
kubectl -n slurm exec slurm-controller-0 -c slurmctld -- scancel <JOB_ID>

# Option B: All jobs on bridged partition(s) (or omit -p to cancel all jobs in Slurm queue)
kubectl -n slurm exec slurm-controller-0 -c slurmctld -- sh -c 'squeue -a -h -o "%i" | xargs -r scancel'
```

### Step 2: Delete `JobSet` Object(s) with Foreground Cascading Deletion
Delete the `JobSet` in Kubernetes using `--cascade=foreground --wait=true` so `k8s-bridge` deregisters its dynamic Slurm nodes, removes the `k8s-bridge.x-k8s.io/slurm-node-cleanup` finalizer, and garbage-collects child `Jobs` and Kueue `Workloads`:

```bash
# Option A: Single job ID
kubectl -n slurm delete jobset slurm-job-<JOB_ID> \
  --cascade=foreground --wait=true --timeout=60s --ignore-not-found

# Option B: All JobSet objects in namespace
kubectl -n slurm delete jobset --all \
  --cascade=foreground --wait=true --timeout=60s
```

If `k8s-bridge` is running and a `JobSet` is stuck in `Terminating` (or leaked after job completion), check if the `JobSet` lost its `k8s-bridge.x-k8s.io/slurm-job-id` label or `WorkloadMixing` `ownerReferences` (with current `WorkloadMixing` `uid`) and restore them so `k8s-bridge` finalizes it; if `JobSet` has only `finalizers: ["foregroundDeletion"]`, `k8s-bridge` is already done — inspect child `Job`/`Pod` `.metadata.finalizers` and remove only orphaned foreign finalizers (never remove `foregroundDeletion` or `batch.kubernetes.io/job-tracking`). If `k8s-bridge` was uninstalled or cannot be restored (if it was only scaled to `0`, scale it back up first) and a `JobSet` is stuck in `Terminating`, remove only `k8s-bridge.x-k8s.io/slurm-node-cleanup` (so `jobset.x-k8s.io/jobset-controller` still cleans up child `Jobs`) and then run Step 4:

```bash
kubectl get jobset <JOBSET_NAME> -n slurm -o json \
  | jq '.metadata.finalizers -= ["k8s-bridge.x-k8s.io/slurm-node-cleanup"]' \
  | kubectl replace -f -
```

### Step 3: Terminate Remaining Dynamic `slurmd` Pods
If worker pods remain after `JobSet` deletion (Steps 3–4 are typically no-ops when Step 2 completed cleanly with `k8s-bridge` running), delete them with `--grace-period=5 --wait=true` (never `--force --grace-period=0`):

```bash
# Option A: Single job ID
kubectl -n slurm delete pod -l k8s-bridge.x-k8s.io/slurm-job-id=<JOB_ID> \
  --grace-period=5 --wait=true --ignore-not-found

# Option B: All k8s-bridge worker pods
kubectl -n slurm delete pod -l app.kubernetes.io/managed-by=k8s-bridge,app.kubernetes.io/component=worker \
  --grace-period=5 --wait=true --ignore-not-found
```

### Step 4: Deregister Stale Dynamic Nodes in `slurmctld`
Once all target `slurmd` pods have terminated, run [scripts/cleanup-stale-nodes.sh](scripts/cleanup-stale-nodes.sh) to mark remaining `slurm-job-<JOB_ID>-slurmd-*` (or all `slurm-job-*`) nodes `DOWN` and delete them from `slurmctld`:

```bash
# Option A: Deregister dynamic nodes for a single job ID
bash skills/ops-cleanup-state/scripts/cleanup-stale-nodes.sh slurm <JOB_ID>

# Option B: Deregister all slurm-job-* dynamic nodes
bash skills/ops-cleanup-state/scripts/cleanup-stale-nodes.sh slurm
```

---

## 4. Verification

1. Verify no orphaned `JobSet`, Kueue `Workload`, or dynamic `slurmd` worker pods remain for the cleaned-up scope:

```bash
kubectl -n slurm get jobset,pods -l app.kubernetes.io/managed-by=k8s-bridge
kubectl -n slurm get workloads
```

2. Verify `slurmctld` has no remaining `slurm-job-*` dynamic nodes or stuck jobs:

```bash
kubectl -n slurm exec slurm-controller-0 -c slurmctld -- squeue
kubectl -n slurm exec slurm-controller-0 -c slurmctld -- scontrol -o show nodes
```

---

## 5. Handoffs & Related Skills

- **To diagnose why a workload became wedged before cleaning it up:** Read [skills/ops-diagnose-workload/SKILL.md](../ops-diagnose-workload/SKILL.md).
- **To tear down `WorkloadMixing`/Kueue CRs, the Slinky `slurm` release, the base operators, or the cloud cluster:** Read [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md), [skills/infra-deploy-stack/SKILL.md](../infra-deploy-stack/SKILL.md), and [skills/infra-provision-cluster/SKILL.md](../infra-provision-cluster/SKILL.md).
