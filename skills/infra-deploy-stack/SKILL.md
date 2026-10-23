---
name: infra-deploy-stack
description: >-
  Installs, upgrades, and tears down the base Kubernetes operators and Helm
  charts (cert-manager, Slinky slurm-operator CRDs and operator, JobSet before
  Kueue, Kueue, and the k8s-bridge controller chart from OCI or local ./helm /
  Kustomize). Use when installing, upgrading, or uninstalling the base operator
  stack in a cluster. Don't use for creating cloud clusters or node pools (use
  infra-provision-cluster) or configuring and applying Kueue, ComputeClass,
  Slinky Slurm partition, and WorkloadMixing CRs (use infra-configure-crs).
---

# Base Operator & Chart Installation (`infra-deploy-stack`)

## 1. Preconditions & Inputs

Before installing, upgrading, or uninstalling the base operators, check or ask the user for:
1. **Kubernetes Context:** Output of `kubectl config current-context` (Kubernetes `1.28+` required by `JobSet` and `Kueue`).
2. **Existing Cluster Components:** Whether any prerequisite operators (`cert-manager`, `slurm-operator`, `JobSet`, `Kueue`) are already installed (see [docs/existing-infra.md](../../docs/existing-infra.md) `## 1. Install missing prerequisites`).
3. **`k8s-bridge` Installation Method:**
   - **Branch A — Published OCI Helm Chart:** `oci://ghcr.io/googlecloudplatform/charts/k8s-bridge --version 0.1.0` (published GHCR release chart; see [docs/quickstart.md](../../docs/quickstart.md) `## 4. Install k8s-bridge`).
   - **Branch B — Build Image & Install Local `./helm` Chart:** Used when building from source and pushing `IMG=<REGISTRY>/k8s-bridge:<TAG>` to a container registry (see `README.md` `## Installation` and `Makefile` `helm-install`).
   - **Branch C — Kustomize (`make install deploy IMG=$IMG`):** Used when deploying from `config/default`.
4. **Required CLI tools:** `kubectl`, `helm` (`3.8+`), and (`docker`, `make` when building from source).

---

## 2. Guardrails

- **Confirmation gate:** Show the target cluster context (`kubectl config current-context`), target namespaces (`cert-manager`, `slinky`, `jobset-system`, `kueue-system`, `k8s-bridge-system`), and planned commands. Get user confirmation before installing, upgrading, or uninstalling releases.
- **Keep operator installation separate from CR and partition configuration:** Use this skill only to install or tear down cluster-level operators and controller deployments. Do not apply Kueue CRs (`ResourceFlavor`, `ClusterQueue`, `LocalQueue`), GKE `ComputeClass` CRs, the Slinky `slurm` cluster release (`slurm-values.yaml` + Lua plugins), or `WorkloadMixing` CRs here; hand off to [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md).
- **Install `JobSet` before `Kueue`:** Kueue detects `JobSet` CRDs at startup. If `JobSet` is installed after `Kueue`, `JobSet` objects stay suspended until you restart `kueue-controller-manager` (`kubectl -n kueue-system rollout restart deploy/kueue-controller-manager`).

---

## 3. Step-by-Step Procedure

### Step 1: Install Prerequisite Operators (`cert-manager`, `slurm-operator`, `JobSet` before `Kueue`, `Kueue`)
Follow [docs/quickstart.md](../../docs/quickstart.md) (`## 1. Install prerequisites`) for a clean cluster, or [docs/existing-infra.md](../../docs/existing-infra.md) (`## 1. Install missing prerequisites`) to install only the missing components. Run the Helm installs in this order (`JobSet` before `Kueue`):

```bash
helm install cert-manager oci://quay.io/jetstack/charts/cert-manager \
  --version v1.21.2 --namespace cert-manager --create-namespace --set crds.enabled=true --wait
helm install slurm-operator-crds oci://ghcr.io/slinkyproject/charts/slurm-operator-crds \
  --version 1.2.2 --namespace slinky --create-namespace --wait
helm install slurm-operator oci://ghcr.io/slinkyproject/charts/slurm-operator \
  --version 1.2.2 --namespace slinky --create-namespace --wait
helm install jobset oci://registry.k8s.io/jobset/charts/jobset \
  --version 0.12.0 --namespace jobset-system --create-namespace --wait
helm install kueue oci://registry.k8s.io/kueue/charts/kueue \
  --version 0.19.4 --namespace kueue-system --create-namespace --wait
```

If `Kueue` was already installed before `JobSet`, restart `kueue-controller-manager` so it registers the `JobSet` framework:

```bash
kubectl -n kueue-system rollout restart deploy/kueue-controller-manager
kubectl -n kueue-system rollout status deploy/kueue-controller-manager
```

*(Upstream installation references: [cert-manager Helm docs](https://cert-manager.io/docs/installation/helm/), [Slinky slurm-operator](https://github.com/SlinkyProject/slurm-operator), [JobSet installation](https://jobset.sigs.k8s.io/docs/installation/), [Kueue installation](https://kueue.sigs.k8s.io/docs/installation/).)*

### Step 2: Install or Upgrade `k8s-bridge` (Select One Branch: A, B, or C)
- **Branch A — Published OCI Helm Chart (see [docs/quickstart.md](../../docs/quickstart.md) `## 4. Install k8s-bridge`):**

```bash
helm upgrade --install k8s-bridge oci://ghcr.io/googlecloudplatform/charts/k8s-bridge \
  --version 0.1.0 --namespace k8s-bridge-system --create-namespace --wait
```

- **Branch B — Build from Source & Install Local `./helm` Chart (see `README.md` `## Installation`):**
  Build and push `IMG=<REGISTRY>/k8s-bridge:<TAG>` to a registry accessible by your cluster, then install `./helm`:

```bash
export REGISTRY=<YOUR_REGISTRY>
export TAG=latest
make docker-build docker-push IMG=${REGISTRY}/k8s-bridge:${TAG}
helm upgrade --install k8s-bridge ./helm \
  --namespace k8s-bridge-system --create-namespace \
  --set image.repository=${REGISTRY}/k8s-bridge --set image.tag=${TAG} --wait
```

- **Branch C — Kustomize Deploy (Alternative to Helm):**
  Build and push `IMG` as in Branch B, then run `make install && make deploy IMG=$IMG`.

*(Stop here after Step 2 and proceed to Section 4 Verification when installing or upgrading. Do not run Step 3 unless the user asked to uninstall the operators.)*

### Step 3: Base Operator Stack Teardown (Run ONLY When Requested to Uninstall)
First remove all `WorkloadMixing` CRs, wait for all managed `JobSet` objects (`k8s-bridge.x-k8s.io/slurm-node-cleanup` finalizer) to terminate, and remove the Slinky `slurm` release and Kueue CRs using [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md) (see [docs/quickstart.md](../../docs/quickstart.md) `## 7. Cleanup` and [docs/existing-infra.md](../../docs/existing-infra.md) `## 6. Cleanup safely`). Verify `kubectl get jobsets -A -l app.kubernetes.io/managed-by=k8s-bridge` returns no resources, then after user confirmation uninstall the operators in reverse order (for Branch C, run `make undeploy uninstall` instead of `helm uninstall k8s-bridge`):

```bash
helm uninstall k8s-bridge -n k8s-bridge-system
helm uninstall kueue -n kueue-system
helm uninstall jobset -n jobset-system
helm uninstall slurm-operator slurm-operator-crds -n slinky
helm uninstall cert-manager -n cert-manager
```

---

## 4. Verification

1. Verify all prerequisite and `k8s-bridge` operator deployments are `Available`:

```bash
kubectl -n cert-manager rollout status deploy/cert-manager deploy/cert-manager-webhook
kubectl -n slinky rollout status deploy/slurm-operator
kubectl -n jobset-system rollout status deploy/jobset-controller
kubectl -n kueue-system rollout status deploy/kueue-controller-manager
kubectl -n k8s-bridge-system rollout status deploy/k8s-bridge-controller-manager
```

2. Verify the `workloadmixings.k8s-bridge.x-k8s.io` CRD is registered in the cluster:

```bash
kubectl get crd workloadmixings.k8s-bridge.x-k8s.io
```

---

## 5. Handoffs & Related Skills

- **Next step — Configure and apply Kueue CRs, GKE `ComputeClass` CRs, the Slinky `slurm` cluster (`slurm-values.yaml` + Lua plugins), and `WorkloadMixing` CRs (CPU, GPU, priority):** Read [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md).
- **To create or update GKE clusters and autoscaling CPU (CCC) or GPU (DWS Flex-start) node pools:** Read [skills/infra-provision-cluster/SKILL.md](../infra-provision-cluster/SKILL.md).
- **To run the automated Kind CPU or GKE GPU E2E test suites:** Read [skills/test-e2e-cluster/SKILL.md](../test-e2e-cluster/SKILL.md).
