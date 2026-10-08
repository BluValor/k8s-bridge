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

echo "=== GKE GPU Cluster Cleanup Configuration ==="
echo "  Project:  ${GCP_PROJECT}"
echo "  Zone:     ${GCP_ZONE}"
echo "  Cluster:  ${GPU_CLUSTER_NAME}"
echo "============================================="

# Check if cluster exists before attempting deletion
if ! gcloud container clusters describe "${GPU_CLUSTER_NAME}" --zone="${GCP_ZONE}" --project="${GCP_PROJECT}" >/dev/null 2>&1; then
  echo "Cluster '${GPU_CLUSTER_NAME}' does not exist in zone '${GCP_ZONE}' (project: ${GCP_PROJECT}). Nothing to clean up."
  exit 0
fi

echo "Deleting GKE cluster '${GPU_CLUSTER_NAME}' in zone '${GCP_ZONE}' (project: ${GCP_PROJECT})..."
gcloud container clusters delete "${GPU_CLUSTER_NAME}" \
  --zone="${GCP_ZONE}" \
  --project="${GCP_PROJECT}" \
  --quiet

echo "GKE cluster '${GPU_CLUSTER_NAME}' deleted successfully."
