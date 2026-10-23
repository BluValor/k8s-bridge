#!/usr/bin/env bash
# Non-root verification commands for hack/cli_filter.lua and hack/job_submit.lua
set -euo pipefail

NAMESPACE="${NAMESPACE:-slurm}"
LOGIN_DEPLOY="${LOGIN_DEPLOY:-deploy/slurm-login-slinky}"
PARTITION="${PARTITION:-k8s-partition}"
TEST_USER="${TEST_USER:-someuser}"

# 1. Ensure non-root user exists on the login pod
kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- sh -c \
  "id -u ${TEST_USER} >/dev/null 2>&1 || useradd -m -s /bin/bash ${TEST_USER}"

# 2. Verify cli_filter.lua accepts allowed options (--test-only)
OUT=$(kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  runuser -u "${TEST_USER}" -- \
  sbatch --partition="${PARTITION}" --nodes=1 --cpus-per-task=1 --mem=1G --chdir=/tmp --wrap="hostname" --test-only 2>&1 || true)
if ! grep -q "Validation error: job uses an option that is not supported by k8s-bridge" <<<"${OUT}" && \
   grep -Eq "to start at|Requested partition configuration not available now" <<<"${OUT}"; then
  echo "PASS: cli_filter.lua accepted allowed options (--test-only)"
else
  echo "FAIL: unexpected output from --test-only: ${OUT}" >&2
  exit 1
fi

# 3. Verify cli_filter.lua rejects disallowed options (e.g. --exclusive)
OUT=$(kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  runuser -u "${TEST_USER}" -- \
  sbatch --partition="${PARTITION}" --exclusive --chdir=/tmp --wrap="hostname" 2>&1 || true)
if grep -q "Validation error: job uses an option that is not supported by k8s-bridge" <<<"${OUT}"; then
  echo "PASS: cli_filter.lua rejected --exclusive"
else
  echo "FAIL: cli_filter.lua did not reject --exclusive: ${OUT}" >&2
  exit 1
fi

# 4. Verify job_submit.lua rejects multi-partition requests containing a bridged partition
OUT=$(kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  runuser -u "${TEST_USER}" -- \
  sbatch --partition="${PARTITION},other" --nodes=1 --chdir=/tmp --wrap="hostname" 2>&1 || true)
if grep -Eq "Unspecified error|Multi-partition request|Invalid partition name" <<<"${OUT}" && \
   kubectl -n "${NAMESPACE}" logs slurm-controller-0 -c slurmctld --since=2m | grep -q "Multi-partition request '${PARTITION},other'"; then
  echo "PASS: multi-partition request with ${PARTITION} rejected"
else
  echo "FAIL: multi-partition request with ${PARTITION} was not rejected: ${OUT}" >&2
  exit 1
fi

# 5. Verify job_submit.lua places held job (--hold) without k8s-bridge-auto-hold,
#    converts user release into k8s-bridge-auto-hold, and rejects subsequent user modifications
JOB_ID=$(kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  runuser -u "${TEST_USER}" -- \
  sbatch --parsable --hold --partition="${PARTITION}" --nodes=1 --chdir=/tmp --wrap="sleep 5")

# Note: scontrol release may return exit code 1 (ESLURM_REQUESTED_PART_CONFIG_UNAVAILABLE)
# when 0 dynamic nodes exist; check the job comment afterwards.
kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  runuser -u "${TEST_USER}" -- \
  scontrol release "${JOB_ID}" || true

if kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  scontrol show job "${JOB_ID}" | grep -Eq "Comment=.*(k8s-bridge-auto-hold|Unheld by k8s-bridge)"; then
  echo "PASS: job_submit.lua converted user release to k8s-bridge-auto-hold"
else
  echo "FAIL: job ${JOB_ID} missing k8s-bridge-auto-hold comment after release" >&2
  kubectl -n "${NAMESPACE}" exec slurm-controller-0 -c slurmctld -- scancel "${JOB_ID}" || true
  exit 1
fi

if ! kubectl -n "${NAMESPACE}" exec "${LOGIN_DEPLOY}" -c login -- \
  runuser -u "${TEST_USER}" -- \
  scontrol update JobId="${JOB_ID}" JobName=modified-name >/dev/null 2>&1; then
  echo "PASS: job_submit.lua rejected non-root scontrol update on auto-held job"
else
  echo "FAIL: job_submit.lua allowed non-root scontrol update on auto-held job" >&2
  kubectl -n "${NAMESPACE}" exec slurm-controller-0 -c slurmctld -- scancel "${JOB_ID}" || true
  exit 1
fi

kubectl -n "${NAMESPACE}" exec slurm-controller-0 -c slurmctld -- scancel "${JOB_ID}" || true
