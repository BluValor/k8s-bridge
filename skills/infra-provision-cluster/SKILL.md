---
name: infra-provision-cluster
description: >-
  Creates, updates, or deletes cloud Kubernetes clusters and autoscaling CPU or
  GPU node pools on GKE (Node Auto-Provisioning for Custom Compute Classes, DWS
  Flex-start GPU node pools, or labeling pre-provisioned GPU nodes). Use when
  creating, resizing, updating, or deleting cloud clusters and node pools for
  workload mixing. Don't use for installing Helm charts and operators inside the
  cluster (use infra-deploy-stack) or configuring and applying Kueue,
  ComputeClass, Slurm partition, and WorkloadMixing CRs (use
  infra-configure-crs).
---

# Cloud Cluster & Node Pool Provisioning (`infra-provision-cluster`)

## 1. Preconditions & Inputs

Before running `gcloud` or `kubectl label` commands, check or ask the user for:
1. **`PROJECT_ID`:** Google Cloud project ID (`gcloud config get-value project`).
2. **`LOCATION`:** Compute zone or region (e.g., `us-central1-a`).
3. **`CLUSTER_NAME`:** Target GKE cluster name.
4. **Provisioning Mode:**
   - **Option A — GKE Cluster with Node Auto-Provisioning (NAP) for Custom Compute Classes (CCC):** Requires GKE `1.31+`. See [docs/gke/ccc.md](../../docs/gke/ccc.md) (`## 1. Create a GKE cluster`).
   - **Option B — GKE Cluster with DWS Flex-Start GPU Node Pool:** Requires GKE `1.35.2-gke.1842000` or later. See [docs/gke/gpu-dws-workloads.md](../../docs/gke/gpu-dws-workloads.md) (`## 1. Create a GKE cluster with a DWS Flex GPU node pool`).
   - **Option C — Pre-Provisioned GPU Nodes:** Verify the NVIDIA device plugin and apply the `nvidia.com/gpu.product` node label. See [docs/gpu-workloads.md](../../docs/gpu-workloads.md) (`## Prerequisites`).
5. Read [references/cloud-nodepool-flags.md](references/cloud-nodepool-flags.md) for GKE version requirements, `gcloud` flag semantics, and links to upstream GKE documentation.

---

## 2. Guardrails

- **Confirmation gate before billable cloud actions:** Show the `gcloud container clusters` or `gcloud container node-pools` command (including `PROJECT_ID`, `LOCATION`, machine types, and node limits) and get user approval before running it.
- **Keep cluster creation separate from operator and CR installation:** This skill only provisions cloud clusters/node pools and node labels. Do not install Helm operators here (use `infra-deploy-stack`) and do not apply `ComputeClass`, Kueue, or `WorkloadMixing` CRs here (use `infra-configure-crs`).
- **Verify GKE version requirements first:**
  - GKE Custom Compute Classes (CCC) require GKE `1.31+` ([GKE CCC docs](https://cloud.google.com/kubernetes-engine/docs/concepts/custom-compute-classes)).
  - GKE DWS Flex-start (`--flex-start`) requires GKE `1.35.2-gke.1842000` or later ([GKE Flex-start docs](https://cloud.google.com/kubernetes-engine/docs/how-to/provisioning-models#flex-start)).
- **Install GPU drivers for CUDA workloads:** `test/e2e/infra/gke-gpu-cluster-setup.sh` provisions GPU nodes without drivers (`sleep` scheduling tests only). For clusters running GPU jobs (`nvidia-smi` / CUDA), include `gpu-driver-version=default` in `--accelerator`.

---

## 3. Step-by-Step Procedure

> **Branch Selection:** Select **Option A** (Step 1), **Option B** (Step 2), or **Option C** (Step 3) based on the user's target infrastructure. Run **Step 4** only when asked to delete a cloud cluster or node pool.

### Step 1: Option A — Create or Update a GKE Cluster for Custom Compute Classes (CCC)
Follow [docs/gke/ccc.md](../../docs/gke/ccc.md) (`## 1. Create a GKE cluster`) to create a new GKE cluster with Node Auto-Provisioning (NAP) enabled, or enable NAP on an existing GKE cluster (requires user confirmation):

```bash
gcloud config set project <PROJECT_ID>
# Create a new GKE cluster with NAP (or use 'clusters update' without --machine-type/--num-nodes):
gcloud container clusters create <CLUSTER_NAME> --location=<LOCATION> \
  --machine-type=e2-standard-4 --num-nodes=2 \
  --enable-autoprovisioning --min-cpu=1 --max-cpu=64 --min-memory=1 --max-memory=256
gcloud container clusters get-credentials <CLUSTER_NAME> --location=<LOCATION>
```

### Step 2: Option B — Create a GKE Cluster and DWS Flex-Start GPU Node Pool
Follow [docs/gke/gpu-dws-workloads.md](../../docs/gke/gpu-dws-workloads.md) (`## 1. Create a GKE cluster with a DWS Flex GPU node pool`). Create the base GKE cluster (`1.35.2-gke.1842000+`) if needed, then create the scale-from-zero DWS Flex-start GPU node pool (requires user confirmation):

```bash
gcloud config set project <PROJECT_ID>
# 1. Create cluster if it does not exist yet:
gcloud container clusters create <CLUSTER_NAME> --location=<LOCATION> \
  --cluster-version=1.35.2-gke.1842000 --num-nodes=2 --machine-type=e2-standard-4
# 2. Create scale-from-zero DWS Flex-start GPU node pool:
gcloud container node-pools create dws-gpu-pool --cluster=<CLUSTER_NAME> --location=<LOCATION> \
  --machine-type=g2-standard-8 --accelerator=type=nvidia-l4,count=1,gpu-driver-version=default \
  --flex-start --enable-autoscaling --num-nodes=0 --min-nodes=0 --max-nodes=2 \
  --location-policy=ANY --reservation-affinity=none
gcloud container clusters get-credentials <CLUSTER_NAME> --location=<LOCATION>
```

### Step 3: Option C — Verify and Label Pre-Provisioned GPU Nodes
If GPU nodes are already provisioned in the cluster, follow [docs/gpu-workloads.md](../../docs/gpu-workloads.md) (`## Prerequisites`) to verify `nvidia.com/gpu` allocatable capacity and ensure the GPU model label (`nvidia.com/gpu.product=NVIDIA-L4`) is present on the GPU nodes:

```bash
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.status.allocatable.nvidia\.com/gpu}{"\n"}{end}'
# Apply the GPU product label if missing (e.g. on nodes without GFD):
kubectl label nodes -l <GPU_NODE_SELECTOR> nvidia.com/gpu.product=NVIDIA-L4 --overwrite
```

### Step 4: Cloud Cluster or Node Pool Teardown (Run ONLY When Requested to Delete Cloud Infra)
First remove in-cluster CRs (`infra-configure-crs`) and operators (`infra-deploy-stack`). Then get user confirmation and delete the node pool or cluster (see [docs/gke/ccc.md](../../docs/gke/ccc.md) `## 8. Cleanup` and [docs/gke/gpu-dws-workloads.md](../../docs/gke/gpu-dws-workloads.md) `## 7. Clean up`):

```bash
# Delete only the DWS GPU node pool:
gcloud container node-pools delete dws-gpu-pool --cluster=<CLUSTER_NAME> --location=<LOCATION>
# Or disable NAP on an existing cluster (docs/gke/ccc.md §8):
gcloud container clusters update <CLUSTER_NAME> --location=<LOCATION> --no-enable-autoprovisioning
# Or delete the entire GKE cluster:
gcloud container clusters delete <CLUSTER_NAME> --location=<LOCATION>
```

---

## 4. Verification

1. Verify `kubectl` context points to the target cluster and nodes are `Ready`:

```bash
kubectl config current-context
kubectl get nodes
```

2. For Option A (CCC) or Option B (DWS), verify GKE cluster/node-pool status (`RUNNING`):

```bash
gcloud container node-pools list --cluster=<CLUSTER_NAME> --location=<LOCATION>
```

3. For Option C (Pre-provisioned GPU nodes), verify that at least one node exposes `nvidia.com/gpu` capacity and the `nvidia.com/gpu.product` label:

```bash
kubectl get nodes -l nvidia.com/gpu.product=NVIDIA-L4
```

---

## 5. Handoffs & Related Skills

- **Next step — Install the base operators (`cert-manager`, `slurm-operator`, `JobSet`, `Kueue`, `k8s-bridge`):** Read [skills/infra-deploy-stack/SKILL.md](../infra-deploy-stack/SKILL.md).
- **After base operators are installed — Configure and apply `ComputeClass`, Kueue, Slinky `slurm` partition, and `WorkloadMixing` CRs:** Read [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md).
- **To run the automated GKE GPU E2E test suite:** Read [skills/test-e2e-cluster/SKILL.md](../test-e2e-cluster/SKILL.md).
