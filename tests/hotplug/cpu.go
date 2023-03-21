package hotplug

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"

	"kubevirt.io/kubevirt/tests/libvmi"

	"kubevirt.io/kubevirt/tests/flags"
	util2 "kubevirt.io/kubevirt/tests/util"

	"kubevirt.io/kubevirt/tests/testsuite"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/resource"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/kubecli"

	"kubevirt.io/kubevirt/pkg/pointer"

	"kubevirt.io/kubevirt/pkg/apimachinery/patch"
	"kubevirt.io/kubevirt/tests"
	"kubevirt.io/kubevirt/tests/decorators"
	"kubevirt.io/kubevirt/tests/framework/kubevirt"
	. "kubevirt.io/kubevirt/tests/framework/matcher"
	"kubevirt.io/kubevirt/tests/libwait"
)

var _ = Describe("[sig-compute][Serial]CPU Hotplug", decorators.SigCompute, decorators.RequiresTwoSchedulableNodes, Serial, func() {
	var virtClient kubecli.KubevirtClient

	BeforeEach(func() {
		virtClient = kubevirt.Client()
		originalKv := util2.GetCurrentKv(virtClient)
		updateStrategy := &v1.KubeVirtWorkloadUpdateStrategy{
			WorkloadUpdateMethods: []v1.WorkloadUpdateMethod{v1.WorkloadUpdateMethodLiveMigrate},
		}
		patchWorkloadUpdateMethod(originalKv.Name, virtClient, updateStrategy)
		testsuite.EnsureKubevirtReady()

	})

	Context("A VM with cpu.maxSockets set higher than cpu.sockets", func() {
		type cpuCount struct {
			enabled  int
			disabled int
		}
		countDomCPUs := func(vmi *v1.VirtualMachineInstance) (count cpuCount) {
			domSpec, err := tests.GetRunningVMIDomainSpec(vmi)
			Expect(err).NotTo(HaveOccurred())
			Expect(domSpec.VCPUs).NotTo(BeNil())
			for _, cpu := range domSpec.VCPUs.VCPU {
				if cpu.Enabled == "yes" {
					count.enabled++
				} else {
					ExpectWithOffset(1, cpu.Enabled).To(Equal("no"))
					ExpectWithOffset(1, cpu.Hotpluggable).To(Equal("yes"))
					count.disabled++
				}
			}
			return
		}
		It("should have spare CPUs that be enabled and more pod CPU resource after a migration", func() {
			By("Creating a running VM with 1 socket and 2 max sockets")
			const (
				maxSockets uint32 = 2
			)

			vmi := libvmi.NewAlpine()
			vmi.Namespace = testsuite.GetTestNamespace(vmi)
			vmi.Spec.Domain.CPU = &v1.CPU{
				Cores:   2,
				Sockets: 1,
				Threads: 1,
			}
			vm := tests.NewRandomVirtualMachine(vmi, true)
			vm.Spec.LiveUpdateFeatures = &v1.LiveUpdateFeatures{
				CPU: &v1.LiveUpdateCPU{
					MaxSockets: pointer.P(maxSockets),
				},
			}

			vm, err := virtClient.VirtualMachine(vm.Namespace).Create(context.Background(), vm)
			Eventually(ThisVM(vm), 360*time.Second, 1*time.Second).Should(beReady())
			libwait.WaitForSuccessfulVMIStart(vmi)

			By("Ensuring the compute container has 200m CPU")
			compute := tests.GetComputeContainerOfPod(tests.GetVmiPod(virtClient, vmi))

			Expect(compute).NotTo(BeNil(), "failed to find compute container")
			Expect(compute.Resources.Requests.Cpu()).To(Equal(resource.MustParse("200m")))

			By("Ensuring the libvirt domain has 2 enabled cores and 2 hotpluggable cores")
			Expect(countDomCPUs(vmi)).To(Equal(cpuCount{
				enabled:  2,
				disabled: 2,
			}))

			By("Enabling the second socket")
			patchData, err := patch.GenerateTestReplacePatch("/spec/template/spec/domain/cpu/sockets", 1, 2)
			Expect(err).NotTo(HaveOccurred())
			_, err = virtClient.VirtualMachine(vm.Namespace).Patch(context.Background(), vm.Name, types.JSONPatchType, patchData, &k8smetav1.PatchOptions{})
			Expect(err).ToNot(HaveOccurred())

			var migration *v1.VirtualMachineInstanceMigration

			By("Ensuring live-migration started")
			Eventually(func() bool {
				migrations, err := virtClient.VirtualMachineInstanceMigration(vm.Namespace).List(&k8smetav1.ListOptions{})
				Expect(err).ToNot(HaveOccurred())
				for _, mig := range migrations.Items {
					if mig.Spec.VMIName == vmi.Name {
						migration = mig.DeepCopy()
						return true
					}
				}
				return false
			}, 30*time.Second, time.Second).Should(BeTrue())
			tests.ExpectMigrationSuccess(virtClient, migration, tests.MigrationWaitTime)

			By("Ensuring the libvirt domain has 4 enabled cores")
			Eventually(func() cpuCount {
				vmi, err = virtClient.VirtualMachineInstance(vm.Namespace).Get(context.Background(), vm.Name, &k8smetav1.GetOptions{})
				Expect(err).NotTo(HaveOccurred())
				return countDomCPUs(vmi)
			}, 30*time.Second, time.Second).Should(Equal(cpuCount{
				enabled:  4,
				disabled: 0,
			}))

			By("Ensuring the virt-launcher pod now has 400m CPU")
			compute = tests.GetComputeContainerOfPod(tests.GetVmiPod(virtClient, vmi))

			Expect(compute).NotTo(BeNil(), "failed to find compute container")
			Expect(*compute.Resources.Requests.Cpu()).To(Equal(resource.MustParse("400m")))
		})
	})
})

func patchWorkloadUpdateMethod(kvName string, virtClient kubecli.KubevirtClient, updateStrategy *v1.KubeVirtWorkloadUpdateStrategy) {
	methodData, err := json.Marshal(updateStrategy)
	ExpectWithOffset(1, err).To(Not(HaveOccurred()))

	data := []byte(fmt.Sprintf(`[{"op": "replace", "path": "/spec/workloadUpdateStrategy", "value": %s}]`, string(methodData)))

	EventuallyWithOffset(1, func() error {
		_, err := virtClient.KubeVirt(flags.KubeVirtInstallNamespace).Patch(kvName, types.JSONPatchType, data, &k8smetav1.PatchOptions{})
		return err
	}, 10*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
}

func beReady() gomegatypes.GomegaMatcher {
	return gstruct.PointTo(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
		"Status": gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Ready": BeTrue(),
		}),
	}))
}
