-- Copyright 2026 Google LLC
--
-- Licensed under the Apache License, Version 2.0 (the "License");
-- you may not use this file except in compliance with the License.
-- You may obtain a copy of the License at
--
--     http://www.apache.org/licenses/LICENSE-2.0
--
-- Unless required by applicable law or agreed to in writing, software
-- distributed under the License is distributed on an "AS IS" BASIS,
-- WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
-- See the License for the specific language governing permissions and
-- limitations under the License.

-- job_submit.lua - Slurm controller plugin script for workload mixing partition management

-- Helper: Parse comma-separated string into a lookup table of partition names
local function parse_wm_partitions()
    local env_val = os.getenv("WORKLOAD_MIXING_PARTITIONS")
    if not env_val or env_val:match("^%s*$") then
        slurm.log_info("WORKLOAD_MIXING_PARTITIONS is not set, but job_submit script is configured to use it.")
        return nil
    end

    local wm_map = {}
    for part in string.gmatch(env_val, "[^,]+") do
        part = part:match("^%s*(.-)%s*$")
        if part ~= "" then
            wm_map[part] = true
        end
    end

    return wm_map
end

-- Parse WORKLOAD_MIXING_PARTITIONS once at script load time outside functions' scope
local wm_map = parse_wm_partitions()

-- Helper: Get the UID of the 'slurm' user from /etc/passwd
local function get_slurm_uid()
    local f = io.open("/etc/passwd", "r")
    if not f then
        return nil
    end
    for line in f:lines() do
        local name, _, uid = line:match("([^:]+):([^:]+):([^:]+)")
        if name == "slurm" then
            f:close()
            return tonumber(uid)
        end
    end
    f:close()
    return nil
end

local slurm_uid = get_slurm_uid()

-- Helper: Parse comma-separated target partitions string into an array
local function parse_target_partitions(partition_str)
    if not partition_str or partition_str:match("^%s*$") then
        return {}
    end

    local list = {}
    for part in string.gmatch(partition_str, "[^,]+") do
        part = part:match("^%s*(.-)%s*$")
        if part ~= "" then
            table.insert(list, part)
        end
    end

    return list
end

-- Helper: Apply k8s-bridge-auto-hold comment and force priority 0
local function apply_k8s_bridge_hold(job_desc, current_comment)
    current_comment = current_comment or ""
    if not current_comment:find("k8s%-bridge%-auto%-hold") then
        if current_comment ~= "" then
            job_desc.comment = current_comment .. ", k8s-bridge-auto-hold"
        else
            job_desc.comment = "k8s-bridge-auto-hold"
        end
    end
    job_desc.priority = 0
end

-- Hook: slurm_job_submit
-- Executed when a new Slurm job is submitted
function slurm_job_submit(job_desc, part_list, submit_uid)
    if not wm_map then
        return slurm.SUCCESS
    end

    local partition_str = job_desc and job_desc.partition
    local target_parts = parse_target_partitions(partition_str)

    -- If no partition was explicitly specified, fallback to the default partition
    if #target_parts == 0 and part_list then
        for _, part in pairs(part_list) do
            if part.flag_default ~= 0 and part.name then
                table.insert(target_parts, part.name)
                break
            end
        end
    end

    local matching_count = 0
    for _, part in ipairs(target_parts) do
        if wm_map[part] then
            matching_count = matching_count + 1
        end
    end

    -- Multi-partition request containing workload mixing partition MUST be rejected
    if #target_parts > 1 and matching_count > 0 then
        slurm.log_error(string.format("slurm_job_submit: Job submission rejected. Multi-partition request '%s' contains workload mixing partition.", partition_str or ""))
        return slurm.ERROR
    -- Single partition matching workload mixing -> Hold job (priority = 0)
    elseif #target_parts == 1 and matching_count == 1 then
        if job_desc.priority ~= 0 then
            apply_k8s_bridge_hold(job_desc, job_desc.comment)
        end
        slurm.log_info(string.format("slurm_job_submit: Job held (priority=0) for workload mixing partition '%s'", target_parts[1]))
        return slurm.SUCCESS
    end

    -- Partition(s) not in workload mixing -> normal submission
    return slurm.SUCCESS
end

-- Hook: slurm_job_modify
-- Executed when an existing Slurm job modification is requested
function slurm_job_modify(job_desc, job_ptr, part_list, modify_uid)
    if not wm_map then
        return slurm.SUCCESS
    end

    -- Only enforce immutability/restrictions on regular user modifications
    -- (Bypass if the modifier is root (0), the slurm user, or if it's an administrative update where modify_uid != job owner)
    local isRootUser = (modify_uid == 0)
    local isSlurmUser = (slurm_uid ~= nil and modify_uid == slurm_uid)
    local isAdministrativeUpdate = (job_ptr == nil or modify_uid ~= job_ptr.user_id)

    if not (isRootUser or isSlurmUser or isAdministrativeUpdate) then
        -- 1. If job is currently on a workload mixing partition, reject modifications
        local current_parts = parse_target_partitions(job_ptr.partition)
        for _, part in ipairs(current_parts) do
            if wm_map[part] then
                -- Intercept explicit unhold: job priority was 0 but is being changed to a non-zero value
                if job_ptr.priority == 0 and job_desc.priority ~= nil and job_desc.priority ~= 0 then
                    local current_comment = job_desc.comment or job_ptr.comment or ""

                    -- If the k8s-bridge-auto-hold comment is NOT there, this is a zombie job (e.g. user-held at submission).
                    -- We intercept the unhold, force it back to being held, and add the comment so the bridge takes over.
                    if not current_comment:find("k8s%-bridge%-auto%-hold") then
                        apply_k8s_bridge_hold(job_desc, current_comment)
                        slurm.log_info(string.format("slurm_job_modify: Intercepted unhold for job on workload mixing partition '%s', transitioned to auto-hold", part))
                        return slurm.SUCCESS
                    end
                    -- If the comment IS already there, it's a bridge-managed job. We fall through to reject the user's unhold.
                end

                slurm.log_error(string.format("slurm_job_modify: Modification rejected for job on workload mixing partition '%s' (immutability rule)", part))
                return slurm.ERROR
            end
        end

        -- 2. If user is trying to move a job onto a workload mixing partition, reject
        local requested_parts = parse_target_partitions(job_desc and job_desc.partition)
        for _, part in ipairs(requested_parts) do
            if wm_map[part] then
                slurm.log_error(string.format("slurm_job_modify: Modification rejected. Cannot move job to workload mixing partition '%s'", part))
                return slurm.ERROR
            end
        end
    end

    return slurm.SUCCESS
end
