package admitters

import (
	"fmt"
	"k8s.io/client-go/tools/cache"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	virtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	validating_webhooks "kubevirt.io/kubevirt/pkg/util/webhooks/validating-webhooks"
	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"
)

type PodEvictionAdmitter struct {
	VMIInformer   cache.SharedIndexInformer
	PodInformer   cache.SharedIndexInformer
	ClusterConfig *virtconfig.ClusterConfig
	VirtClient    kubecli.KubevirtClient
}

func (admitter *PodEvictionAdmitter) Admit(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	if !admitter.ClusterConfig.LiveMigrationEnabled() {
		return validating_webhooks.NewPassingAdmissionResponse()
	}

	key := fmt.Sprintf("%v/%v", ar.Request.Namespace, ar.Request.Name)
	obj, exists, err := admitter.PodInformer.GetStore().GetByKey(key)
	if err != nil {
		return denied(fmt.Sprintf("kubevirt failed getting the virt-launcher pod: %s", err.Error()))
	} else if !exists {
		return validating_webhooks.NewPassingAdmissionResponse()
	}

	launcher := obj.(*corev1.Pod)
	domainName, exists := launcher.GetAnnotations()[virtv1.DomainAnnotation]
	if !exists {
		return validating_webhooks.NewPassingAdmissionResponse()
	}

	key = fmt.Sprintf("%v/%v", ar.Request.Namespace, domainName)
	obj, exists, err = admitter.VMIInformer.GetStore().GetByKey(key)
	if err != nil {
		return denied(fmt.Sprintf("kubevirt failed getting the vmi: %s", err.Error()))
	} else if !exists {
		return denied(fmt.Sprintf("VMI %s corresponding to the virt-launcher pod %s not found", key, launcher.Name))
	}

	vmi := obj.(*virtv1.VirtualMachineInstance)
	if !vmi.IsEvictable() {
		// we don't act on VMIs without an eviction strategy
		return validating_webhooks.NewPassingAdmissionResponse()
	} else if !vmi.IsMigratable() {
		return denied(fmt.Sprintf(
			"VMI %s is configured with an eviction strategy but is not live-migratable", vmi.Name))
	}

	if !vmi.IsMarkedForEviction() &&
		vmi.Status.NodeName == launcher.Spec.NodeName {
			dryRun := ar.Request.DryRun != nil && *ar.Request.DryRun == true

			err := admitter.markVMI(ar, vmi, dryRun)
			if err != nil {
				return denied(fmt.Sprintf("kubevirt failed marking the vmi for eviction: %s", err.Error()))
			}
	}

	// We can let the request go through because the pod is protected by a PDB if the VMI wants to be live-migrated on
	// eviction. Otherwise, we can just evict it.
	return validating_webhooks.NewPassingAdmissionResponse()
}

func (admitter *PodEvictionAdmitter) markVMI(ar *admissionv1.AdmissionReview, vmi *virtv1.VirtualMachineInstance, dryRun bool) (err error) {

	data := fmt.Sprintf(`[{ "op": "add", "path": "/status/evacuationNodeName", "value": "%s" }]`, vmi.Status.NodeName)

	if !dryRun {
		_, err = admitter.
			VirtClient.
			VirtualMachineInstance(ar.Request.Namespace).
			Patch(vmi.Name,
				types.JSONPatchType,
				[]byte(data),
				&metav1.PatchOptions{})
	}

	return err
}

func denied(message string) *admissionv1.AdmissionResponse {
	return &admissionv1.AdmissionResponse{
		Allowed: false,
		Result: &metav1.Status{
			Message: message,
			Code:    http.StatusTooManyRequests,
		},
	}
}
