# Kubebuilder Validation & RBAC Markers Reference

## 1. CRD Schema & Validation Markers (`api/v1alpha1/workloadmixing_types.go`)

| Marker | Target | Purpose / Example in `k8s-bridge` |
|---|---|---|
| `// +kubebuilder:object:root=true` | Struct (`WorkloadMixing`, `WorkloadMixingList`) | Registers type as a root Kubernetes API object for DeepCopy and CRD generation. |
| `// +kubebuilder:subresource:status` | Struct (`WorkloadMixing`) | Enables the `/status` subresource so `r.Status().Patch(...)` updates `status` without incrementing `metadata.generation`. |
| `// +kubebuilder:printcolumn:name="Available",type="string",JSONPath=".status.conditions[?(@.type=='Available')].status"` | Struct (`WorkloadMixing`) | Optional marker to add custom columns to `kubectl get workloadmixing` output (not currently enabled on `WorkloadMixing`). |
| `// +required` | Struct field | Marks field as required in OpenAPI v3 schema (`controllerRef`, `partition`, `localQueue`, `resourceName`, `k8sResourceName`). |
| `// +optional` | Struct field | Marks field as optional (`partitionsMapping`, `mappedResources`, `podTemplate`, `workloadPriorityClass`, `dynamicNodeFeature`, `resourceType`, `nodeSelectors`). |
| `// +kubebuilder:validation:MaxLength=64` | `string` field | Enforces maximum string length (`DynamicNodeFeature`). |
| `// +kubebuilder:validation:Pattern="^[a-zA-Z0-9_-]+$"` | `string` field | Enforces regex pattern (`DynamicNodeFeature`). |
| `// +kubebuilder:validation:MinItems=1` | Slice field | Requires at least 1 element when the slice is present. |
| `// +kubebuilder:validation:Enum=val1;val2` | `string` field | Restricts field to allowed enum values. |
| `// +listType=map` + `// +listMapKey=type` | `[]metav1.Condition` | Declares associative list merge keys (`WorkloadMixingStatus.Conditions`). |

---

## 2. Controller RBAC Matrix (`config/rbac/role.yaml` & `helm/templates/clusterrole.yaml`)

When adding or changing `// +kubebuilder:rbac` markers in `internal/controller/**`, keep `helm/templates/clusterrole.yaml` synchronized with `config/rbac/role.yaml`:

| API Group (`groups`) | Resource (`resources`) | Verbs (`verbs`) | Source Controller File |
|---|---|---|---|
| `""` (core) | `events` | `create;patch` | `workloadmixing_controller.go` |
| `""` (core) | `secrets` | `get;list;watch` | `workloadmixing_controller.go`, `slurmclient_controller.go` |
| `jobset.x-k8s.io` | `jobsets` | `create;delete;get;list;patch;update;watch` | `slurmjob_controller.go` (full verbs); `workloadmixing_controller.go` (`get;list;watch`) |
| `jobset.x-k8s.io` | `jobsets/finalizers` | `update` | `slurmjob_controller.go` |
| `k8s-bridge.x-k8s.io` | `workloadmixings` | `create;delete;get;list;patch;update;watch` | `workloadmixing_controller.go` (full verbs); `slurmjob_controller.go` (`get;list;watch`) |
| `k8s-bridge.x-k8s.io` | `workloadmixings/finalizers` | `update` | `workloadmixing_controller.go` |
| `k8s-bridge.x-k8s.io` | `workloadmixings/status` | `get;patch;update` | `workloadmixing_controller.go` |
| `slinky.slurm.net` | `controllers` | `get;list;watch` | `workloadmixing_controller.go`, `slurmclient_controller.go` |
| `slinky.slurm.net` | `restapis` | `get;list;watch` | `workloadmixing_controller.go`, `slurmclient_controller.go` |
