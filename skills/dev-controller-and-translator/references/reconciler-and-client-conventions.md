# Reconciler, `slurm-client`, and Label/Metric Conventions

## 1. `slurm-client` (`V0044JobInfo`) Pointer & `NoValStruct` Rules

1. **Scalar Pointer Fields (`JobId *int32`, `Hold *bool`, `Name *string`, `Partition *string`, `Comment *string`, `Features *string`, `StateReason *string`, `TresPerNode *string`, `TresPerTask *string`, `TresPerJob *string`, `CpusPerTres *string`, `MemoryPerTres *string`):**
   - Read with `ptr.Deref(job.Partition, "")` (`k8s.io/utils/ptr`).
2. **Numeric `NoValStruct` Fields (`NodeCount`, `Tasks`, `TasksPerNode`, `CpusPerTask`, `Priority` of type `*slurmapi.V0044Uint32NoValStruct`; `MemoryPerNode`, `MemoryPerCpu` of type `*slurmapi.V0044Uint64NoValStruct`):**
   - Check `field != nil && ptr.Deref(field.Set, false) && !ptr.Deref(field.Infinite, false)` before reading `*field.Number`.
3. **Job State Set (`JobState *[]slurmapi.V0044JobInfoJobState`):**
   - Query via `job.GetStateAsSet().Has(slurmapi.V0044JobInfoJobStatePENDING)`.

---

## 2. `slurmclient.Client` Call Patterns

```go
// 1. Get a single Slurm job by ID
job := &slurmclienttypes.V0044JobInfo{}
err := slurmClient.Get(ctx, object.ObjectKey(jobID), job)

// 2. List all Slurm jobs
list := &slurmclienttypes.V0044JobInfoList{}
err := slurmClient.List(ctx, list)

// 3. Update Slurm job (unhold and set constraints/comment)
req := slurmapi.V0044JobDescMsg{
	Hold:        ptr.To(false),
	Constraints: &constraints,
	Comment:     &comment,
}
err := slurmClient.Update(ctx, job, req)

// 4. Cancel Slurm job or delete dynamic Slurm node
err := slurmClient.Delete(ctx, job)
err := slurmClient.Delete(ctx, &slurmclienttypes.V0044Node{
	V0044Node: slurmapi.V0044Node{Name: ptr.To(nodeName)},
})
```

---

## 3. Managed Labels & Finalizers (`internal/controller/slurmjob/translator.go`)

| Key / Finalizer | Target | Value / Purpose |
|---|---|---|
| `app.kubernetes.io/name` | `JobSet`, Pod | `"k8s-bridge"` |
| `app.kubernetes.io/managed-by` | `JobSet`, Pod | `"k8s-bridge"` |
| `app.kubernetes.io/component` | `JobSet`, Pod | `"jobset"` on `JobSet`; `"worker"` on Pod |
| `k8s-bridge.x-k8s.io/slurm-job-id` | `JobSet`, Pod | Slurm job ID string (e.g. `"1"`) |
| `k8s-bridge.x-k8s.io/slurm-job-name` | `JobSet`, Pod | Sanitized Slurm job name (`[a-z0-9_.-]`, max 63 chars) |
| `k8s-bridge.x-k8s.io/slurm-partition` | `JobSet` | Slurm partition name |
| `kueue.x-k8s.io/queue-name` | `JobSet` | Mapped `LocalQueue` name from `partitionsMapping` |
| `kueue.x-k8s.io/priority-class` | `JobSet` | Mapped `WorkloadPriorityClass` name (when non-empty) |
| `k8s-bridge.x-k8s.io/slurm-node-cleanup` (`FinalizerJobSet`) | `JobSet` (`metadata.finalizers`) | Retains deleting `JobSet` until `finalizeJobSet` removes its dynamic Slurm nodes. |

---

## 4. Prometheus Metrics (`internal/metrics/metrics.go`)

| Metric Name | Type | Labels | Description |
|---|---|---|---|
| `k8s_bridge_slurm_poller_duration_seconds` | Histogram | `controller` | Slurm REST polling latency in seconds. |
| `k8s_bridge_slurm_poller_errors_total` | Counter | `controller` | Total Slurm REST polling errors. |
| `k8s_bridge_slurm_client_status` | Gauge | `controller` | Slurm REST client status (`1` = healthy, `0` = error). |
| `k8s_bridge_jobs_discovered_total` | Counter | `workload_mixing`, `partition` | Total pending Slurm jobs discovered. |
| `k8s_bridge_jobs_sync_total` | Counter | `status`, `reason`, `workload_mixing`, `partition` | Sync outcomes (`jobset_created`, `translation_error`, `jobset_completed`, `jobset_failed`, `slurm_update_error`, `job_completed`, `job_failed`, `job_cancelled`, `job_orphaned`). |
| `k8s_bridge_job_translation_duration_seconds` | Histogram | `workload_mixing`, `partition` | Latency of translating a Slurm job to a `JobSet`. |
| `k8s_bridge_active_jobs` | Gauge | `workload_mixing`, `partition` | Count of active managed `JobSet` objects. |
| `k8s_bridge_active_workload_mixings` | Gauge | *(none)* | Count of active `WorkloadMixing` CRs. |
| `k8s_bridge_tracked_partitions` | Gauge | `workload_mixing` | Count of partitions in `spec.partitionsMapping`. |
| `k8s_bridge_dropped_events_total` | Counter | `workload_mixing` | Events dropped due to full channel buffers. |
| `k8s_bridge_events_queue_depth` | Gauge | *(none)* | Depth of `admissionEventsChan`. |
