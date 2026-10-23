---
name: infra-configure-crs
description: >-
  Configures, applies, and removes all workload-mixing Custom Resources (CRs)
  and Slurm partitions for CPU, GPU, priority, and GKE CCC workloads: Kueue CRs
  (ResourceFlavor, ClusterQueue, LocalQueue, WorkloadPriorityClass), GKE
  ComputeClass CRs, Slinky slurm Helm release (slurm-values.yaml and Lua
  plugins), WorkloadMixing CRs, non-root user verification, and job submission
  checks. Use when deploying, updating, or removing Slurm partitions, Kueue
  queues, priority classes, or typed/untyped GPU mappedResources. Don't use for
  creating cloud clusters or node pools (use infra-provision-cluster) or
  installing and tearing down the base operators (use infra-deploy-stack).
---

# CR Configuration, Slurm Partitions, and Workload Setup (`infra-configure-crs`)

## 1. Preconditions & Inputs

1. **Base operators installed:** `cert-manager`, `slurm-operator`, `JobSet`, `Kueue`, and `k8s-bridge` must be running in the cluster. If not, read [skills/infra-deploy-stack/SKILL.md](../infra-deploy-stack/SKILL.md) first. If cloud node pools or GPU labels are needed, read [skills/infra-provision-cluster/SKILL.md](../infra-provision-cluster/SKILL.md) first.
2. **Select the target scenario guide and manifests** (read the linked `docs/` guide for scenario architecture and steps instead of writing YAML from memory):

| Scenario | Repository Guide | Kueue / ComputeClass Manifests | `WorkloadMixing` Manifest (`metadata.name`) |
|---|---|---|---|
| **Base CPU Quickstart** | [docs/quickstart.md](../../docs/quickstart.md) (Steps 2, 3, 5, 6, 7) | `docs/examples/quickstart/kueue-resources.yaml` | `docs/examples/quickstart/workloadmixing.yaml` (`name: default`) |
| **Existing Kueue / Slurm Infra** | [docs/existing-infra.md](../../docs/existing-infra.md) (Steps 2, 3, 5, 6) | Existing Kueue queues (`docs/existing-infra.md` Step 2) | `docs/existing-infra.md` Step 5 (`name: slurm-workload-mixing`) |
| **GKE Custom Compute Classes (CCC)** | [docs/gke/ccc.md](../../docs/gke/ccc.md) (Steps 3–8) | `docs/examples/ccc/computeclass.yaml` + `docs/examples/ccc/kueue-resources.yaml` | `docs/examples/quickstart/workloadmixing.yaml` (`name: default`) |
| **Priority & Preemption (`prod` / `batch`)** | [docs/priorities-and-preemption.md](../../docs/priorities-and-preemption.md) (Steps 1–5) | `docs/examples/priority-preemption/kueue-resources.yaml` | `docs/examples/priority-preemption/workloadmixing-priority.yaml` (`name: priority`) |
| **Pre-Provisioned GPU Workloads** | [docs/gpu-workloads.md](../../docs/gpu-workloads.md) (Steps 1–5) | `docs/examples/gpu/kueue-resources.yaml` | `docs/examples/gpu/workloadmixing-gpu.yaml` (or [examples/gpu-mapped-resources.yaml](examples/gpu-mapped-resources.yaml) for typed + untyped GPU requests; `name: gpu`) |
| **GKE DWS Flex-Start GPU Workloads** | [docs/gke/gpu-dws-workloads.md](../../docs/gke/gpu-dws-workloads.md) (Steps 3–7) | `docs/examples/gpu-dws/kueue-resources.yaml` | `docs/examples/gpu-dws/workloadmixing-gpu.yaml` (or [examples/gpu-mapped-resources.yaml](examples/gpu-mapped-resources.yaml) customized with DWS labels/tolerations; `name: gpu`) |

*(Upstream CR references: [Kueue ResourceFlavor](https://kueue.sigs.k8s.io/docs/concepts/resource_flavor/), [Kueue ClusterQueue](https://kueue.sigs.k8s.io/docs/concepts/cluster_queue/), [Kueue LocalQueue](https://kueue.sigs.k8s.io/docs/concepts/local_queue/), [Kueue WorkloadPriorityClass](https://kueue.sigs.k8s.io/docs/concepts/workload_priority_class/), [Slurm slurm.conf](https://slurm.schedmd.com/slurm.conf.html), [Slurm GRES](https://slurm.schedmd.com/gres.html).)*

---

## 2. Guardrails

- **Confirmation gate:** Show the target cluster context (`kubectl config current-context`), namespace (`slurm`), and planned `helm` or `kubectl` commands. Get user confirmation before installing, upgrading, or deleting the `slurm` Helm release or cluster CRs.
- **Enforce 3-place partition parity:** Every bridged partition name must match across all three places:
  1. `controller.slurmctld.env` -> `WORKLOAD_MIXING_PARTITIONS="part1,part2"` (read by `hack/job_submit.lua`; check: `kubectl -n slurm exec slurm-controller-0 -c slurmctld -- printenv WORKLOAD_MIXING_PARTITIONS`).
  2. `controller.extraConf` (`slurm.conf`) -> `CliFilterParameters=workload_mixing_partitions=part1,part2` (read by `hack/cli_filter.lua`; check login cache: `kubectl -n slurm exec deploy/slurm-login-slinky -c login -- grep CliFilterParameters /run/slurm/conf/slurm.conf` and controller: `scontrol show config | grep CliFilterParameters`).
  3. `WorkloadMixing.spec.partitionsMapping[].partition` (read by `k8s-bridge`; check: `kubectl -n slurm get workloadmixing -o jsonpath='{..partitionsMapping[*].partition}'`).
- **Enforce `DynamicNodeFeature` parity and `ignore_constraint_validation`:**
  - `WorkloadMixing.spec.partitionsMapping[].dynamicNodeFeature` (`^[a-zA-Z0-9_-]+$`, max 64 chars) must match the `Feature=` on the partition's `NodeSet` in `slurm.conf` (`NodeSet=k8s_nodes Feature=k8s_dynamic`).
  - `controller.extraConf` must include `SchedulerParameters=ignore_constraint_validation` so `slurmctld` accepts `Constraints=k8s-id-<jobID>` when `k8s-bridge` releases the hold.
- **Mount both Lua scripts on every `helm install`/`upgrade slurm`:** Pass `--set-file 'configFiles.job_submit\.lua=hack/job_submit.lua'` and `--set-file 'configFiles.cli_filter\.lua=hack/cli_filter.lua'`.
- **Specify a valid `slurmd` image in `WorkloadMixing.spec.podTemplate`:** Omitting `podTemplate` causes `k8s-bridge` to fall back to `"placeholder-image"`, which fails with `ImagePullBackOff`.
- **Define both typed and untyped GPU `mappedResources` entries if users submit both `--gres=gpu:<type>:1` and `--gpus=1`:** `docs/examples/gpu/workloadmixing-gpu.yaml` and `docs/examples/gpu-dws/workloadmixing-gpu.yaml` only define `resourceType: nvidia_l4`. Because `JobSetTranslator` matches `(resourceName, resourceType)`, untyped `--gpus=1` or `--gres=gpu:1` jobs fail with `Translation failed: missing GRES mapping for gpu:` unless an untyped entry (`resourceType` omitted) is also present (see [examples/gpu-mapped-resources.yaml](examples/gpu-mapped-resources.yaml)).

---

## 3. Step-by-Step Procedure

### Step 1: Create the `slurm` Namespace and Apply Kueue & `ComputeClass` CRs
Create the `slurm` namespace and apply the Kueue CRs (`ResourceFlavor`, `ClusterQueue`, `LocalQueue`, and optional `WorkloadPriorityClass`) and GKE `ComputeClass` CR for your scenario from Section 1:

```bash
kubectl create namespace slurm --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f <SCENARIO_KUEUE_RESOURCES_YAML>
# If using GKE CCC (docs/gke/ccc.md Steps 3-4), also apply ComputeClass:
# kubectl apply -f docs/examples/ccc/computeclass.yaml
```

- **For GKE DWS Flex-start GPUs ([docs/gke/gpu-dws-workloads.md](../../docs/gke/gpu-dws-workloads.md) Step 3):** Ensure `ResourceFlavor` (`gpu-flavor`) sets `nodeLabels` (`cloud.google.com/gke-accelerator: nvidia-l4`, `cloud.google.com/gke-flex-start: "true"`). It must also tolerate `nvidia.com/gpu` (`Exists`) and `cloud.google.com/gke-queued: "true"` (`Equal`), without creating a Kueue `ProvisioningRequestConfig`.

### Step 2: Install or Upgrade the Slinky `slurm` Cluster with Partition & Lua Plugin Config
Select the `slurm-values.yaml` for your scenario (`docs/examples/quickstart/slurm-values.yaml`, `docs/examples/priority-preemption/slurm-values.yaml`, `docs/examples/gpu/slurm-values.yaml`, or `docs/examples/gpu-dws/slurm-values.yaml`), or merge their settings when combining features:
- **Priority & Preemption ([docs/priorities-and-preemption.md](../../docs/priorities-and-preemption.md) Step 2):** Include `JobRequeue=1` and `SlurmdTimeout=60` in `controller.extraConf`.
- **GPU Workloads ([docs/gpu-workloads.md](../../docs/gpu-workloads.md) Step 2):** Include `configFiles.gres.conf: "AutoDetect=nvidia"` and `SelectType=select/cons_tres`, `GresTypes=gpu`, `ReturnToService=2`, `EnforcePartLimits=NO`, `MaxNodeCount=64` in `controller.extraConf`.

Run `helm upgrade --install slurm` with both Lua plugins mounted:

```bash
helm upgrade --install slurm oci://ghcr.io/slinkyproject/charts/slurm \
  --version 1.2.2 --namespace slurm \
  -f <SCENARIO_SLURM_VALUES_YAML> \
  --set-file 'configFiles.job_submit\.lua=hack/job_submit.lua' \
  --set-file 'configFiles.cli_filter\.lua=hack/cli_filter.lua' \
  --wait
```

- **Drain before `helm upgrade slurm` or Slinky `NodeSet` changes:** With `persistence.enabled: false` (default in examples), `helm upgrade slurm` or adding/deleting a Slinky `NodeSet` restarts `slurm-controller-0` and wipes in-memory jobs and node state; drain or cancel active jobs and wait for managed `JobSet` objects to finalize first (`ops-cleanup-state`).
- **After upgrading `slurm.conf` or Lua scripts:** If `GresTypes` or `SelectType` changed, restart the controller (`kubectl -n slurm delete pod slurm-controller-0`). Delete the login pod (`kubectl -n slurm delete pod -l app.kubernetes.io/name=login` — Slinky's `LoginSet` controller reverts `rollout restart`) so it refreshes `/run/slurm/conf/`, re-create any `useradd` test users, and verify with `scontrol show config | grep -iE 'GresTypes|EnforcePartLimits|SelectType'`.
- **Immutable `clusterName` across example files:** If upgrading an existing release between `quickstart`/`ccc`/`priority-preemption` and `gpu`/`gpu-dws` values fails on immutable `ClusterName`, pass `--set clusterName=<live-value>` (`null` if `kubectl -n slurm get controller -o jsonpath='{..clusterName}'` is empty).

### Step 3: Apply the `WorkloadMixing` CR
Apply the `WorkloadMixing` CR for your scenario from Section 1 (delete any previous `WorkloadMixing` CR in namespace `slurm` first if switching scenarios on the same cluster):
- **Base CPU / GKE CCC:** `kubectl apply -f docs/examples/quickstart/workloadmixing.yaml` (`name: default`; see [docs/quickstart.md](../../docs/quickstart.md) Step 5 and [docs/gke/ccc.md](../../docs/gke/ccc.md) Step 6). *(Note: `docs/examples/ccc/kueue-resources.yaml` modifies `default-flavor` and `cluster-queue` in place; re-apply `docs/examples/quickstart/kueue-resources.yaml` before disabling NAP to remove the `compute-class` node label.)*
- **Priority & Preemption:** `kubectl apply -f docs/examples/priority-preemption/workloadmixing-priority.yaml` (`name: priority`; see [docs/priorities-and-preemption.md](../../docs/priorities-and-preemption.md) Step 3).
- **Pre-Provisioned GPU:** `kubectl apply -f docs/examples/gpu/workloadmixing-gpu.yaml` (`name: gpu`; see [docs/gpu-workloads.md](../../docs/gpu-workloads.md) Step 3), or `kubectl apply -f skills/infra-configure-crs/examples/gpu-mapped-resources.yaml` (see [examples/gpu-mapped-resources.yaml](examples/gpu-mapped-resources.yaml)) if users submit both typed (`--gres=gpu:nvidia_l4:1`) and untyped (`--gpus=1`) GPU requests.
- **GKE DWS Flex-Start GPU:** `kubectl apply -f docs/examples/gpu-dws/workloadmixing-gpu.yaml` (`name: gpu`; see [docs/gke/gpu-dws-workloads.md](../../docs/gke/gpu-dws-workloads.md) Step 5). If users also submit untyped (`--gpus=1`) GPU requests, add the untyped `mappedResources` entry from [examples/gpu-mapped-resources.yaml](examples/gpu-mapped-resources.yaml).

*(Stop here and proceed to Section 4 Verification when configuring partitions or CRs. For live single-field fixes on `WorkloadMixing`, use `kubectl -n slurm patch workloadmixing <name> --type=merge -p '...'` for scalars/objects or `--type=json -p '[{"op":"add","path":"/spec/partitionsMapping/0/<field>","value":"..."}]'` for fields inside a list item, and align your manifest. Run Step 4 only when asked to remove the CRs and Slurm release.)*

### Step 4: CR & Slurm Release Teardown (Run ONLY When Requested to Remove CRs)
Get user confirmation before deleting CRs or uninstalling releases. Because managed `JobSet` objects carry the `k8s-bridge.x-k8s.io/slurm-node-cleanup` finalizer, delete all `WorkloadMixing` CRs first and wait until `k8s-bridge` has finalized and removed all managed `JobSet` objects before uninstalling the `slurm` Helm release, Kueue/`ComputeClass` CRs, or base operators via `infra-deploy-stack` (see [docs/quickstart.md](../../docs/quickstart.md) `## 7. Cleanup`, [docs/existing-infra.md](../../docs/existing-infra.md) `## 6. Cleanup safely`, and [docs/gke/ccc.md](../../docs/gke/ccc.md) `## 8. Cleanup`):

```bash
kubectl -n slurm delete workloadmixing --all --ignore-not-found
# Wait until this returns no resources before uninstalling slurm or k8s-bridge:
kubectl get jobsets -A -l app.kubernetes.io/managed-by=k8s-bridge
helm uninstall slurm -n slurm
kubectl delete -f <SCENARIO_KUEUE_RESOURCES_YAML> --ignore-not-found
# If GKE CCC was configured, also delete the ComputeClass CR:
# kubectl delete -f docs/examples/ccc/computeclass.yaml --ignore-not-found
```

---

## 4. Verification

1. Verify `WorkloadMixing` (`Available=True`, `Reason=Reconciled`) and Slurm partitions:

```bash
kubectl -n slurm wait --for=condition=Available workloadmixing --all --timeout=60s
kubectl -n slurm exec deploy/slurm-login-slinky -c login -- sinfo
```

2. Provision a non-root user (`someuser`) on the login pod and submit a verification job (`auth/slurm` with `use_client_ids` and `enable_nss_slurm` propagates `someuser` from the login pod to `slurmstepd` on the dynamic `slurmd` worker pods):
   - **CPU / CCC verification ([docs/quickstart.md](../../docs/quickstart.md) Step 6):**

```bash
kubectl -n slurm exec deploy/slurm-login-slinky -c login -- sh -c \
  "id -u someuser >/dev/null 2>&1 || useradd -m -s /bin/bash someuser"
kubectl -n slurm exec deploy/slurm-login-slinky -c login -- runuser -u someuser -- \
  srun --partition=k8s-partition --nodes=1 --ntasks-per-node=1 --cpus-per-task=1 --mem=2G --chdir=/tmp hostname
```
   *For `sbatch`, check `scontrol show job <JOB_ID>` before `MinJobAge` (default 300s) expires (`sacct` is disabled without `slurmdbd`; `--chdir=/tmp` logs to the ephemeral worker pod; brief `Reason=PartitionConfig` while dynamic nodes register is normal).*

   - **GPU verification ([docs/gpu-workloads.md](../../docs/gpu-workloads.md) Step 4 — for `srun`, pass `--export=ALL,LD_LIBRARY_PATH=/usr/local/nvidia/lib64` and run `/usr/local/nvidia/bin/nvidia-smi`; for `sbatch`, set `LD_LIBRARY_PATH` inside `--wrap` instead of `--export=ALL,...` to avoid `user_env_retrieval_failed_requeued_held`):**

```bash
kubectl -n slurm exec deploy/slurm-login-slinky -c login -- runuser -u someuser -- \
  srun --partition=k8s-gpu --gres=gpu:nvidia_l4:1 --cpus-per-task=2 --mem=4G \
  --export=ALL,LD_LIBRARY_PATH=/usr/local/nvidia/lib64 --chdir=/tmp /usr/local/nvidia/bin/nvidia-smi
```

   - **Priority preemption verification:** Follow [docs/priorities-and-preemption.md](../../docs/priorities-and-preemption.md) (`## 4. Run priority and preemption tests`) using `runuser -u someuser -- sbatch --chdir=/tmp ...` to submit a `batch` job followed by a `prod` job and verify `prod` preempts and requeues `batch`.

---

## 5. Handoffs & Related Skills

- **To create or update GKE clusters and CCC or DWS Flex-start node pools:** Read [skills/infra-provision-cluster/SKILL.md](../infra-provision-cluster/SKILL.md).
- **To install, upgrade, or tear down the base operators (`cert-manager`, `slurm-operator`, `JobSet`, `Kueue`, `k8s-bridge`):** Read [skills/infra-deploy-stack/SKILL.md](../infra-deploy-stack/SKILL.md).
- **If a submitted job is rejected, stays held, or fails with `Translation failed:` or `BadConstraints`:** Read [skills/ops-diagnose-workload/SKILL.md](../ops-diagnose-workload/SKILL.md).
