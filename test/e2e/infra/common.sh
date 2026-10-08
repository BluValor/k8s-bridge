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
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# Load .env without overriding variables already set in the environment
load_env() {
  local env_file="$1"
  if [[ -f "${env_file}" ]]; then
    while IFS='=' read -r key value || [[ -n "${key}" ]]; do
      key="$(echo "${key}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
      [[ -z "${key}" || "${key}" =~ ^# ]] && continue
      # Only export if not already set in the environment
      if [[ -z "${!key+x}" ]]; then
        value="$(echo "${value}" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
        # Strip surrounding quotes if present
        value="${value%\"}"
        value="${value#\"}"
        value="${value%\'}"
        value="${value#\'}"
        export "${key}=${value}"
      fi
    done < "${env_file}"
  fi
}

# Load local .env first for local overrides, then fallback to repo root .env
if [[ -f ".env" ]]; then
  load_env ".env"
fi

if [[ -f "${REPO_ROOT}/.env" ]]; then
  load_env "${REPO_ROOT}/.env"
fi
