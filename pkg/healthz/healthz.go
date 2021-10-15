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
 * Copyright 2017 Red Hat, Inc.
 *
 */

package healthz

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	restful "github.com/emicklei/go-restful"
	"k8s.io/apimachinery/pkg/util/json"

	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"

	"kubevirt.io/client-go/kubecli"
)

type KubeApiHealthzVersion struct {
	version interface{}
	sync.RWMutex
}

func (h *KubeApiHealthzVersion) Update(body interface{}) {
	h.Lock()
	defer h.Unlock()
	h.version = body
}

func (h *KubeApiHealthzVersion) Clear() {
	h.Lock()
	defer h.Unlock()
	h.version = nil
}

func (h *KubeApiHealthzVersion) GetVersion() (v interface{}) {
	h.RLock()
	defer h.RUnlock()
	v = h.version
	return
}

/*
   This check is primarily to determine whether a controller can reach the Kubernetes API.
   We can reflect this based on other connections we depend on (informers and their error handling),
   rather than testing the kubernetes API every time the healthcheck endpoint is called. This
   should avoid a lot of unnecessary calls to the API while informers are healthy.

   Note that It is possible for the contents of a KubeApiHealthzVersion to be out of date if the
   Kubernetes API version changes without an informer disconnect, or if informer doesn't call
   KubeApiHealthzVersion.Clear() when it encounters an error.
*/

type KubeConnectionHealthzParams struct {
	ClusterConfig *virtconfig.ClusterConfig
	HVersion      *KubeApiHealthzVersion
}

func KubeConnectionHealthzFuncFactory(params *KubeConnectionHealthzParams) restful.RouteFunction {
	return func(_ *restful.Request, response *restful.Response) {
		res := map[string]interface{}{}
		var version = params.HVersion.GetVersion()

		if version == nil {
			cli, err := kubecli.GetKubevirtClient()
			if err != nil {
				unhealthy(err, params.ClusterConfig, response)
				return
			}

			body, err := cli.CoreV1().RESTClient().Get().AbsPath("/version").Do(context.Background()).Raw()
			if err != nil {
				unhealthy(err, params.ClusterConfig, response)
				return
			}

			err = json.Unmarshal(body, &version)
			if err != nil {
				unhealthy(err, params.ClusterConfig, response)
				return
			}

			params.HVersion.Update(version)
		}

		res["apiserver"] = map[string]interface{}{"connectivity": "ok", "version": version}
		res["config-resource-version"] = params.ClusterConfig.GetResourceVersion()
		response.WriteHeaderAndJson(http.StatusOK, res, restful.MIME_JSON)
		return
	}
}

func unhealthy(err error, clusterConfig *virtconfig.ClusterConfig, response *restful.Response) {
	res := map[string]interface{}{}
	res["apiserver"] = map[string]interface{}{"connectivity": "failed", "error": fmt.Sprintf("%v", err)}
	res["config-resource-version"] = clusterConfig.GetResourceVersion()
	response.WriteHeaderAndJson(http.StatusInternalServerError, res, restful.MIME_JSON)
}
