/*
Copyright 2023.

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

	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/NCCloud/tabby-cni/pkg/bridge"
	"github.com/vishvananda/netlink"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const networkAttachmentFinalizer = "network.namecheapcloud.net/finalizer"

// NetworkAttachmentReconciler reconciles a Network object
type NetworkAttachmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networkattachments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networkattachments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networkattachments/finalizers,verbs=update

func (r *NetworkAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	hostname, err := getHostname()
	if err != nil {
		log.Log.Error(err, "Failed to get node hostname")
		return ctrl.Result{}, err
	}

	networkAttachment := &networkv1alpha1.NetworkAttachment{}
	err = r.Get(ctx, req.NamespacedName, networkAttachment)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Log.Info("NetworkAttachemnt resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if hostname != networkAttachment.Spec.NodeName {
		return ctrl.Result{}, nil
	}

	isNetworkMarkedToBeDeleted := networkAttachment.GetDeletionTimestamp() != nil
	if isNetworkMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(networkAttachment, networkAttachmentFinalizer) {
			log.Log.Info("Performing Finalizer Operations for Network resource before delete CR")

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			// Re-fetch the network Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, networkAttachment); err != nil {
				log.Log.Error(err, "Failed to re-fetch network")
				return ctrl.Result{}, err
			}
			log.Log.Info(fmt.Sprintf("Resource %+v", networkAttachment))

			// Remove linux bridge
			for _, bridge_spec := range networkAttachment.Spec.Bridge {
				if err := bridge.Remove(bridge_spec.Name); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Remove iptables rules
			if networkAttachment.Spec.IpMasq.Enabled {
				if err = DeleteMasquerade(&networkAttachment.Spec.IpMasq); err != nil {
					return ctrl.Result{}, err
				}
			}

			log.Log.Info("Removing Finalizer for network after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(networkAttachment, networkAttachmentFinalizer); !ok {
				log.Log.Error(err, "Failed to remove finalizer for network")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, networkAttachment); err != nil {
				log.Log.Error(err, "Failed to remove finalizer for network")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(networkAttachment, networkAttachmentFinalizer) {
		log.Log.Info("Adding Finalizer for network resource")
		if ok := controllerutil.AddFinalizer(networkAttachment, networkAttachmentFinalizer); !ok {
			log.Log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, networkAttachment); err != nil {
			log.Log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Create network resources
	// Create linux bridge
	for _, bridge_spec := range networkAttachment.Spec.Bridge {
		br, err := bridge.Create(bridge_spec.Name, bridge_spec.Mtu)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Add vlan to the interface
		for _, port_spec := range bridge_spec.Ports {
			vlan, err := bridge.AddVlan(port_spec.Name, port_spec.Vlan, port_spec.Mtu)
			if err != nil {
				log.Log.Error(err, fmt.Sprintf("failed to add vlan %d to interface %s", port_spec.Vlan, port_spec.Name))
				return ctrl.Result{}, err
			}

			// Attach vlan interface to the linux bridge
			if err := netlink.LinkSetMaster(vlan, br); err != nil {
				log.Log.Error(err, fmt.Sprintf("failed to add interface %s to the bridge %s", vlan.Name, br.Name))
				return ctrl.Result{}, err
			}
		}
	}

	// Add static routes
	for _, route := range networkAttachment.Spec.Routes {
		if err = addRoute(route); err != nil {
			log.Log.Error(err, "Failed to add static routes")
			return ctrl.Result{}, err
		}
	}

	// Add or remove snat firewall rules
	if networkAttachment.Spec.IpMasq.Enabled {
		if err = EnableMasquerade(&networkAttachment.Spec.IpMasq); err != nil {
			log.Log.Error(err, fmt.Sprintf("failed to add masquerade: %v", networkAttachment.Spec.IpMasq))
			return ctrl.Result{}, err
		}
	}

	log.Log.Info(fmt.Sprintf("RUNNING NetworkAttachment reconciler %+v", req))

	return ctrl.Result{}, nil
}

func NewNetworkAttachment(hostname string, n *networkv1alpha1.Network, req ctrl.Request) (*networkv1alpha1.NetworkAttachment, error) {
	return &networkv1alpha1.NetworkAttachment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-%s", hostname, req.Name),
			Namespace: req.Namespace,
			OwnerReferences: []metav1.OwnerReference{{
				// d.APIVersion is empty.
				APIVersion: "network.namecheapcloud.net/v1alpha1",
				// d.Kind is empty.
				Kind:               "Network",
				Name:               n.Name,
				UID:                n.UID,
				BlockOwnerDeletion: pointer.Bool(true),
				Controller:         pointer.Bool(true),
			}},
		},
		Spec: networkv1alpha1.NetworkAttachmentSpec{
			Bridge:        n.Spec.Bridge,
			Routes:        n.Spec.Routes,
			IpMasq:        n.Spec.IpMasq,
			NodeSelectors: n.Spec.NodeSelectors,
			NodeName:      hostname,
		},
	}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1alpha1.NetworkAttachment{}).
		Complete(r)
}
