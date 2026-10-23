---
name: dev-slurm-lua
description: >-
  Implements, modifies, and tests the Slurm Lua plugins (hack/job_submit.lua and
  hack/cli_filter.lua) that validate CLI options, place jobs on bridged
  partitions into auto-hold, and protect k8s-bridge job constraints. Use when
  adding allowed Slurm CLI flags, changing job submission or modification
  interception rules in Lua, or testing plugin behavior with non-root users.
  Don't use for Go controller or JobSet translator changes (use
  dev-controller-and-translator) or cluster partition mapping configuration (use
  infra-configure-crs).
---

# Slurm Lua Plugins Workflow (`dev-slurm-lua`)

## 1. Preconditions & Inputs

Before modifying or testing the Slurm Lua scripts, verify:
- **Plugin files in repository:**
  - `hack/cli_filter.lua`: Client-side Lua plugin loaded by `sbatch`, `srun`, and `salloc` (`slurm_cli_pre_submit`) on login pods. Reads `CliFilterParameters=workload_mixing_partitions=<csv>` and `Default=YES` partition from `/etc/slurm/slurm.conf` (or `$SLURM_CONF`).
  - `hack/job_submit.lua`: Server-side Lua plugin loaded by `slurmctld` (`slurm_job_submit` and `slurm_job_modify`). Reads the `WORKLOAD_MIXING_PARTITIONS` environment variable on `slurmctld`.
- **Related E2E test suites:**
  - `test/e2e/slurm_flag_rejection_test.go` (tests allowed and disallowed CLI flags)
  - `test/e2e/partition_priority_mapping_test.go` and `test/e2e/e2e_test.go` (tests auto-hold, `--hold` user release takeover, and multi-partition rejection)
- **Optional offline syntax checker:** `luac -p` (if `luac` is installed locally).

---

## 2. Guardrails

- **Test `job_submit.lua` modification rules as a non-root user:** `kubectl exec` into `deploy/slurm-login-slinky` runs as `root` (UID 0). In `hack/job_submit.lua`, `slurm_job_modify` bypasses validation when `modify_uid == 0` (`root`), `modify_uid == slurm_uid` (`slurm`), or `modify_uid ~= job_ptr.user_id`. Run tests via `runuser -u <nonroot-user> -- ...`.
- **Preserve the `"k8s-bridge-auto-hold"` marker string:** `SlurmJobReconciler` (`AutoHoldMarker` in `internal/controller/slurmjob/translator.go` and `isJobExplicitlyHeld` in `internal/controller/slurmjob/slurmjob_controller.go`) distinguishes `job_submit.lua` auto-holds from user `--hold` submissions by checking whether `job.Comment` contains `"k8s-bridge-auto-hold"`. Do not rename this string without updating `translator.go` in lockstep.
- **Normalize CLI option keys in `cli_filter.lua`:** `slurm.json_cli_options(options)` normalizes short flags (such as `-c` or `-N`) to long option names (`"cpus-per-task"`, `"nodes"`). Add long option names without leading dashes to `ALLOWED_OPTIONS`.
- **Keep error message prefixes consistent:**
  - `cli_filter.lua` rejections must log `Validation error: job uses an option that is not supported by k8s-bridge (--<opt>)` and return `slurm.ERROR`.
  - `job_submit.lua` rejections must return `slurm.ERROR`.

---

## 3. Step-by-Step Procedure

### Step 1: Add or Modify Allowed CLI Flags (`hack/cli_filter.lua`)
1. Open `hack/cli_filter.lua` and locate the `ALLOWED_OPTIONS` table.
2. Add or remove the normalized long option key (for example, `["cpus-per-task"] = true`).
   - Note: Options prefixed with `spank:` and `argv` are permitted by default.
   - Heterogeneous job submissions (`pack_offset > 0` or `options["het-group"]`) must remain rejected on workload-mixing partitions.
3. Update `test/e2e/slurm_flag_rejection_test.go` and the inline `configFiles.cli_filter.lua` in `docs/examples/*/slurm-values.yaml` whenever adding or removing supported flags so the E2E flag matrix and example Helm values stay synchronized.

### Step 2: Modify Submission or Modification Interception (`hack/job_submit.lua`)
1. Open `hack/job_submit.lua`.
2. In `slurm_job_submit(job_desc, part_list, submit_uid)`:
   - Resolve target partitions from `job_desc.partition` or the cluster default partition (`part.flag_default ~= 0`).
   - If `#target_parts > 1` and any partition is in `WORKLOAD_MIXING_PARTITIONS`, reject with `slurm.ERROR`.
   - If `#target_parts == 1` and it is in `WORKLOAD_MIXING_PARTITIONS`:
     - When `job_desc.priority ~= 0`, set `job_desc.priority = 0` and append `"k8s-bridge-auto-hold"` to `job_desc.comment`.
     - When `job_desc.priority == 0` (user passed `--hold`), leave `priority = 0` without adding `"k8s-bridge-auto-hold"`.
3. In `slurm_job_modify(job_desc, job_ptr, part_list, modify_uid)`:
   - Allow `root` (`0`), `slurm_uid`, and admin users (`modify_uid ~= job_ptr.user_id`).
   - For the job owner on a bridged partition: if the job is user-held without `"k8s-bridge-auto-hold"` and the user runs `scontrol release` (`job_desc.priority ~= 0`), reset `job_desc.priority = 0` and append `"k8s-bridge-auto-hold"` so `k8s-bridge` takes over. Reject all other user modifications.

### Step 3: Update Lua Scripts in an Active Cluster
Select the option matching how Slurm was deployed on the cluster:
- **Option A — Helm-Deployed Slinky Slurm Cluster (`slurm` release in `slurm` namespace):**
  After `helm upgrade`, wait for kubelet volume propagation (~60s, or until `sha256sum /etc/slurm/cli_filter.lua` in `slurmctld` matches `hack/cli_filter.lua`) before running `scontrol reconfigure` (so `slurmctld` pushes updated scripts to `sackd` on `deploy/slurm-login-slinky`):

```bash
helm upgrade slurm oci://ghcr.io/slinkyproject/charts/slurm \
  --version 1.2.2 --namespace slurm --reuse-values \
  --set-file 'configFiles.job_submit\.lua=hack/job_submit.lua' \
  --set-file 'configFiles.cli_filter\.lua=hack/cli_filter.lua' \
  --wait
kubectl -n slurm exec slurm-controller-0 -c slurmctld -- scontrol reconfigure
```

- **Option B — Kind E2E Cluster (`test/e2e/testdata/slurm_cluster.yaml`):**
  Update the `ConfigMap` objects and wait for kubelet volume propagation (~60s) into `slurm-controller-0` and `deploy/slurm-login` before running `scontrol reconfigure`:

```bash
kubectl -n slurm create configmap slurm-job-submit-lua --from-file=job_submit.lua=hack/job_submit.lua --dry-run=client -o yaml | kubectl apply -f -
kubectl -n slurm create configmap slurm-cli-filter-lua --from-file=cli_filter.lua=hack/cli_filter.lua --dry-run=client -o yaml | kubectl apply -f -
kubectl -n slurm exec slurm-controller-0 -c slurmctld -- scontrol reconfigure
```

---

## 4. Verification

1. **Syntax check (if `luac` is installed):**

```bash
luac -p hack/cli_filter.lua hack/job_submit.lua
```

2. **Non-root verification on a running cluster:**
   Run `bash skills/dev-slurm-lua/scripts/test-nonroot-submission.sh` (or `LOGIN_DEPLOY=deploy/slurm-login PARTITION=workloadmixing-partition TEST_USER=foo bash skills/dev-slurm-lua/scripts/test-nonroot-submission.sh` on a Kind E2E cluster; see [scripts/test-nonroot-submission.sh](scripts/test-nonroot-submission.sh)) to verify:
   - Disallowed CLI flags (`--exclusive`, `--nodelist`) fail client-side with `Validation error: job uses an option that is not supported by k8s-bridge`.
   - Multi-partition submissions including a bridged partition are rejected by `slurmctld`.
   - User `scontrol update` modifications on an auto-held job are rejected for a non-root user.
   - Note on `scontrol release`: When releasing a user-held job on a dynamic partition with 0 registered nodes, `scontrol release <jobID>` returns exit code 1 (`ESLURM_REQUESTED_PART_CONFIG_UNAVAILABLE`) even though `job_submit.lua` applied `"k8s-bridge-auto-hold"`. Verify the transition by checking `scontrol show job <jobID>` for `Comment=k8s-bridge-auto-hold`.
3. **Automated E2E verification:**
   Run the flag rejection and partition mapping specs via [skills/test-e2e-cluster/SKILL.md](../test-e2e-cluster/SKILL.md).

---

## 5. Handoffs & Related Skills

- **To run or update the Kind E2E flag rejection tests (`test/e2e/slurm_flag_rejection_test.go`):** Read [skills/test-e2e-cluster/SKILL.md](../test-e2e-cluster/SKILL.md).
- **To modify how `k8s-bridge` translates held jobs or handles holds in Go:** Read [skills/dev-controller-and-translator/SKILL.md](../dev-controller-and-translator/SKILL.md).
- **To configure bridged partition names across `slurm.conf` and `WorkloadMixing`:** Read [skills/infra-configure-crs/SKILL.md](../infra-configure-crs/SKILL.md).
