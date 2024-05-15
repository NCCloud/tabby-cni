package bridge

import (
	"fmt"
	"os"
	"syscall"

	"github.com/vishvananda/netlink"
)

const SYSFS_PATH = "/sys/class/net/%s/brif/"

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

	ports, err := BridgeListPorts(bridge.Name)
	if err != nil {
		return err
	}

	if len(ports) > 0 {
		// Remove port that was added by ourself since it will be
		// deleted together with the bridge and could be ignored.
		for _, port := range bridge.Ports {
			p := port.Name
			if port.Vlan > 0 {
				p = fmt.Sprintf("%s.%d", port.Name, port.Vlan)
			}

			ports = removeElement(ports, p)
		}

		if len(ports) > 0 {
			return fmt.Errorf("unable to delete bridge, there is still attached interface %s", ports)
		}
	}

	if err = netlink.LinkDel(br); err != nil {
		return err
	}

	return nil
}

func BridgeListPorts(name string) ([]string, error) {
	var ports []string

	files, err := os.ReadDir(fmt.Sprintf(SYSFS_PATH, name))
	if err != nil {
		return nil, fmt.Errorf("unable to find bridge path %w", err)
	}

	for _, file := range files {
		ports = append(ports, file.Name())
	}

	return ports, nil
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
		return fmt.Errorf("only vlan interface could be removed: name: %s, type: %s", name, port.Type())
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
		err = fmt.Errorf("failed to find a link by name %s: %v", iface, err)
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
		err = fmt.Errorf("failed to add a new link device vlan=%+v, error=%v", vlan, err)
		return nil, err
	}

	if err = netlink.LinkSetUp(vlan); err != nil {
		err = fmt.Errorf("failed to enable the link vlan=%+v, error=%v", vlan, err)
		return nil, err
	}

	return vlan, nil
}

func removeElement(s []string, value string) []string {
	var idx = -1

	for i, v := range s {
		if v == value {
			idx = i
			break
		}
	}

	// Return slice as is if there is no matching
	if idx < 0 {
		return s
	}

	s[idx] = s[len(s)-1]
	return s[:len(s)-1]
}
