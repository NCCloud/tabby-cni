package bridge

import (
	"fmt"
	"syscall"

	"github.com/vishvananda/netlink"
)

type Bridge struct {
	Name string
	Mtu  int
}

func Create(name string, mtu int) (*netlink.Bridge, error) {

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
			MTU:  mtu,
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

func Remove(name string) error {
	br, err := netlink.LinkByName(name)
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
