#!/usr/bin/env bash

# Set strict error handling
set -euo pipefail

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

# Configuration
ROOT="${ROOT:-$(pwd)}"
grep=""
dry_run="0"

# Parse arguments
while [[ $# -gt 0 ]]; do
    echo "ARG: \"$1\""
    case "$1" in
        "--dry_run") dry_run="1"; shift ;;
        *) grep="$1"; shift; break ;;
    esac
done

# Logging function
log() {
    if [[ $dry_run == "1" ]]; then
        echo "[DRY_RUN]: $1"
    else
        echo "$1"
    fi
}

# Task runner function
run_tasks() {
    local dir_name="$1"
    local tasks=()

    log "RUNNING: $dir_name"

    tasks=($(find "$script_dir/$dir_name" -mindepth 1 -maxdepth 1 -perm -001 | sort))

    for task in "${tasks[@]}"; do
        if [[ -n "$grep" && ! "$task" =~ "$grep" ]]; then
            log "grep '$grep' filtered out $task"
            continue
        fi

        log "running script: $task"

        if [[ $dry_run == "0" ]]; then
            env=$ROOT "$task"
            local retVal=$?

            if [[ $retVal -ne 0 ]]; then
                exit $retVal
            fi
        fi
    done
}

# Main execution
log "RUN: root: $ROOT -- grep: $grep"
log "$script_dir"

# Run all phases
phases=("pre-validation" "dependencies" "operator")
for phase in "${phases[@]}"; do
    run_tasks "$phase"
done
