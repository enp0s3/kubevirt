package migrations

import (
	"fmt"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/cache"
	v1 "kubevirt.io/client-go/api/v1"
	"kubevirt.io/client-go/kubecli"
	pvcutils "kubevirt.io/kubevirt/pkg/util/types"
	k8sv1 "k8s.io/api/core/v1"
)

func ListUnfinishedMigrations(informer cache.SharedIndexInformer) ([]*v1.VirtualMachineInstanceMigration, error) {
	objs := informer.GetStore().List()
	migrations := []*v1.VirtualMachineInstanceMigration{}
	for _, obj := range objs {
		migration := obj.(*v1.VirtualMachineInstanceMigration)
		if !migration.IsFinal() {
			migrations = append(migrations, migration)
		}
	}
	return migrations, nil
}

func FilterRunningMigrations(migrations []v1.VirtualMachineInstanceMigration) []v1.VirtualMachineInstanceMigration {
	runningMigrations := []v1.VirtualMachineInstanceMigration{}
	for _, migration := range migrations {
		if migration.IsRunning() {
			runningMigrations = append(runningMigrations, migration)
		}
	}
	return runningMigrations
}

func DetermineMigrationCapability(client kubecli.KubevirtClient, vmi *v1.VirtualMachineInstance)(
	v1.VirtualMachineInstanceMigrationMethod,
	[]v1.VirtualMachineInstanceCondition,
	bool) {
		var conditions []v1.VirtualMachineInstanceCondition
		var method v1.VirtualMachineInstanceMigrationMethod
		var err error
		var isLiveMigratable bool

		isBlockLiveMigration, err := CheckVolumesForMigration(client, vmi)
		if err != nil {
			conditions = append(conditions, v1.VirtualMachineInstanceCondition{
				Type:    v1.VirtualMachineInstanceIsMigratable,
				Status:  k8sv1.ConditionFalse,
				Message: err.Error(),
				Reason:  v1.VirtualMachineInstanceReasonDisksNotMigratable,
			})
			isLiveMigratable = false
		}

		err = CheckNetworkInterfacesForMigration(vmi)
		if err != nil {
			conditions = append(conditions, v1.VirtualMachineInstanceCondition{
				Type:    v1.VirtualMachineInstanceIsMigratable,
				Status:  k8sv1.ConditionFalse,
				Message: err.Error(),
				Reason:  v1.VirtualMachineInstanceReasonInterfaceNotMigratable,
			})
			isLiveMigratable = false
		}

		if len(conditions) == 0 {
			conditions = append(conditions, v1.VirtualMachineInstanceCondition{
				Type:   v1.VirtualMachineInstanceIsMigratable,
				Status: k8sv1.ConditionTrue,
			})
			isLiveMigratable = true
		}

		if isBlockLiveMigration {
			method = v1.BlockMigration
		} else {
			method = v1.LiveMigration
		}

		return method, conditions, isLiveMigratable
}

func CheckVolumesForMigration(client kubecli.KubevirtClient, vmi *v1.VirtualMachineInstance) (blockMigrate bool, err error) {
	// Check if all VMI volumes can be shared between the source and the destination
	// of a live migration. blockMigrate will be returned as false, only if all volumes
	// are shared and the VMI has no local disks
	// Some combinations of disks makes the VMI no suitable for live migration.
	// A relevant error will be returned in this case.
	for _, volume := range vmi.Spec.Volumes {
		volSrc := volume.VolumeSource
		if volSrc.PersistentVolumeClaim != nil || volSrc.DataVolume != nil {
			var volName string
			if volSrc.PersistentVolumeClaim != nil {
				volName = volSrc.PersistentVolumeClaim.ClaimName
			} else {
				volName = volSrc.DataVolume.Name
			}
			_, shared, err := pvcutils.IsSharedPVCFromClient(client, vmi.Namespace, volName)
			if errors.IsNotFound(err) {
				return blockMigrate, fmt.Errorf("persistentvolumeclaim %v not found", volName)
			} else if err != nil {
				return blockMigrate, err
			}
			if !shared {
				return true, fmt.Errorf("cannot migrate VMI with non-shared PVCs")
			}
		} else if volSrc.HostDisk != nil {
			shared := volSrc.HostDisk.Shared != nil && *volSrc.HostDisk.Shared
			if !shared {
				return true, fmt.Errorf("cannot migrate VMI with non-shared HostDisk")
			}
		} else {
			blockMigrate = true
		}
	}
	return
}

func CheckNetworkInterfacesForMigration(vmi *v1.VirtualMachineInstance) error {
	networks := map[string]*v1.Network{}
	for _, network := range vmi.Spec.Networks {
		networks[network.Name] = network.DeepCopy()
	}
	for _, iface := range vmi.Spec.Domain.Devices.Interfaces {
		if iface.Masquerade == nil && networks[iface.Name].Pod != nil {
			return fmt.Errorf("cannot migrate VMI which does not use masquerade to connect to the pod network")
		}
	}

	return nil
}

func DetermineMigrationCapability2(client kubecli.KubevirtClient, vmi *v1.VirtualMachineInstance)(
	method v1.VirtualMachineInstanceMigrationMethod,
	reason string,
	err error) {

	return
}
