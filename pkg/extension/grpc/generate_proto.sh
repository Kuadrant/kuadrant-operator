#!/usr/bin/env bash


if [ ! $# -eq 1 ]; then
  echo "Provide version folder"
  exit 1
fi

if ! command -v protoc 2>&1 >/dev/null
then
    echo "\"protoc\" could not be found; consider getting it https://protobuf.dev/installation/"
    echo "and don't forget protocol compiler plugins for Go"
    echo "go install google.golang.org/protobuf/cmd/protoc-gen-go@latest"
    exit 1
fi

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
protoc -I=$SCRIPT_DIR -I=$SCRIPT_DIR/$1 --go_out=$SCRIPT_DIR $SCRIPT_DIR/$1/common.proto
protoc -I=$SCRIPT_DIR -I=$SCRIPT_DIR/$1 --go_out=$SCRIPT_DIR --go-grpc_out=$SCRIPT_DIR $SCRIPT_DIR/$1/kuadrant.proto
protoc -I=$SCRIPT_DIR -I=$SCRIPT_DIR/$1 --go_out=$SCRIPT_DIR $SCRIPT_DIR/$1/gateway_api.proto
protoc -I=$SCRIPT_DIR -I=$SCRIPT_DIR/$1 --go_out=$SCRIPT_DIR $SCRIPT_DIR/$1/policy.proto
