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
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const networkAttachmentFinalizer = "network.namecheapcloud.net/finalizer"
const lastAppliedConfiguration = "networkattachment/last-applied-configuration"

// NetworkAttachmentReconciler reconciles a Network object
type NetworkAttachmentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

type NetworkAttachmentChangelog struct {
	addPorts []networkv1alpha1.Bridge
	delPorts []networkv1alpha1.Bridge
}

//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networkattachments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networkattachments/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.namecheapcloud.net,resources=networkattachments/finalizers,verbs=update

func (r *NetworkAttachmentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	log.Log.Info(fmt.Sprintf("NetworkAttachment: Reconsile networkattachment resource %+v", req))

	hostname, err := getHostname()
	if err != nil {
		log.Log.Error(err, "NetworkAttachment: Failed to get node hostname")
		return ctrl.Result{}, err
	}

	networkAttachment := &networkv1alpha1.NetworkAttachment{}
	err = r.Get(ctx, req.NamespacedName, networkAttachment)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Log.Info("NetworkAttachment: NetworkAttachemnt resource not found. Ignoring since object must be deleted")
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
			// Perform all operations required before remove the finalizer and allow
			// the Kubernetes API to remove the custom resource.
			// Re-fetch the network Custom Resource before update the status
			// so that we have the latest state of the resource on the cluster and we will avoid
			// raise the issue "the object has been modified, please apply
			// your changes to the latest version and try again" which would re-trigger the reconciliation
			if err := r.Get(ctx, req.NamespacedName, networkAttachment); err != nil {
				log.Log.Error(err, "NetworkAttachment: Failed to re-fetch network")
				return ctrl.Result{}, err
			}

			log.Log.Info("NetworkAttachment: Performing Finalizer Operations for Network resource before delete CR")

			if err = DeleteNetwork(ctx, &networkAttachment.Spec); err != nil {
				return ctrl.Result{}, err
			}

			if err = DeleteMasquerade(&networkAttachment.Spec.IpMasq); err != nil {
				return ctrl.Result{}, err
			}

			log.Log.Info("NetworkAttachment: Removing Finalizer for network after successfully perform the operations")
			if ok := controllerutil.RemoveFinalizer(networkAttachment, networkAttachmentFinalizer); !ok {
				log.Log.Error(err, "NetworkAttachment: Failed to remove finalizer for network")
				return ctrl.Result{Requeue: true}, nil
			}

			if err := r.Update(ctx, networkAttachment); err != nil {
				log.Log.Error(err, "NetworkAttachment: Failed to remove finalizer for network")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(networkAttachment, networkAttachmentFinalizer) {
		log.Log.Info("NetworkAttachment: Adding Finalizer for network resource")
		if ok := controllerutil.AddFinalizer(networkAttachment, networkAttachmentFinalizer); !ok {
			log.Log.Error(err, "NetworkAttachment: Failed to add finalizer into the custom resource")
			return ctrl.Result{Requeue: true}, nil
		}

		if err = r.Update(ctx, networkAttachment); err != nil {
			log.Log.Error(err, "NetworkAttachment: Failed to update custom resource to add finalizer")
			return ctrl.Result{}, err
		}
	}
	if err = r.DiffNetwork(ctx, req); err != nil {
		return ctrl.Result{}, err
	}

	if err = CreateNetwork(ctx, &networkAttachment.Spec); err != nil {
		return ctrl.Result{}, err
	}

	if err = r.lastAppliedConfig(ctx, req); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *NetworkAttachmentReconciler) lastAppliedConfig(ctx context.Context, req ctrl.Request) error {
	networkAttachment := &networkv1alpha1.NetworkAttachment{}

	if err := r.Get(ctx, req.NamespacedName, networkAttachment); err != nil {
		log.Log.Error(err, "NetworkAttachment: Failed to re-fetch network")
		return err
	}

	netAttachSpec, err := json.Marshal(networkAttachment.Spec)
	if err != nil {
		log.Log.Error(err, "NetworkAttachment: Couldn't serialize networkAttachment.Spec into json")
		return err
	}

	networkAttachment.SetAnnotations(map[string]string{lastAppliedConfiguration: string(netAttachSpec)})

	if err = r.Update(ctx, networkAttachment); err != nil {
		log.Log.Error(err, "NetworkAttachment: Failed to update custom resource to add finalizer")
		return err
	}
	return nil
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

func filterNetworkAttachmentEvent(e event.UpdateEvent) bool {

	newNetworkAttachmentObj, ok := e.ObjectNew.(*networkv1alpha1.NetworkAttachment)
	if !ok {
		return true
	}
	oldNetworkAttachmentNodeObj, ok := e.ObjectOld.(*networkv1alpha1.NetworkAttachment)
	if !ok {
		return true
	}

	if newNetworkAttachmentObj.GetDeletionTimestamp() != nil {
		return true
	}

	if reflect.DeepEqual(newNetworkAttachmentObj.Spec, oldNetworkAttachmentNodeObj.Spec) {
		return false
	}

	return true
}

// SetupWithManager sets up the controller with the Manager.
func (r *NetworkAttachmentReconciler) SetupWithManager(mgr ctrl.Manager) error {

	p := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return filterNetworkAttachmentEvent(e)
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1alpha1.NetworkAttachment{}).
		WithEventFilter(p).
		Complete(r)
}
