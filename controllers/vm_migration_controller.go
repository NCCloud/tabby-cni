/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"net"
	"strings"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/j-keck/arping"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	virtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// VirtualMachineReconciler makes sure the VM has a correct mac address on the switch
type VirtualMachineReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=kubevirt.io,resources=virtualmachineinstances,verbs=get;list;watch

func (r *VirtualMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	log.Log.Info(fmt.Sprintf("VirtualMachine: Reconcile Virtual Machine %+v", req))

	virtualMachineInstance := &virtv1.VirtualMachineInstance{}
	err := r.Get(ctx, req.NamespacedName, virtualMachineInstance)
	if err != nil {
		return ctrl.Result{}, err
	}

	virtualMachineNetworks := virtualMachineInstance.Spec.Networks

	for i := range virtualMachineNetworks {
		multusNetwork := virtualMachineNetworks[i].NetworkSource.Multus
		// network type can be Multus or Pod. We will work only with Multus
		if multusNetwork == nil {
			continue
		}

		namespacedNetworkName := getNamespacedNetworkName(multusNetwork.NetworkName)

		network := &networkv1alpha1.Network{}
		err := r.Get(ctx, namespacedNetworkName, network)
		if err != nil {
			return ctrl.Result{}, err
		}

		networkIpMasq := network.Spec.IpMasq
		// send a garp request only if IP masquerading is enabled
		if !networkIpMasq.Enabled {
			continue
		}

		// arping -A -i <interface-name> -S 169.254.1.1 169.254.1.1
		interfaceName := networkIpMasq.Bridge
		log.Log.Info(
			fmt.Sprintf(
				"VirtualMachine: Sending a garp request to IP %s on interface %s for VM %s",
				virtualIpaddress, interfaceName, req.Name),
		)
		err = arping.GratuitousArpOverIfaceByName(net.ParseIP(virtualIpaddress), interfaceName)
		if err != nil {
			return ctrl.Result{}, err
		}

	}

	return ctrl.Result{}, nil
}

// Given a network name of the KubeVirt VM object,
// construct a namespaced name that can be used in API requests
func getNamespacedNetworkName(networkName string) types.NamespacedName {
	var namespacedNetworkName types.NamespacedName

	networkNameParts := strings.Split(networkName, "/")
	// this means the network is provided with the namespace
	// default/network-name
	if len(networkNameParts) == 2 {
		namespacedNetworkName.Namespace = networkNameParts[0]
		namespacedNetworkName.Name = networkNameParts[1]
	} else {
		namespacedNetworkName.Name = networkNameParts[0]
	}

	return namespacedNetworkName
}

// check if the migration status of the VMI object indicates that the migration was successful
func migrationSuccessful(vmi virtv1.VirtualMachineInstance) bool {
	// the migration has not yet been performed on this VM
	// therefore, we consider the migration not successful
	if vmi.Status.MigrationState == nil {
		return false
	}
	migrationCompleted := vmi.Status.MigrationState.Completed
	migrationFailed := vmi.Status.MigrationState.Failed

	// completed and not failed
	// migration can fail but complete if it was aborted
	return migrationCompleted && !migrationFailed
}

func filterVirtualMachineMigrationEvents(e event.UpdateEvent) bool {

	newVirtualMachineInstanceObj, ok := e.ObjectNew.(*virtv1.VirtualMachineInstance)
	if !ok {
		return false
	}

	oldVirtualMachineInstanceObj, ok := e.ObjectOld.(*virtv1.VirtualMachineInstance)
	if !ok {
		return false
	}

	newMigrationSuccessful := migrationSuccessful(*newVirtualMachineInstanceObj)
	oldMigrationSuccessful := migrationSuccessful(*oldVirtualMachineInstanceObj)

	// the migration has not succeeded yet
	if !newMigrationSuccessful {
		return false
	}

	// the migration state is the same, e.g. true and true; don't send a duplicate arp request
	if newMigrationSuccessful == oldMigrationSuccessful {
		return false
	}

	hostname, err := getHostname()
	if err != nil {
		log.Log.Error(err, "VirtualMachine Filter: Failed to get node hostname, skipping the event")
		return false
	}

	// each pod subscribes to the same events. Make sure the arp request is sent only from
	// the target node
	if hostname != newVirtualMachineInstanceObj.Status.MigrationState.TargetNode {
		return false
	}

	vmiName := newVirtualMachineInstanceObj.Name
	log.Log.Info(fmt.Sprintf("VirtualMachine Filter: VM %s has completed the migration", vmiName))
	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *VirtualMachineReconciler) SetupWithManager(mgr ctrl.Manager) error {

	p := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return filterVirtualMachineMigrationEvents(e)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&virtv1.VirtualMachineInstance{}).
		WithEventFilter(p).
		Complete(r)
}
