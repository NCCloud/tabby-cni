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
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
)

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
	var shouldRun bool = false
	var isUpdateRequired bool = false

	log.Log.Info("Network: Reconsile network resource")

	hostname, err := getHostname()
	if err != nil {
		log.Log.Error(err, "Network: Failed to get node hostname")
		return ctrl.Result{}, err
	}

	network := &networkv1alpha1.Network{}
	err = r.Get(ctx, req.NamespacedName, network)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Log.Info("Network: Network resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if len(network.Spec.NodeSelectors) == 0 {
		shouldRun = true
	} else {
		// Get node hostname. Default from env variable NODE_NAME, if not defined then use hostname
		nodelabels, err := r.nodeLabels(ctx, hostname)
		if err != nil {
			log.Log.Error(err, "Network: Failed to get node labels")
		}

		var nodeSels []labels.Selector
		for _, s := range network.Spec.NodeSelectors {
			s := s // so we can use &s
			labelSelector, _ := metav1.LabelSelectorAsSelector(&s)
			nodeSels = append(nodeSels, labelSelector)
		}

		for _, ns := range nodeSels {
			if ns.Matches(nodelabels) {
				shouldRun = true
			}
		}
	}

	if shouldRun {
		networkAttachment := &networkv1alpha1.NetworkAttachment{}

		err = r.Get(ctx, types.NamespacedName{Name: fmt.Sprintf("%s-%s", hostname, req.Name), Namespace: req.Namespace}, networkAttachment)
		if err != nil && errors.IsNotFound(err) {
			log.Log.Info("Network: Creating networkAttachment resource")

			networkAttachment, err := NewNetworkAttachment(hostname, network, req)
			if err != nil {
				log.Log.Error(err, "Network: Unable to allocate networkAttachment structure")
			}

			if err := r.Create(ctx, networkAttachment); err != nil {
				log.Log.Error(err, "Network: Failed to create networkAttachment resource")
				return ctrl.Result{}, err
			}

			return ctrl.Result{}, nil
		} else if err != nil {
			log.Log.Error(err, "Network: Failed to get networkattachment resource")
			return ctrl.Result{}, err
		}
		// TBD add proper filter and remove this w/a
		if !reflect.DeepEqual(network.Spec.Bridge, networkAttachment.Spec.Bridge) {
			networkAttachment.Spec.Bridge = network.Spec.Bridge
			isUpdateRequired = true
		}

		if !reflect.DeepEqual(network.Spec.Routes, networkAttachment.Spec.Routes) {
			networkAttachment.Spec.Routes = network.Spec.Routes
			isUpdateRequired = true
		}

		if !reflect.DeepEqual(network.Spec.IpMasq, networkAttachment.Spec.IpMasq) {
			networkAttachment.Spec.IpMasq = network.Spec.IpMasq
			isUpdateRequired = true
		}

		if isUpdateRequired {
			log.Log.Info("Network: Updating networkAttachment resource")

			if err := r.Update(ctx, networkAttachment); err != nil {
				log.Log.Error(err, "Network: Failed to update networkattachment resource")
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

func (r *NetworkReconciler) nodeLabels(ctx context.Context, nodeName string) (labels.Set, error) {
	// Get list of kubernetes nodes
	nodes := &corev1.NodeList{}
	err := r.List(ctx, nodes)
	if err != nil {
		log.Log.Error(err, "Network: Could't get list of nodes")
		return nil, err
	}

	// Find node labels
	for _, name := range nodes.Items {
		if name.Name == nodeName {
			return labels.Set(name.Labels), nil
		}
	}

	return nil, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1alpha1.Network{}).
		Complete(r)
}

func getHostname() (string, error) {
	var err error

	host := os.Getenv(nodeName)
	if host == "" {
		host, err = os.Hostname()
		if err != nil {
			log.Log.Error(err, fmt.Sprintf("Network: Unable to get node hostname %s", host))
			return "", err
		}
	}
	return host, nil
}
