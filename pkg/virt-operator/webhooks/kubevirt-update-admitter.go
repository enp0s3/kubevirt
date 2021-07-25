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
 * Copyright 2018 Red Hat, Inc.
 *
 */

package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"

	"kubevirt.io/kubevirt/pkg/virt-operator/resource/generate/components"
	"kubevirt.io/kubevirt/pkg/virt-operator/util"

	admissionv1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "kubevirt.io/client-go/api/v1"
	"kubevirt.io/client-go/kubecli"
	webhookutils "kubevirt.io/kubevirt/pkg/util/webhooks"
	validating_webhooks "kubevirt.io/kubevirt/pkg/util/webhooks/validating-webhooks"
	"kubevirt.io/kubevirt/pkg/virt-operator/resource/apply"
)

// KubeVirtUpdateAdmitter validates KubeVirt updates
type KubeVirtUpdateAdmitter struct {
	Client kubecli.KubevirtClient
}

// NewKubeVirtUpdateAdmitter creates a KubeVirtUpdateAdmitter
func NewKubeVirtUpdateAdmitter(client kubecli.KubevirtClient) *KubeVirtUpdateAdmitter {
	return &KubeVirtUpdateAdmitter{
		Client: client,
	}
}

func (admitter *KubeVirtUpdateAdmitter) Admit(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	// Get new and old KubeVirt from admission response
	newKV, currKV, err := getAdmissionReviewKubeVirt(ar)
	if err != nil {
		return webhookutils.ToAdmissionResponseError(err)
	}

	if resp := webhookutils.ValidateSchema(v1.KubeVirtGroupVersionKind, ar.Request.Object.Raw); resp != nil {
		return resp
	}

	var results []metav1.StatusCause

	results = append(results, validateCustomizeComponents(newKV.Spec.CustomizeComponents)...)
	results = append(results, validateCertificates(newKV.Spec.CertificateRotationStrategy.SelfSigned)...)

	if !reflect.DeepEqual(currKV.Spec.Infra, newKV.Spec.Infra) {
		if newKV.Spec.Infra != nil && newKV.Spec.Infra.NodePlacement != nil {
			results = append(results,
				validateInfraPlacement(newKV.Namespace, newKV.Spec.Infra.NodePlacement, admitter.Client)...)
		}
	}

	if !reflect.DeepEqual(currKV.Spec.Workloads, newKV.Spec.Workloads) {
		if newKV.Spec.Workloads != nil && newKV.Spec.Workloads.NodePlacement != nil {
			results = append(results,
				validateWorkloadPlacement(newKV.Namespace, newKV.Spec.Workloads.NodePlacement, admitter.Client)...)
		}
	}

	return validating_webhooks.NewAdmissionResponse(results)
}

func getAdmissionReviewKubeVirt(ar *admissionv1.AdmissionReview) (new *v1.KubeVirt, old *v1.KubeVirt, err error) {
	if !webhookutils.ValidateRequestResource(ar.Request.Resource, KubeVirtGroupVersionResource.Group, KubeVirtGroupVersionResource.Resource) {
		return nil, nil, fmt.Errorf("expect resource to be '%s'", KubeVirtGroupVersionResource)
	}

	raw := ar.Request.Object.Raw
	newKV := v1.KubeVirt{}

	err = json.Unmarshal(raw, &newKV)
	if err != nil {
		return nil, nil, err
	}

	if ar.Request.Operation == admissionv1.Update {
		raw := ar.Request.OldObject.Raw
		oldKV := v1.KubeVirt{}
		err = json.Unmarshal(raw, &oldKV)
		if err != nil {
			return nil, nil, err
		}

		return &newKV, &oldKV, nil
	}

	return &newKV, nil, nil
}

func validateCustomizeComponents(customization v1.CustomizeComponents) []metav1.StatusCause {
	patches := customization.Patches
	statuses := []metav1.StatusCause{}

	for _, patch := range patches {
		if json.Valid([]byte(patch.Patch)) {
			continue
		}

		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueNotSupported,
			Message: fmt.Sprintf("patch %q is not valid JSON", patch.Patch),
		})
	}

	return statuses
}

func validateCertificates(certConfig *v1.KubeVirtSelfSignConfiguration) []metav1.StatusCause {
	statuses := []metav1.StatusCause{}

	if certConfig == nil {
		return statuses
	}

	deprecatedApi := false
	if certConfig.CARotateInterval != nil || certConfig.CertRotateInterval != nil || certConfig.CAOverlapInterval != nil {
		deprecatedApi = true
	}

	currentApi := false
	if certConfig.CA != nil || certConfig.Server != nil {
		currentApi = true
	}

	if deprecatedApi && currentApi {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueNotSupported,
			Message: fmt.Sprintf("caRotateInterval, certRotateInterval and caOverlapInterval are deprecated and conflict with CertConfig defined rotation parameters"),
		})
	}

	caDuration := apply.GetCADuration(certConfig)
	caRenewBefore := apply.GetCARenewBefore(certConfig)
	certDuration := apply.GetCertDuration(certConfig)
	certRenewBefore := apply.GetCertRenewBefore(certConfig)

	if caDuration.Duration < caRenewBefore.Duration {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: fmt.Sprintf("CA RenewBefore cannot exceed Duration (spec.certificateRotationStrategy.selfSigned.ca.duration < spec.certificateRotationStrategy.selfSigned.ca.renewBefore)"),
		})

	}

	if certDuration.Duration < certRenewBefore.Duration {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: fmt.Sprintf("Cert RenewBefore cannot exceed Duration (spec.certificateRotationStrategy.selfSigned.server.duration < spec.certificateRotationStrategy.selfSigned.server.renewBefore)"),
		})
	}

	if certDuration.Duration > caDuration.Duration {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: fmt.Sprintf("Certificate duration cannot exceed CA (spec.certificateRotationStrategy.selfSigned.server.duration > spec.certificateRotationStrategy.selfSigned.ca.duration)"),
		})
	}

	return statuses
}

func validateWorkloadPlacement(namespace string, placementConfig *v1.NodePlacement, client kubecli.KubevirtClient) []metav1.StatusCause {
	statuses := []metav1.StatusCause{}

	config, err := util.GetConfigFromEnv()
	if err != nil {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeUnexpectedServerResponse,
			Message: err.Error(),
		})
		return statuses
	}

	handler, err := components.NewHandlerDaemonSet(
		config.GetNamespace(),
		config.GetImageRegistry(),
		config.GetImagePrefix(),
		config.GetHandlerVersion(),
		config.GetLauncherVersion(),
		"productName",
		"productVersion",
		config.GetImagePullPolicy(),
		config.GetVerbosity(),
		config.GetExtraEnv(),
	)

	handler.Name = "test-handler"
	handler.Spec.Template.Spec.NodeSelector = placementConfig.NodeSelector
	handler.Spec.Template.Spec.Affinity = placementConfig.Affinity
	handler.Spec.Template.Spec.Tolerations = placementConfig.Tolerations

	_, err = client.
		AppsV1().
		DaemonSets(namespace).
		Create(context.Background(),
			handler,
			metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})

	if err != nil {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: err.Error(),
		})
	}
	return statuses
}

func validateInfraPlacement(namespace string, placementConfig *v1.NodePlacement, client kubecli.KubevirtClient) []metav1.StatusCause {
	statuses := []metav1.StatusCause{}

	config, err := util.GetConfigFromEnv()
	if err != nil {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeUnexpectedServerResponse,
			Message: err.Error(),
		})
		return statuses
	}

	deployment, err := components.NewControllerDeployment(
		config.GetNamespace(),
		config.GetImageRegistry(),
		config.GetImagePrefix(),
		config.GetControllerVersion(),
		config.GetLauncherVersion(),
		"productName",
		"productVersion",
		config.GetImagePullPolicy(),
		config.GetVerbosity(),
		config.GetExtraEnv(),
	)

	if err != nil {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeUnexpectedServerResponse,
			Message: err.Error(),
		})
		return statuses
	}

	deployment.Name = "test-deployment"
	deployment.Spec.Template.Spec.NodeSelector = placementConfig.NodeSelector
	deployment.Spec.Template.Spec.Affinity = placementConfig.Affinity
	deployment.Spec.Template.Spec.Tolerations = placementConfig.Tolerations

	_, err = client.
		AppsV1().
		Deployments(namespace).
		Create(context.Background(),
			deployment,
			metav1.CreateOptions{DryRun: []string{metav1.DryRunAll}})

	if err != nil {
		statuses = append(statuses, metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: err.Error(),
		})
	}

	return statuses
}
