#!/bin/bash
#
# This file is part of the KubeVirt project
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
#
# Copyright 2019 Red Hat, Inc.
#

set -e

source hack/common.sh
source hack/bootstrap.sh
source hack/config.sh

PUSH_OTHER_IMAGES=${PUSH_OTHER_IMAGES:-true}

if [ ! -z "$PUSH_TARGETS" ]; then
  PUSH_OTHER_IMAGES=false
fi

PUSH_TARGETS=(${PUSH_TARGETS:-virt-operator virt-api virt-controller virt-handler virt-launcher virt-exportserver virt-exportproxy conformance libguestfs-tools pr-helper})

function push_target() {
    local path=$1
    local target=$2

    for tag in ${docker_tag} ${docker_tag_alt}; do
        bazel run \
            --config=${ARCHITECTURE} \
            --define container_prefix=${docker_prefix} \
            --define image_prefix=${image_prefix} \
            --define container_tag=${tag} \
            //${path}:push-${target}
    done
    if [[ $image_prefix_alt ]]; then
      bazel run \
          --config=${ARCHITECTURE} \
          --define container_prefix=${docker_prefix} \
          --define image_prefix=${image_prefix_alt} \
          --define container_tag=${docker_tag} \
          //${path}:push-${target}
    fi
}

if [ "${PUSH_OTHER_IMAGES}" == "true" ]; then
  container_disk_images=(
    alpine-container-disk-image \
    cirros-container-disk-image \
    cirros-custom-container-disk-image \
    virtio-container-disk-image \
    fedora-with-test-tooling \
    alpine-with-test-tooling \
    alpine-ext-kernel-boot-demo-container \
    fedora-realtime \
  )


  for target in ${container_disk_images[@]} ; do
    push_target "containerimages" $target
  done

fi

for target in ${PUSH_TARGETS[@]}; do
  push_target "" $target
done


# alpine-container-disk-image cirros-container-disk-image cirros-custom-container-disk-image virtio-container-disk-image fedora-with-test-tooling alpine-with-test-tooling alpine-ext-kernel-boot-demo-container
rm -rf ${DIGESTS_DIR}/${ARCHITECTURE}
mkdir -p ${DIGESTS_DIR}/${ARCHITECTURE}

for f in $(find bazel-bin/ -name '*.digest'); do
    dir=${DIGESTS_DIR}/${ARCHITECTURE}/$(dirname $f)
    mkdir -p ${dir}
    cp -f ${f} ${dir}/$(basename ${f})
done
