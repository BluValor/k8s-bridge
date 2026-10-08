#!/usr/bin/env bash
# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -euo pipefail

if ! command -v gcloud >/dev/null 2>&1; then
  echo "Error: 'gcloud' CLI is required but not installed or not in PATH." >&2
  exit 1
fi

GCP_PROJECT="${GCP_PROJECT:-$(gcloud config get-value project 2>/dev/null || true)}"
if [[ -z "${GCP_PROJECT}" ]]; then
  echo "Error: GCP_PROJECT is not set and no default project is configured in gcloud." >&2
  echo "Please set GCP_PROJECT in .env or via environment variable." >&2
  exit 1
fi

GCP_ZONE="${GCP_ZONE:-us-central1-a}"
GPU_CLUSTER_NAME="${GPU_CLUSTER_NAME:-k8s-bridge-gpu-e2e}"
SYSTEM_MACHINE_TYPE="${SYSTEM_MACHINE_TYPE:-e2-standard-4}"
SYSTEM_NODE_COUNT="${SYSTEM_NODE_COUNT:-1}"
GPU_NODE_POOL_NAME="${GPU_NODE_POOL_NAME:-gpu-pool}"
GPU_NODE_COUNT="${GPU_NODE_COUNT:-1}"
GPU_ACCELERATOR_TYPE="${GPU_ACCELERATOR_TYPE:-nvidia-l4}"
GPU_ACCELERATOR_COUNT="${GPU_ACCELERATOR_COUNT:-1}"
RESERVATION_NAME="${RESERVATION_NAME:-}"

# Auto-derive default MACHINE_TYPE based on accelerator type if not explicitly configured
if [[ -z "${MACHINE_TYPE:-}" ]]; then
  case "${GPU_ACCELERATOR_TYPE}" in
    nvidia-l4)
      MACHINE_TYPE="g2-standard-4" ;;
    nvidia-tesla-t4)
      MACHINE_TYPE="n1-standard-4" ;;
    nvidia-tesla-a100|nvidia-a100-80gb)
      MACHINE_TYPE="a2-highgpu-1g" ;;
    nvidia-h100-80gb|nvidia-h100-mega-80gb)
      MACHINE_TYPE="a3-highgpu-8g" ;;
    *)
      MACHINE_TYPE="g2-standard-4" ;;
  esac
fi

echo "=== GKE GPU Cluster Setup Configuration ==="
echo "  Project:            ${GCP_PROJECT}"
echo "  Zone:               ${GCP_ZONE}"
echo "  Cluster:            ${GPU_CLUSTER_NAME}"
echo "  System Machine:     ${SYSTEM_MACHINE_TYPE} (nodes: ${SYSTEM_NODE_COUNT})"
echo "  GPU Node Pool:      ${GPU_NODE_POOL_NAME}"
echo "  GPU Machine Type:   ${MACHINE_TYPE}"
echo "  GPU Accelerator:    ${GPU_ACCELERATOR_TYPE} (count: ${GPU_ACCELERATOR_COUNT})"
echo "  GPU Nodes:          ${GPU_NODE_COUNT}"
if [[ -n "${RESERVATION_NAME}" ]]; then
  echo "  Reservation:        ${RESERVATION_NAME}"
fi
echo "==========================================="

# 1. Create base GKE cluster with standard system pool if not already present
if gcloud container clusters describe "${GPU_CLUSTER_NAME}" --zone="${GCP_ZONE}" --project="${GCP_PROJECT}" >/dev/null 2>&1; then
  echo "Cluster '${GPU_CLUSTER_NAME}' already exists in zone '${GCP_ZONE}'."
else
  echo "Creating GKE cluster '${GPU_CLUSTER_NAME}' with system node pool..."
  gcloud container clusters create "${GPU_CLUSTER_NAME}" \
    --zone="${GCP_ZONE}" \
    --project="${GCP_PROJECT}" \
    --machine-type="${SYSTEM_MACHINE_TYPE}" \
    --num-nodes="${SYSTEM_NODE_COUNT}"
fi

# 2. Create separate GPU node pool without installing GPU drivers
if gcloud container node-pools describe "${GPU_NODE_POOL_NAME}" --cluster="${GPU_CLUSTER_NAME}" --zone="${GCP_ZONE}" --project="${GCP_PROJECT}" >/dev/null 2>&1; then
  echo "GPU node pool '${GPU_NODE_POOL_NAME}' already exists in cluster '${GPU_CLUSTER_NAME}'."
else
  echo "Creating separate GPU node pool '${GPU_NODE_POOL_NAME}' in cluster '${GPU_CLUSTER_NAME}'..."
  NODE_POOL_ARGS=(
    "${GPU_NODE_POOL_NAME}"
    "--cluster=${GPU_CLUSTER_NAME}"
    "--zone=${GCP_ZONE}"
    "--project=${GCP_PROJECT}"
    "--machine-type=${MACHINE_TYPE}"
    "--num-nodes=${GPU_NODE_COUNT}"
    "--accelerator=type=${GPU_ACCELERATOR_TYPE},count=${GPU_ACCELERATOR_COUNT}"
  )

  if [[ -n "${RESERVATION_NAME}" ]]; then
    NODE_POOL_ARGS+=(
      "--reservation-affinity=specific"
      "--reservation=${RESERVATION_NAME}"
    )
  fi

  gcloud container node-pools create "${NODE_POOL_ARGS[@]}"
fi

# 3. Configure credentials and verify cluster connectivity
echo "Configuring kubectl credentials for '${GPU_CLUSTER_NAME}'..."
gcloud container clusters get-credentials "${GPU_CLUSTER_NAME}" --zone="${GCP_ZONE}" --project="${GCP_PROJECT}"

if command -v kubectl >/dev/null 2>&1; then
  echo "Verifying cluster nodes..."
  kubectl get nodes -o wide || true
fi

echo "GKE cluster '${GPU_CLUSTER_NAME}' with GPU node pool '${GPU_NODE_POOL_NAME}' is ready."
