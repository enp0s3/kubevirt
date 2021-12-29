package admitters

import (
	"fmt"
	"net/http"
	"strings"

	"k8s.io/client-go/tools/cache"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	virtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/client-go/log"

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
	log.Log.V(2).Infof("Entering Admit with key %s", key)
	obj, exists, err := admitter.PodInformer.GetStore().GetByKey(key)
	if !exists || err != nil {
		log.Log.V(2).Infof("could not find pod %s", key)
		if err != nil {
			log.Log.V(2).Infof("could not find pod %s with error: %s", key, err.Error())
		}
		return validating_webhooks.NewPassingAdmissionResponse()
	}

	//launcher, err := admitter.VirtClient.CoreV1().Pods(ar.Request.Namespace).Get(context.Background(), ar.Request.Name, metav1.GetOptions{})
	//if err != nil {
	//	return validating_webhooks.NewPassingAdmissionResponse()
	//}

	launcher := obj.(*corev1.Pod)
	if value, exists := launcher.GetLabels()[virtv1.AppLabel]; !exists || value != "virt-launcher" {
		log.Log.V(2).Infof("Pod with name %s doesn't have the kubevirt value ", launcher.Name)
		return validating_webhooks.NewPassingAdmissionResponse()
	}

	domainName, exists := launcher.GetAnnotations()[virtv1.DomainAnnotation]
	if !exists {
		log.Log.V(2).Infof("Pod with name %s doesn't have the annotation to the VMI ", launcher.Name)
		return validating_webhooks.NewPassingAdmissionResponse()
	}

	key = fmt.Sprintf("%v/%v", ar.Request.Namespace, domainName)
	log.Log.V(2).Infof("looking for VMI with key %s", key)
	obj, exists, err = admitter.VMIInformer.GetStore().GetByKey(key)

	if err != nil {
		log.Log.V(2).Infof("kubevirt failed getting the vmi: %s", err.Error())
		return denied(fmt.Sprintf("kubevirt failed getting the vmi: %s", err.Error()))
	} else if !exists {
		log.Log.V(2).Infof("VMI %s corresponding to the virt-launcher pod %s not found", key, launcher.Name)
		return denied(fmt.Sprintf("VMI %s corresponding to the virt-launcher pod %s not found", key, launcher.Name))
	}

	vmi := obj.(*virtv1.VirtualMachineInstance)
	//vmi, err = admitter.VirtClient.VirtualMachineInstance(ar.Request.Namespace).Get(domainName, &metav1.GetOptions{})
	//if err != nil {
	//	return denied(fmt.Sprintf("kubevirt failed getting the vmi: %s", err.Error()))
	//}

	log.Log.V(2).Infof("VMI %s found, is it evictable", key)
	if !vmi.IsEvictable() {
		// we don't act on VMIs without an eviction strategy
		return validating_webhooks.NewPassingAdmissionResponse()
	} else if !vmi.IsMigratable() {
		log.Log.V(2).Infof("VMI %s is configured with an eviction strategy but is not live-migratable", vmi.Name)
		return denied(fmt.Sprintf(
			"VMI %s is configured with an eviction strategy but is not live-migratable", vmi.Name))
	}

	log.Log.V(2).Infof("VMI %s found launcher name is %s ", key, launcher.Name)
	if !vmi.IsMarkedForEviction() && vmi.Status.NodeName == launcher.Spec.NodeName {
		dryRun := ar.Request.DryRun != nil && *ar.Request.DryRun == true
		err := admitter.markVMI(ar, vmi, dryRun)
		if err != nil {
			// As with the previous case, it is up to the user to issue a retry.
			log.Log.V(2).Infof("kubevirt failed marking the vmi for eviction: %s", err.Error())
			return denied(fmt.Sprintf("kubevirt failed marking the vmi for eviction: %s", err.Error()))
		}
	}

	// We can let the request go through because the pod is protected by a PDB if the VMI wants to be live-migrated on
	// eviction. Otherwise, we can just evict it.
	return validating_webhooks.NewPassingAdmissionResponse()
}

func (admitter *PodEvictionAdmitter) markVMI(ar *admissionv1.AdmissionReview, vmi *virtv1.VirtualMachineInstance, dryRun bool) (err error) {
	vmiCopy := vmi.DeepCopy()
	vmiCopy.Status.EvacuationNodeName = vmi.Status.NodeName
	//
	//oldVmi, err := json.Marshal(vmi)
	//if err != nil {
	//	return
	//}
	//
	//newVmi, err := json.Marshal(vmiCopy)
	//if err != nil {
	//	return
	//}
	//
	//patch, err := strategicpatch.CreateTwoWayMergePatch(oldVmi, newVmi, vmi)
	//if err != nil {
	//	return
	//}
	// fmt.Sprintf("{\"spec\":{\"runStrategy\": \"%s\"}}", runStrategy)
	//data := fmt.Sprintf("{\"status\":{\"evacuationNodeName\": \"%s\"}}", vmi.Status.NodeName)
	data := generateUpdateStatusPatch(vmi, vmiCopy)

	if len(data) > 0 && !dryRun {
		_, err = admitter.
			VirtClient.
			VirtualMachineInstance(ar.Request.Namespace).
			Patch(vmi.Name,
				types.JSONPatchType,
				data,
				&metav1.PatchOptions{})
	} else {
		log.Log.V(2).Info("Entering markVMI, nothing to patch")
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

func generateUpdateStatusPatch(oldVMI, newVMI *virtv1.VirtualMachineInstance) []byte {
	var patchOps []string

	if oldVMI.Status.EvacuationNodeName != newVMI.Status.EvacuationNodeName {
		if oldVMI.Status.EvacuationNodeName == "" {
			patchOps = append(patchOps, fmt.Sprintf(`{ "op": "add", "path": "/status/evacuationNodeName", "value": "%s" }`, newVMI.Status.EvacuationNodeName))
		} else {
			patchOps = append(patchOps, fmt.Sprintf(`{ "op": "test", "path": "/status/evacuationNodeName", "value": "%s" }`, oldVMI.Status.EvacuationNodeName))
			patchOps = append(patchOps, fmt.Sprintf(`{ "op": "replace", "path": "/status/evacuationNodeName", "value": "%s" }`, newVMI.Status.EvacuationNodeName))
		}
	}

	if len(patchOps) == 0 {
		return nil
	}

	log.Log.V(2).Infof("Entering generateUpdateStatusPatch, patch data is  %s", patchOps)
	return []byte(fmt.Sprintf("[%s]", strings.Join(patchOps, ", ")))
}
