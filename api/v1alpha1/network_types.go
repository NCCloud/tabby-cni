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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// NetworkSpec defines the desired state of Network
type NetworkSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	Bridge        []Bridge               `json:"bridge"`
	IpMasq        Masquerade             `json:"ipMasq,omitempty"`
	Routes        []Route                `json:"routes,omitempty"`
	NodeSelectors []metav1.LabelSelector `json:"nodeSelectors,omitempty"`
}

// Linux bridge
type Bridge struct {
	Name  string `json:"name"`
	Mtu   int    `json:"mtu,omitempty"`
	Ports []Port `json:"ports,omitempty"`
}

type Port struct {
	Name string `json:"name"`
	Vlan int    `json:"vlan,omitempty"`
	Mtu  int    `json:"mtu,omitempty"`
}

// Static routes
// The Via parameter could be ip address or device name.
type Route struct {
	Via         string `json:"via"`
	Destination string `json:"destination"`
	Source      string `json:"source,omitempty"`
}

// Masquerade virtual machine traffic
type Masquerade struct {
	Enabled       bool     `json:"enabled"`
	Source        string   `json:"source"`
	Ignore        []string `json:"ignore,omitempty"`
	Bridge        string   `json:"bridge"`
	EgressNetwork string   `json:"egressnetwork,omitempty"`
}

// NetworkStatus defines the observed state of Network
type NetworkStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Network is the Schema for the networks API
type Network struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   NetworkSpec   `json:"spec,omitempty"`
	Status NetworkStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// NetworkList contains a list of Network
type NetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Network `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Network{}, &NetworkList{})
}
