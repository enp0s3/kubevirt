/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2023 Red Hat, Inc.
 *
 */
package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Operator Image Name Parser", func() {

	var operatorImageParser *OperatorImageParser

	BeforeEach(func() {
		operatorImageParser = &OperatorImageParser{}
	})

	When("Operator image name is set with tag", func() {
		BeforeEach(func() {
			operatorImageParser.Parse(
				"acme.com:8080/mykubevirt/virt-operator:v1.0.0")
		})

		It("return registry", func() {
			Expect(operatorImageParser.GetRegistry()).To(Equal("acme.com:8080"))
		})

		It("return prefix", func() {
			Expect(operatorImageParser.GetPrefix()).To(Equal("mykubevirt"))
		})

		It("return tag", func() {
			Expect(operatorImageParser.GetTag()).To(Equal("v1.0.0"))
		})

		It("return empty digest", func() {
			Expect(operatorImageParser.GetDigest()).To(BeEmpty())
		})
	})

	When("Operator image name is set with digest", func() {
		BeforeEach(func() {
			operatorImageParser.Parse(
				"acme.com:8080/mykubevirt/my-virt-operator@sha256:abcdef")
		})

		It("return registry", func() {
			Expect(operatorImageParser.GetRegistry()).To(Equal("acme.com:8080"))
		})

		It("return prefix", func() {
			Expect(operatorImageParser.GetPrefix()).To(Equal("mykubevirt"))
		})

		It("return empty tag", func() {
			Expect(operatorImageParser.GetTag()).To(BeEmpty())
		})

		It("return digest", func() {
			Expect(operatorImageParser.GetDigest()).To(Equal("sha256:abcdef"))
		})
	})
})
