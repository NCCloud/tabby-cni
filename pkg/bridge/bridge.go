package bridge

import (
	"fmt"
	"syscall"

	"github.com/vishvananda/netlink"
)

type Bridge struct {
	Name  string
	Mtu   int
	Ports []Port
}

type Port struct {
	Name string
	Mtu  int
	Vlan int
}

func (bridge *Bridge) Create() (*netlink.Bridge, error) {

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: bridge.Name,
			MTU:  bridge.Mtu,
			// Let kernel use default txqueuelen; leaving it unset
			// means 0, and a zero-length TX queue messes up FIFO
			// traffic shapers which use TX queue length as the
			// default packet limit
			TxQLen: -1,
		},
	}

	err := netlink.LinkAdd(br)
	if err != nil && err != syscall.EEXIST {
		return nil, err
	}

	if err = netlink.LinkSetUp(br); err != nil {
		return nil, err
	}

	return br, nil
}

func (bridge *Bridge) Remove() error {
	br, err := netlink.LinkByName(bridge.Name)
	if err != nil {
		if err.Error() == "Link not found" {
			return nil
		}
		return err
	}

	if err = netlink.LinkDel(br); err != nil {
		return err
	}

	return nil
}

func (bridge *Bridge) AddPort(port *Port) error {
	if port.Mtu == 0 {
		port.Mtu = bridge.Mtu
	}

	if port.Vlan == 0 {

	} else {
		AddVlan(port.Name, port.Vlan, port.Mtu)
	}

	fmt.Println(port.Mtu)
	return nil
}

func DeletePort(name string) error {
	port, err := netlink.LinkByName(name)
	if err != nil {
		if err.Error() == "Link not found" {
			return nil
		}
		return err
	}

	// Allow to remove vlan interface for now
	if port.Type() != "vlan" {
		return fmt.Errorf("Only vlan interface could be removed: name: %s, type: %s", name, port.Type())
	}

	if err = netlink.LinkSetNoMaster(port); err != nil {
		return err
	}

	if err = netlink.LinkDel(port); err != nil {
		return err
	}

	return nil
}

func AddVlan(iface string, vlanId int, mtu int) (*netlink.Vlan, error) {

	parentLink, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, err
	}

	vlan := &netlink.Vlan{
		VlanId: vlanId,
		LinkAttrs: netlink.LinkAttrs{
			Name:        fmt.Sprintf("%s.%d", iface, vlanId),
			MTU:         mtu,
			TxQLen:      -1,
			ParentIndex: parentLink.Attrs().Index,
		},
	}

	err = netlink.LinkAdd(vlan)
	if err != nil && err != syscall.EEXIST {
		return nil, err
	}

	if err = netlink.LinkSetUp(vlan); err != nil {
		return nil, err
	}

	return vlan, nil
}
