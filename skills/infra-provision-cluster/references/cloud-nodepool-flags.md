# Cloud Node Pool Flags, Node Labels, and Documentation Reference

## 1. Provisioning Modes & Documentation Map

| Mode | Minimum Version | Node Labels & Taints Provisioned by Infrastructure | Repo Guide & Upstream Docs |
|---|---|---|---|
| **Option A: GKE Custom Compute Classes (CCC)** | GKE `1.31+` | GKE NAP provisions nodes matching `ComputeClass` (`cloud.google.com/compute-class: cost-optimized`) when pods are admitted. | Repo: [docs/gke/ccc.md](../../../docs/gke/ccc.md)<br>Upstream: [GKE Custom Compute Classes](https://cloud.google.com/kubernetes-engine/docs/concepts/custom-compute-classes), [Configure CCC](https://cloud.google.com/kubernetes-engine/docs/how-to/custom-compute-classes) |
| **Option B: GKE DWS Flex-Start GPU Pool** | GKE `1.35.2-gke.1842000+` | Labels: `cloud.google.com/gke-accelerator: nvidia-l4`, `cloud.google.com/gke-flex-start: "true"`<br>Taints (`NoSchedule`): `nvidia.com/gpu` (`Exists`), `cloud.google.com/gke-queued: "true"` (`Equal`) | Repo: [docs/gke/gpu-dws-workloads.md](../../../docs/gke/gpu-dws-workloads.md)<br>Upstream: [GKE Flex-start provisioning](https://cloud.google.com/kubernetes-engine/docs/how-to/provisioning-models#flex-start), [About DWS](https://cloud.google.com/kubernetes-engine/docs/concepts/about-dws) |
| **Option C: Pre-Provisioned GPU Nodes** | Kubernetes `1.28+` + NVIDIA Device Plugin | Label: `nvidia.com/gpu.product: NVIDIA-L4` (set by GPU Feature Discovery or `kubectl label`)<br>Taint (`NoSchedule`): `nvidia.com/gpu` (`Exists`) | Repo: [docs/gpu-workloads.md](../../../docs/gpu-workloads.md)<br>Upstream: [NVIDIA k8s-device-plugin](https://github.com/NVIDIA/k8s-device-plugin) |

---

## 2. `gcloud` & `kubectl` Flag Semantics and Customization Rules

| Flag / Label | Used In | Purpose & Customization Rules |
|---|---|---|
| `--machine-type=e2-standard-4 --num-nodes=2` | Option A & B (Default Pool) | Sizes the default system node pool to host Slinky `slurm-operator`, `slurmctld`, `slurmrestd`, `mariadb`, `kueue`, and `k8s-bridge` control-plane pods. |
| `--enable-autoprovisioning --min-cpu=1 --max-cpu=64 --min-memory=1 --max-memory=256` | Option A (GKE CCC) | Enables GKE Node Auto-Provisioning (NAP) and sets cluster-wide CPU/memory bounds for `ComputeClass` (`nodePoolAutoCreation.enabled: true`). Adjust `--max-cpu` and `--max-memory` to match your `ClusterQueue` nominal quota. |
| `--machine-type=g2-standard-8` | Option B (GKE DWS GPU) | Selects NVIDIA L4 GPU machine family (`g2-standard-*`). If changing to A100 (`a2-highgpu-*`) or H100 (`a3-highgpu-*`), update `--accelerator` and `ResourceFlavor` / `WorkloadMixing` GPU types to match. |
| `--accelerator=type=nvidia-l4,count=1,gpu-driver-version=default` | Option B (GKE DWS GPU) | Attaches 1 L4 GPU per node and installs the GKE default NVIDIA driver so `nvidia-smi` and CUDA work inside `slurmd` pods (`test/e2e/infra/gke-gpu-cluster-setup.sh` omits `gpu-driver-version=default` because it only runs `sleep`). |
| `--flex-start --enable-autoscaling --num-nodes=0 --min-nodes=0 --max-nodes=2` | Option B (GKE DWS GPU) | Configures DWS Flex-start queued provisioning scaling from 0 nodes up to `--max-nodes`. |
| `--location-policy=ANY` | Option B (GKE DWS GPU) | Allows the cluster autoscaler to place DWS Flex-start nodes in any zone with available GPU capacity. |
| `--reservation-affinity=none` | Option B (GKE DWS GPU) | Required by GKE whenever `--flex-start` is enabled on a node pool. |
| `nvidia.com/gpu.product=NVIDIA-L4` | Option C (Pre-Provisioned GPU) | Node label matched by `ResourceFlavor.spec.nodeLabels` and `WorkloadMixing.spec.mappedResources[].nodeSelectors` in `docs/examples/gpu/`. |

---

## 3. How Node Pool Flags Connect to Layer 3 (`infra-configure-crs`)

- **Do not create Kueue `ProvisioningRequestConfig` resources for DWS Flex-start pools:**
  - In `infra-configure-crs`, the Kueue `ClusterQueue` gates GPU quota. Admitted `JobSet` pods carry `nodeSelector` (`cloud.google.com/gke-accelerator: nvidia-l4`, `cloud.google.com/gke-flex-start: "true"`) and tolerations (`nvidia.com/gpu` and `cloud.google.com/gke-queued: "true"`), which trigger GKE Cluster Autoscaler and DWS.
