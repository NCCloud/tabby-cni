package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/NCCloud/tabby-cni/pkg/bridge"
	"github.com/r3labs/diff"
	"golang.org/x/exp/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func (r *NetworkAttachmentReconciler) DiffNetwork(ctx context.Context, req ctrl.Request) error {
	_ = log.FromContext(ctx)

	networkAttachment := &networkv1alpha1.NetworkAttachment{}
	prevNetworkAttachmentSpec := &networkv1alpha1.NetworkAttachmentSpec{}

	if err := r.Get(ctx, req.NamespacedName, networkAttachment); err != nil {
		log.Log.Error(err, "NetworkAttachment: Failed to re-fetch network")
		return err
	}

	annotations := networkAttachment.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	originAnnotation, hasAnnotation := annotations[lastAppliedConfiguration]

	if !hasAnnotation {
		return nil
	}

	err := json.Unmarshal([]byte(originAnnotation), prevNetworkAttachmentSpec)
	if err != nil {
		log.Log.Error(err, "NetworkAttachment: Failed to parse json")
		return err
	}

	portsDiff, err := portDiff(prevNetworkAttachmentSpec, &networkAttachment.Spec)
	if err != nil {
		log.Log.Error(err, "NetworkAttachment: Unable to get diff of ports for cleanup")
		return err
	}

	for _, p := range portsDiff {
		if err = bridge.DeletePort(p); err != nil {
			log.Log.Error(err, fmt.Sprintf("NetworkAttachment: Unable to delete port from linux bridge %s", p))
			return err

		}
	}

	// TBD routeDiff
	// TBD firewallDiff

	return nil
}

func portDiff(prev, current *networkv1alpha1.NetworkAttachmentSpec) ([]string, error) {
	var ports []string

	changelog, err := diff.Diff(prev, current)
	if err != nil {
		log.Log.Error(err, "NetworkAttachment: Failed to perform diff")
		return nil, err
	}

	for _, change := range changelog {
		log.Log.Info(fmt.Sprintf("NetworkAttachment: Show networkattachment diff %+v", change))

		if change.Type == "delete" || change.Type == "update" {
			if slices.Contains(change.Path, "Bridge") {
				// Changelog Path returns [Bridge 0 Port 0 Name]
				_brId, _portId := change.Path[1], change.Path[3]
				brId, err := strToInt(_brId)
				if err != nil {
					log.Log.Error(err, fmt.Sprintf("Failed to convert string to int %s", _brId))
					return nil, err
				}
				portId, err := strToInt(_portId)
				if err != nil {
					log.Log.Error(err, fmt.Sprintf("Failed to convert string to int %s", _portId))
					return nil, err
				}

				port := prev.Bridge[brId].Ports[portId]
				portName := fmt.Sprintf("%s.%d", port.Name, port.Vlan)

				if !slices.Contains(ports, portName) {
					ports = append(ports, portName)
				}
			}
		}
	}
	return ports, nil
}

func strToInt(value string) (int, error) {
	v, err := strconv.Atoi(value)
	if err != nil {
		log.Log.Error(err, "Error during conversion")
		return 0, err
	}
	return v, nil
}
