#!/usr/bin/env bash
set -e

if [ "${KUBEVIRT_CGROUPV2}" == "false" ]; then
    export KUBEVIRT_PROVIDER_EXTRA_ARGS="${KUBEVIRT_PROVIDER_EXTRA_ARGS} --kernel-args='systemd.unified_cgroup_hierarchy=0'"
fi

# shellcheck disable=SC1090
source "${KUBEVIRTCI_PATH}/cluster/k8s-provider-common.sh"
