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

# Vendors the pinned cert-manager, JobSet, and Kueue release manifests into
# thirdPartyManifestDir based on the version constants in test/utils/utils.go.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UTILS_GO="${REPO_ROOT}/test/utils/utils.go"

extract_const() {
  local name="$1"
  local val
  val="$(sed -nE "s/^[[:space:]]*${name}[[:space:]]*=[[:space:]]*\"([^\"]+)\".*/\1/p" "${UTILS_GO}")"
  if [[ -z "${val}" ]]; then
    echo "Error: failed to parse ${name} from ${UTILS_GO}" >&2
    exit 1
  fi
  printf '%s\n' "${val}"
}

CERTMANAGER_VERSION="$(extract_const certmanagerVersion)"
JOBSET_VERSION="$(extract_const jobsetVersion)"
KUEUE_VERSION="$(extract_const kueueVersion)"
MANIFEST_DIR="${REPO_ROOT}/$(extract_const thirdPartyManifestDir)"

mkdir -p "${MANIFEST_DIR}"

vendor_manifest() {
  local name="$1"
  local version="$2"
  local url="$3"
  local dest="${MANIFEST_DIR}/${name}-${version}.yaml"
  local tmp
  tmp="$(mktemp)"
  trap 'rm -f "${tmp}"' RETURN

  echo "Downloading ${name} ${version} from ${url}..."
  curl -fsSL "${url}" | sed 's/[[:space:]]*$//' > "${tmp}"
  rm -f "${MANIFEST_DIR}/${name}-"*.yaml
  mv "${tmp}" "${dest}"
  trap - RETURN
  echo "Vendored ${dest#${REPO_ROOT}/}"
}

vendor_manifest "cert-manager" "${CERTMANAGER_VERSION}" \
  "https://github.com/cert-manager/cert-manager/releases/download/${CERTMANAGER_VERSION}/cert-manager.yaml"

vendor_manifest "jobset" "${JOBSET_VERSION}" \
  "https://github.com/kubernetes-sigs/jobset/releases/download/${JOBSET_VERSION}/manifests.yaml"

vendor_manifest "kueue" "${KUEUE_VERSION}" \
  "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
