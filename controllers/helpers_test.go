package controllers

import (
	"net"
	"reflect"
	"sort"
	"testing"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	virtv1 "kubevirt.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func cidr(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatalf("failed to parse %s: %v", s, err)
	}
	return n
}

func TestEqualCIDR(t *testing.T) {
	a := cidr(t, "10.0.0.0/24")

	tests := []struct {
		name string
		a, b *net.IPNet
		want bool
	}{
		{name: "same pointer", a: a, b: a, want: true},
		{name: "equal networks", a: cidr(t, "10.0.0.0/24"), b: cidr(t, "10.0.0.0/24"), want: true},
		{name: "different address", a: cidr(t, "10.0.0.0/24"), b: cidr(t, "10.0.1.0/24"), want: false},
		{name: "different mask", a: cidr(t, "10.0.0.0/24"), b: cidr(t, "10.0.0.0/16"), want: false},
		{name: "both nil", a: nil, b: nil, want: true},
		{name: "one nil", a: a, b: nil, want: false},
		{name: "other nil", a: nil, b: a, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EqualCIDR(tt.a, tt.b); got != tt.want {
				t.Errorf("EqualCIDR(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestPortDiff(t *testing.T) {
	spec := func(bridges ...networkv1alpha1.Bridge) *networkv1alpha1.NetworkAttachmentSpec {
		return &networkv1alpha1.NetworkAttachmentSpec{Bridge: bridges}
	}
	br := func(name string, ports ...networkv1alpha1.Port) networkv1alpha1.Bridge {
		return networkv1alpha1.Bridge{Name: name, Ports: ports}
	}
	port := func(name string, vlan int) networkv1alpha1.Port {
		return networkv1alpha1.Port{Name: name, Vlan: vlan}
	}

	tests := []struct {
		name    string
		prev    *networkv1alpha1.NetworkAttachmentSpec
		current *networkv1alpha1.NetworkAttachmentSpec
		want    []string
	}{
		{
			name:    "no changes",
			prev:    spec(br("br0", port("eth0", 100))),
			current: spec(br("br0", port("eth0", 100))),
			want:    nil,
		},
		{
			name:    "port added",
			prev:    spec(br("br0", port("eth0", 100))),
			current: spec(br("br0", port("eth0", 100), port("eth0", 200))),
			want:    nil,
		},
		{
			name:    "last port removed",
			prev:    spec(br("br0", port("eth0", 100), port("eth0", 200))),
			current: spec(br("br0", port("eth0", 100))),
			want:    []string{"eth0.200"},
		},
		{
			name:    "vlan changed",
			prev:    spec(br("br0", port("eth0", 100))),
			current: spec(br("br0", port("eth0", 300))),
			want:    []string{"eth0.100"},
		},
		{
			name:    "parent interface changed",
			prev:    spec(br("br0", port("eth0", 100))),
			current: spec(br("br0", port("eth1", 100))),
			want:    []string{"eth0.100"},
		},
		{
			name: "bridge mtu changed",
			prev: &networkv1alpha1.NetworkAttachmentSpec{Bridge: []networkv1alpha1.Bridge{
				{Name: "br0", Mtu: 1500, Ports: []networkv1alpha1.Port{port("eth0", 100)}}}},
			current: &networkv1alpha1.NetworkAttachmentSpec{Bridge: []networkv1alpha1.Bridge{
				{Name: "br0", Mtu: 9000, Ports: []networkv1alpha1.Port{port("eth0", 100)}}}},
			want: nil,
		},
		{
			name:    "bridge renamed",
			prev:    spec(br("br0", port("eth0", 100))),
			current: spec(br("br1", port("eth0", 100))),
			want:    nil,
		},
		{
			name:    "bridge removed",
			prev:    spec(br("br0", port("eth0", 100)), br("br1", port("eth1", 10), port("eth1", 20))),
			current: spec(br("br0", port("eth0", 100))),
			want:    []string{"eth1.10", "eth1.20"},
		},
		{
			name:    "all bridges removed",
			prev:    spec(br("br0", port("eth0", 100))),
			current: spec(),
			want:    []string{"eth0.100"},
		},
		{
			name:    "port removed from the second bridge",
			prev:    spec(br("br0", port("eth0", 100)), br("br1", port("eth1", 10), port("eth1", 20))),
			current: spec(br("br0", port("eth0", 100)), br("br1", port("eth1", 10))),
			want:    []string{"eth1.20"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := portDiff(tt.prev, tt.current)
			if err != nil {
				t.Fatalf("portDiff() error = %v", err)
			}
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("portDiff() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStrToInt(t *testing.T) {
	if got, err := strToInt("42"); err != nil || got != 42 {
		t.Errorf("strToInt(\"42\") = %d, %v, want 42, nil", got, err)
	}
	if _, err := strToInt("Name"); err == nil {
		t.Error("strToInt(\"Name\") should fail")
	}
}

func TestNewNetworkAttachment(t *testing.T) {
	network := &networkv1alpha1.Network{
		ObjectMeta: metav1.ObjectMeta{Name: "vlan100", UID: types.UID("uid-1")},
		Spec: networkv1alpha1.NetworkSpec{
			Bridge: []networkv1alpha1.Bridge{{Name: "br0", Ports: []networkv1alpha1.Port{{Name: "eth0", Vlan: 100}}}},
			Routes: []networkv1alpha1.Route{{Via: "br0", Destination: "10.0.0.0/24"}},
			IpMasq: networkv1alpha1.Masquerade{Enabled: true, Source: "10.0.0.0/24", Bridge: "br0"},
		},
	}
	req := ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "default", Name: "vlan100"}}

	na, err := NewNetworkAttachment("node-1", network, req)
	if err != nil {
		t.Fatalf("NewNetworkAttachment() error = %v", err)
	}

	if na.Name != "node-1-vlan100" {
		t.Errorf("name = %q, want node-1-vlan100", na.Name)
	}
	if na.Namespace != "default" {
		t.Errorf("namespace = %q, want default", na.Namespace)
	}
	if na.Spec.NodeName != "node-1" {
		t.Errorf("spec.nodeName = %q, want node-1", na.Spec.NodeName)
	}
	if !reflect.DeepEqual(na.Spec.Bridge, network.Spec.Bridge) ||
		!reflect.DeepEqual(na.Spec.Routes, network.Spec.Routes) ||
		!reflect.DeepEqual(na.Spec.IpMasq, network.Spec.IpMasq) {
		t.Errorf("spec was not copied from the network: %+v", na.Spec)
	}

	if len(na.OwnerReferences) != 1 {
		t.Fatalf("owner references = %d, want 1", len(na.OwnerReferences))
	}
	owner := na.OwnerReferences[0]
	if owner.Kind != "Network" || owner.Name != "vlan100" || owner.UID != "uid-1" {
		t.Errorf("owner reference = %+v, want the Network vlan100", owner)
	}
	if owner.Controller == nil || !*owner.Controller {
		t.Errorf("owner reference is not marked as controller")
	}
}

func TestFilterNetworkAttachmentEvent(t *testing.T) {
	na := func(vlan int) *networkv1alpha1.NetworkAttachment {
		return &networkv1alpha1.NetworkAttachment{
			Spec: networkv1alpha1.NetworkAttachmentSpec{
				Bridge: []networkv1alpha1.Bridge{{Name: "br0", Ports: []networkv1alpha1.Port{{Name: "eth0", Vlan: vlan}}}},
			},
		}
	}
	deleting := na(100)
	now := metav1.Now()
	deleting.DeletionTimestamp = &now

	tests := []struct {
		name     string
		old, new client.Object
		want     bool
	}{
		{name: "spec unchanged", old: na(100), new: na(100), want: false},
		{name: "spec changed", old: na(100), new: na(200), want: true},
		{name: "being deleted", old: na(100), new: deleting, want: true},
		{name: "not a network attachment", old: &networkv1alpha1.Network{}, new: &networkv1alpha1.Network{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}
			if got := filterNetworkAttachmentEvent(e); got != tt.want {
				t.Errorf("filterNetworkAttachmentEvent() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetNamespacedNetworkName(t *testing.T) {
	tests := []struct {
		in   string
		want types.NamespacedName
	}{
		{in: "vlan100", want: types.NamespacedName{Name: "vlan100"}},
		{in: "default/vlan100", want: types.NamespacedName{Namespace: "default", Name: "vlan100"}},
	}

	for _, tt := range tests {
		if got := getNamespacedNetworkName(tt.in); got != tt.want {
			t.Errorf("getNamespacedNetworkName(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func vmi(state *virtv1.VirtualMachineInstanceMigrationState) *virtv1.VirtualMachineInstance {
	return &virtv1.VirtualMachineInstance{
		ObjectMeta: metav1.ObjectMeta{Name: "vm-1"},
		Status:     virtv1.VirtualMachineInstanceStatus{MigrationState: state},
	}
}

func TestMigrationSuccessful(t *testing.T) {
	tests := []struct {
		name  string
		state *virtv1.VirtualMachineInstanceMigrationState
		want  bool
	}{
		{name: "never migrated", state: nil, want: false},
		{name: "in progress", state: &virtv1.VirtualMachineInstanceMigrationState{}, want: false},
		{name: "completed", state: &virtv1.VirtualMachineInstanceMigrationState{Completed: true}, want: true},
		{name: "aborted", state: &virtv1.VirtualMachineInstanceMigrationState{Completed: true, Failed: true}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := migrationSuccessful(*vmi(tt.state)); got != tt.want {
				t.Errorf("migrationSuccessful() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilterVirtualMachineMigrationEvents(t *testing.T) {
	t.Setenv(nodeName, "node-1")

	completedOn := func(node string) *virtv1.VirtualMachineInstanceMigrationState {
		return &virtv1.VirtualMachineInstanceMigrationState{Completed: true, TargetNode: node}
	}
	inProgress := &virtv1.VirtualMachineInstanceMigrationState{TargetNode: "node-1"}

	tests := []struct {
		name     string
		old, new client.Object
		want     bool
	}{
		{name: "migration completed on this node", old: vmi(inProgress), new: vmi(completedOn("node-1")), want: true},
		{name: "migration completed on another node", old: vmi(inProgress), new: vmi(completedOn("node-2")), want: false},
		{name: "migration still in progress", old: vmi(nil), new: vmi(inProgress), want: false},
		{name: "already completed before", old: vmi(completedOn("node-1")), new: vmi(completedOn("node-1")), want: false},
		{name: "not a virtual machine instance", old: &networkv1alpha1.Network{}, new: &networkv1alpha1.Network{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := event.UpdateEvent{ObjectOld: tt.old, ObjectNew: tt.new}
			if got := filterVirtualMachineMigrationEvents(e); got != tt.want {
				t.Errorf("filterVirtualMachineMigrationEvents() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetHostname(t *testing.T) {
	t.Setenv(nodeName, "node-from-env")
	if got, err := getHostname(); err != nil || got != "node-from-env" {
		t.Errorf("getHostname() = %q, %v, want node-from-env, nil", got, err)
	}

	t.Setenv(nodeName, "")
	got, err := getHostname()
	if err != nil {
		t.Fatalf("getHostname() error = %v", err)
	}
	if got == "" {
		t.Error("getHostname() should fall back to the OS hostname")
	}
}
