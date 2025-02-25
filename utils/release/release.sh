#!/usr/bin/env bash

script_dir="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

if [[ -z "${ROOT}" ]]; then
	echo "[WARNING] env var ROOT not set, using $(pwd)"
	ROOT=$(pwd)
fi

grep=""
dry_run="0"

while [[ $# -gt 0 ]]; do
	echo "ARG: \"$1\""
	if [[ "$1" == "--dry_run" ]]; then
		dry_run="1"
	else
		grep="$1"
	fi
	shift
done

log() {
	if [[ $dry_run == "1" ]]; then
		echo "[DRY_RUN]: $1"
	else
		echo "$1"
	fi
}

log "RUN: root: $ROOT -- grep: $grep"
log $script_dir

log "RUNNING: pre-validation"
tasks=`find $script_dir/pre-validation -mindepth 1 -maxdepth 1 -perm -001 | sort`
for s in $tasks; do
	if echo "$s" | grep -vq "$grep"; then
		log "gerp \"$grep\" fileterd out $s"
		continue
	fi

	log "running script: $s"
	if [[ $dry_run == "0" ]]; then
		env=$ROOT $s
		retVal=$?
		if [[ $retVal -ne 0 ]]; then
			exit $retVal
		fi
	fi
done
log "RUNNING: dependencies"
tasks=`find $script_dir/dependencies -mindepth 1 -maxdepth 1 -perm -001 | sort`
for s in $tasks; do
	if echo "$s" | grep -vq "$grep"; then
		log "gerp \"$grep\" fileterd out $s"
		continue
	fi

	log "running script: $s"
	if [[ $dry_run == "0" ]]; then
		env=$ROOT $s
		retVal=$?
		if [[ $retVal -ne 0 ]]; then
			exit $retVal
		fi
	fi
done
log "RUNNING: operator"
tasks=`find $script_dir/operator -mindepth 1 -maxdepth 1 -perm -001 | sort`
for s in $tasks; do
	if echo "$s" | grep -vq "$grep"; then
		log "gerp \"$grep\" fileterd out $s"
		continue
	fi

	log "running script: $s"
	if [[ $dry_run == "0" ]]; then
		env=$ROOT $s
		retVal=$?
		if [[ $retVal -ne 0 ]]; then
			exit $retVal
		fi
	fi
done
log "RUNNING: post-validation"
tasks=`find $script_dir/post-validation -mindepth 1 -maxdepth 1 -perm -001 | sort`
for s in $tasks; do
	if echo "$s" | grep -vq "$grep"; then
		log "gerp \"$grep\" fileterd out $s"
		continue
	fi

	log "running script: $s"
	if [[ $dry_run == "0" ]]; then
		env=$ROOT $s
		retVal=$?
		if [[ $retVal -ne 0 ]]; then
			exit $retVal
		fi
	fi
done
