package bridge

import (
	"net"
	"reflect"
	"sort"
	"testing"

	"github.com/NCCloud/tabby-cni/internal/testutil"
	"github.com/vishvananda/netlink"
)

func TestRemoveElement(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		value string
		want  []string
	}{
		{name: "removes the value", input: []string{"a", "b", "c"}, value: "b", want: []string{"a", "c"}},
		{name: "removes the last value", input: []string{"a", "b"}, value: "b", want: []string{"a"}},
		{name: "keeps the slice when the value is missing", input: []string{"a", "b"}, value: "x", want: []string{"a", "b"}},
		{name: "handles an empty slice", input: []string{}, value: "a", want: []string{}},
		{name: "removes only the first match", input: []string{"a", "a"}, value: "a", want: []string{"a"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := removeElement(tt.input, tt.value)
			sort.Strings(got)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("removeElement(%v, %q) = %v, want %v", tt.input, tt.value, got, tt.want)
			}
		})
	}
}

func TestBridgeCreateAndRemove(t *testing.T) {
	testutil.RequireIntegration(t)
	t.Cleanup(func() { testutil.DeleteLink("tbr-create") })

	br := &Bridge{Name: "tbr-create", Mtu: 1400}

	created, err := br.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Attrs().Name != "tbr-create" {
		t.Errorf("created bridge name = %q, want %q", created.Attrs().Name, "tbr-create")
	}

	link, err := netlink.LinkByName("tbr-create")
	if err != nil {
		t.Fatalf("bridge was not created: %v", err)
	}
	if link.Type() != "bridge" {
		t.Errorf("link type = %q, want bridge", link.Type())
	}
	if link.Attrs().MTU != 1400 {
		t.Errorf("bridge MTU = %d, want 1400", link.Attrs().MTU)
	}
	if link.Attrs().Flags&net.FlagUp == 0 {
		t.Errorf("bridge is not up")
	}

	// Creating an existing bridge is not an error.
	if _, err := br.Create(); err != nil {
		t.Fatalf("second Create() error = %v", err)
	}

	if err := br.Remove(); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if testutil.LinkExists(t, "tbr-create") {
		t.Errorf("bridge still exists after Remove()")
	}

	// Removing a missing bridge is not an error.
	if err := br.Remove(); err != nil {
		t.Fatalf("Remove() of a missing bridge error = %v", err)
	}
}

func TestAddVlanAndBridgePorts(t *testing.T) {
	testutil.RequireIntegration(t)
	testutil.AddDummy(t, "tvl0", "")
	t.Cleanup(func() {
		testutil.DeleteLink("tvl0.100")
		testutil.DeleteLink("tbr-ports")
	})

	vlan, err := AddVlan("tvl0", 100, 1450)
	if err != nil {
		t.Fatalf("AddVlan() error = %v", err)
	}
	if vlan.Attrs().Name != "tvl0.100" {
		t.Errorf("vlan name = %q, want tvl0.100", vlan.Attrs().Name)
	}

	link, err := netlink.LinkByName("tvl0.100")
	if err != nil {
		t.Fatalf("vlan interface was not created: %v", err)
	}
	v, ok := link.(*netlink.Vlan)
	if !ok {
		t.Fatalf("link type = %q, want vlan", link.Type())
	}
	if v.VlanId != 100 {
		t.Errorf("vlan id = %d, want 100", v.VlanId)
	}
	if v.Attrs().MTU != 1450 {
		t.Errorf("vlan MTU = %d, want 1450", v.Attrs().MTU)
	}

	// Adding the same vlan again is not an error.
	if _, err := AddVlan("tvl0", 100, 1450); err != nil {
		t.Fatalf("second AddVlan() error = %v", err)
	}

	br := &Bridge{Name: "tbr-ports", Ports: []Port{{Name: "tvl0", Vlan: 100}}}
	created, err := br.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := netlink.LinkSetMaster(link, created); err != nil {
		t.Fatalf("failed to attach vlan to bridge: %v", err)
	}

	ports, err := BridgeListPorts("tbr-ports")
	if err != nil {
		t.Fatalf("BridgeListPorts() error = %v", err)
	}
	if !reflect.DeepEqual(ports, []string{"tvl0.100"}) {
		t.Errorf("BridgeListPorts() = %v, want [tvl0.100]", ports)
	}

	// The bridge can be removed because its only port is one we own.
	if err := br.Remove(); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if testutil.LinkExists(t, "tbr-ports") {
		t.Errorf("bridge still exists after Remove()")
	}
}

func TestPinHardwareAddr(t *testing.T) {
	testutil.RequireIntegration(t)
	uplink := addDummyWithMAC(t, "tpin-up0", "02:00:00:00:00:0a")
	vm := addDummyWithMAC(t, "tpin-vm0", "02:00:00:00:00:05")
	lowerVM := addDummyWithMAC(t, "tpin-vm1", "02:00:00:00:00:01")
	t.Cleanup(func() { testutil.DeleteLink("tbr-pin") })

	created, err := (&Bridge{Name: "tbr-pin"}).Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	attach := func(port netlink.Link) {
		t.Helper()
		if err := netlink.LinkSetMaster(port, created); err != nil {
			t.Fatalf("failed to attach %s to the bridge: %v", port.Attrs().Name, err)
		}
	}
	detach := func(port netlink.Link) {
		t.Helper()
		if err := netlink.LinkSetNoMaster(port); err != nil {
			t.Fatalf("failed to detach %s from the bridge: %v", port.Attrs().Name, err)
		}
	}

	attach(uplink)
	attach(vm)

	// Without pinning, the bridge follows its lowest port address. This is the
	// behaviour being fixed: the VM port leaving changes the bridge address.
	if got := linkMAC(t, "tbr-pin"); got != "02:00:00:00:00:05" {
		t.Fatalf("unpinned bridge MAC = %s, want the lowest port MAC 02:00:00:00:00:05", got)
	}
	detach(vm)
	if got := linkMAC(t, "tbr-pin"); got != "02:00:00:00:00:0a" {
		t.Fatalf("unpinned bridge MAC after the lowest port left = %s, want 02:00:00:00:00:0a", got)
	}
	attach(vm)

	pinned, err := PinHardwareAddr("tbr-pin")
	if err != nil {
		t.Fatalf("PinHardwareAddr() error = %v", err)
	}
	if !pinned {
		t.Errorf("PinHardwareAddr() = false, want true for an unpinned bridge")
	}
	if got := linkMAC(t, "tbr-pin"); got != "02:00:00:00:00:05" {
		t.Errorf("PinHardwareAddr() changed the bridge MAC to %s, want it kept at 02:00:00:00:00:05", got)
	}
	if got, err := addrAssignType("tbr-pin"); err != nil || got != netAddrSet {
		t.Errorf("addr_assign_type = %d (err %v), want %d", got, err, netAddrSet)
	}

	// Pinning an already pinned bridge does nothing.
	if pinned, err := PinHardwareAddr("tbr-pin"); err != nil || pinned {
		t.Errorf("second PinHardwareAddr() = %v, %v, want false, nil", pinned, err)
	}

	// The address no longer follows the ports.
	detach(vm)
	if got := linkMAC(t, "tbr-pin"); got != "02:00:00:00:00:05" {
		t.Errorf("pinned bridge MAC after a port left = %s, want 02:00:00:00:00:05", got)
	}
	attach(lowerVM)
	if got := linkMAC(t, "tbr-pin"); got != "02:00:00:00:00:05" {
		t.Errorf("pinned bridge MAC after a lower port joined = %s, want 02:00:00:00:00:05", got)
	}
}

func TestPinHardwareAddrMissingBridge(t *testing.T) {
	testutil.RequireIntegration(t)

	if _, err := PinHardwareAddr("tmissing-br"); err == nil {
		t.Fatal("PinHardwareAddr() of a missing bridge should fail")
	}
}

func addDummyWithMAC(t *testing.T, name string, mac string) netlink.Link {
	t.Helper()

	link := testutil.AddDummy(t, name, "")
	hwAddr, err := net.ParseMAC(mac)
	if err != nil {
		t.Fatalf("failed to parse MAC %s: %v", mac, err)
	}
	if err := netlink.LinkSetHardwareAddr(link, hwAddr); err != nil {
		t.Fatalf("failed to set MAC %s on %s: %v", mac, name, err)
	}

	return link
}

func linkMAC(t *testing.T, name string) string {
	t.Helper()

	link, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatalf("failed to find %s: %v", name, err)
	}

	return link.Attrs().HardwareAddr.String()
}

func TestAddVlanMissingParent(t *testing.T) {
	testutil.RequireIntegration(t)

	if _, err := AddVlan("tmissing0", 10, 1500); err == nil {
		t.Fatal("AddVlan() with a missing parent interface should fail")
	}
}

func TestBridgeRemoveWithForeignPort(t *testing.T) {
	testutil.RequireIntegration(t)
	foreign := testutil.AddDummy(t, "tforeign0", "")
	t.Cleanup(func() { testutil.DeleteLink("tbr-foreign") })

	br := &Bridge{Name: "tbr-foreign"}
	created, err := br.Create()
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := netlink.LinkSetMaster(foreign, created); err != nil {
		t.Fatalf("failed to attach interface to bridge: %v", err)
	}

	if err := br.Remove(); err == nil {
		t.Fatal("Remove() should fail while an interface we do not own is attached")
	}
	if !testutil.LinkExists(t, "tbr-foreign") {
		t.Errorf("bridge was removed although a foreign interface is attached")
	}
}

func TestDeletePort(t *testing.T) {
	testutil.RequireIntegration(t)
	testutil.AddDummy(t, "tdp0", "")
	t.Cleanup(func() { testutil.DeleteLink("tdp0.20") })

	if _, err := AddVlan("tdp0", 20, 1500); err != nil {
		t.Fatalf("AddVlan() error = %v", err)
	}

	if err := DeletePort("tdp0.20"); err != nil {
		t.Fatalf("DeletePort() error = %v", err)
	}
	if testutil.LinkExists(t, "tdp0.20") {
		t.Errorf("vlan interface still exists after DeletePort()")
	}

	// Deleting a missing port is not an error.
	if err := DeletePort("tdp0.20"); err != nil {
		t.Fatalf("DeletePort() of a missing port error = %v", err)
	}

	// Only vlan interfaces can be deleted.
	if err := DeletePort("tdp0"); err == nil {
		t.Fatal("DeletePort() of a non-vlan interface should fail")
	}
	if !testutil.LinkExists(t, "tdp0") {
		t.Errorf("non-vlan interface was deleted")
	}
}

func TestAddPort(t *testing.T) {
	testutil.RequireIntegration(t)
	testutil.AddDummy(t, "tap0", "")
	t.Cleanup(func() { testutil.DeleteLink("tap0.40") })

	br := &Bridge{Name: "tbr-addport", Mtu: 1420}

	// The port MTU defaults to the bridge MTU.
	port := &Port{Name: "tap0", Vlan: 40}
	if err := br.AddPort(port); err != nil {
		t.Fatalf("AddPort() error = %v", err)
	}
	link, err := netlink.LinkByName("tap0.40")
	if err != nil {
		t.Fatalf("vlan interface was not created: %v", err)
	}
	if link.Attrs().MTU != 1420 {
		t.Errorf("vlan MTU = %d, want the bridge MTU 1420", link.Attrs().MTU)
	}

	// A port without a vlan does not create anything.
	if err := br.AddPort(&Port{Name: "tap0"}); err != nil {
		t.Fatalf("AddPort() without vlan error = %v", err)
	}

	// Errors from creating the vlan are returned.
	if err := br.AddPort(&Port{Name: "tmissing0", Vlan: 40}); err == nil {
		t.Fatal("AddPort() with a missing parent interface should fail")
	}
}
