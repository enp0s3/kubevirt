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
	"regexp"
	"strings"
)

type ImageNameParser interface {
	GetRegistry() string
	GetPrefix() string
	GetTag() string
	GetDigest() string
}

type OperatorImageParser struct {
	registry string
	prefix   string
	tag      string
	digest   string
}

func (o *OperatorImageParser) Parse(image string) {
	const operatorImageRegex = "^(.*)/(.*)virt-operator([@:].*)?$"
	//const operatorImageRegex = "^(.*)/(.*)/virt-operator(.*[@:].*)?$"

	matches := regexp.MustCompile(operatorImageRegex).FindAllStringSubmatch(image, 1)

	if len(matches) == 1 && len(matches[0]) == 4 {
		o.registry = matches[0][1]
		o.prefix = matches[0][2]

		if strings.HasPrefix(matches[0][3], ":") {
			o.tag = strings.TrimPrefix(matches[0][3], ":")
		}

		if strings.HasPrefix(matches[0][3], "@") {
			o.digest = strings.TrimPrefix(matches[0][3], "@")
		}
	}
}

func (o OperatorImageParser) GetRegistry() string {
	return o.registry
}

func (o OperatorImageParser) GetPrefix() string {
	return o.prefix
}

func (o OperatorImageParser) GetTag() string {
	return o.tag
}

func (o OperatorImageParser) GetDigest() string {
	return o.digest
}
