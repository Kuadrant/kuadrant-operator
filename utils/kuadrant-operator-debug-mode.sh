#!/bin/bash

kubectl patch deployment kuadrant-operator-controller-manager -n kuadrant-system  --patch-file=/dev/stdin <<-EOF
---
spec:
  template:
    spec:
      containers:
        - name: manager
          env:
          - name: LOG_LEVEL
            value: debug
          - name: LOG_MODE
            value: development
EOF
