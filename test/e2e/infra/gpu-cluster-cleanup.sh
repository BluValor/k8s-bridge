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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/common.sh"

INFRA_PROVIDER="${INFRA_PROVIDER:-gke}"
SCRIPT_NAME="${INFRA_PROVIDER}-gpu-cluster-cleanup.sh"
PROVIDER_SCRIPT="${SCRIPT_DIR}/${SCRIPT_NAME}"

if [[ ! -f "${PROVIDER_SCRIPT}" ]]; then
  echo "Error: Infrastructure provider cleanup script not found: ${PROVIDER_SCRIPT}" >&2
  echo "Please ensure ${SCRIPT_NAME} exists under test/e2e/infra/ or set INFRA_PROVIDER to a supported provider." >&2
  exit 1
fi

exec "${PROVIDER_SCRIPT}" "$@"
