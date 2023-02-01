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

	"os"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/NCCloud/tabby-cni/pkg/bridge"
	"github.com/vishvananda/netlink"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
)

const networkFinalizer = "network.namecheapcloud.net/finalizer"
const nodeName = "NODE_NAME"

// NetworkReconciler reconciles a Network object
type NetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Network object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *NetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// Use NODE_NAME env variable or Hostname to identify host.
	host := os.Getenv(nodeName)
	if host == "" {
		host, err := os.Hostname()
		if err != nil {
			log.Log.Error(err, fmt.Sprintf("Unable to get node hostname %s", host))
			return ctrl.Result{Requeue: true}, nil
		}
	}

	// TODO(user): your logic here
	log.Log.Info("TESTING")
	log.Log.Info(fmt.Sprintf("REQ %+v", req))

	network := &networkv1alpha1.Network{}
	err := r.Get(ctx, req.NamespacedName, network)

	if err != nil {
		if errors.IsNotFound(err) {
			log.Log.Info("Network resource not found. Ignoring since object must be deleted, Cleanup interfaces")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if network.Spec.Host != host {
		log.Log.Info(fmt.Sprintf("Nothing to do, node hostname %s is not equal to Host %s in spec", host, network.Spec.Host))
		return ctrl.Result{}, nil
	}

	// Let's add a finalizer. Then, we can define some operations which should
	// occurs before the custom resource to be deleted.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers
	if !controllerutil.ContainsFinalizer(network, networkFinalizer) {
		log.Log.Info("Adding Finalizer for network resource")
		if ok := controllerutil.AddFinalizer(network, networkFinalizer); !ok {
			log.Log.Error(err, "Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, network); err != nil {
			log.Log.Error(err, "Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// Check if the Network instance is marked to be deleted, which is
	// indicated by the deletion timestamp being set.
	isNetworkMarkedToBeDeleted := network.GetDeletionTimestamp() != nil
	if isNetworkMarkedToBeDeleted {
		if controllerutil.ContainsFinalizer(network, networkFinalizer) {
			log.Log.Info("Performing Finalizer Operations for Network resource before delete CR")

			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			// Re-fetch the network Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, network); err != nil {
				log.Log.Error(err, "Failed to re-fetch network")
				return ctrl.Result{}, err
			}
			log.Log.Info(fmt.Sprintf("Resource %+v", network))

			// Remove linux bridge
			for _, bridge_spec := range network.Spec.Bridge {
				if err := bridge.Remove(bridge_spec.Name); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Remove iptables rules
			if network.Spec.IpMasq.Enabled {
				if err = DeleteMasquerade(&network.Spec.IpMasq); err != nil {
					return ctrl.Result{}, err
				}
			}

			log.Log.Info("Removing Finalizer for network after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(network, networkFinalizer); !ok {
				log.Log.Error(err, "Failed to remove finalizer for network")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, network); err != nil {
				log.Log.Error(err, "Failed to remove finalizer for network")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Create network resources
	// Create linux bridge
	for _, bridge_spec := range network.Spec.Bridge {
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
	for _, route := range network.Spec.Routes {
		if err = addRoute(route); err != nil {
			log.Log.Error(err, "Failed to add static routes")
			return ctrl.Result{}, err
		}
	}

	// Add or remove snat firewall rules
	if network.Spec.IpMasq.Enabled {
		if err = EnableMasquerade(&network.Spec.IpMasq); err != nil {
			log.Log.Error(err, fmt.Sprintf("failed to add masquerade: %v", network.Spec.IpMasq))
			return ctrl.Result{}, err
		}
	} else {
		if err = DeleteMasquerade(&network.Spec.IpMasq); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Log.Info(fmt.Sprintf("Resource %+v", network))
	log.Log.Info(fmt.Sprintf("Linux bridge was created %s", network.Name))

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1alpha1.Network{}).
		Complete(r)
}
