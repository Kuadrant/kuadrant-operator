#!/usr/bin/env bash
controller-gen crd paths="$env/api/v1alpha1;$env/api/v1beta1;$env/api/v1" output:crd:artifacts:config=$env/config/crd/bases
controller-gen rbac:roleName=manager-role webhook paths="$env/..."
