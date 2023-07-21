package controllers

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

type virtualRouter struct {
	Name string
	//TBD
}

func NewVirtualRouter(name string) *virtualRouter {
	return &virtualRouter{Name: name}
}

func (vr *virtualRouter) Create() error {
	var err error

	// Create linux network namespace
	err = CreateNamespace(vr.Name)
	if err != nil {
		return err
	}

	fmt.Printf("vrouter namespace has been created %s\n", vr.Name)

	return nil
}

func (vr *virtualRouter) AttachInterface(iface string) error {

	fmt.Println(iface)

	/*	if err := WithNetNS(vr.Namespace, func() error {
			return netlink.LinkSetNsFd(iface, int(vr.Namespace))
		}); err != nil {
			fmt.Printf("failed to move veth to container netns: %s \n", err)
		}
	*/
	ns, _ := netns.GetFromName(vr.Name)

	link, err := netlink.LinkByName(iface)
	if err == nil {
		if err := netlink.LinkSetNsFd(link, int(ns)); err != nil {
			fmt.Println(err)
		}
	}

	if err := vr.WithNetNS(func() error {
		if err := setLinkUp(iface); err != nil {
			fmt.Println(err)
			return nil
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (vr *virtualRouter) Delete(name string) error {
	// TBD
	return nil
}

func CreateNamespace(name string) error {
	// Lock the OS Thread so we don't accidentally switch namespaces
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Save the current network namespace
	oldNs, _ := netns.Get()
	defer oldNs.Close()

	// Create a new network namespace
	ns, err := netns.NewNamed(name)
	defer ns.Close()

	if err != nil {
		if errors.Is(err, syscall.EEXIST) {
			fmt.Printf("Namespace already exists, nothing to do: %s\n", err)
		} else {
			fmt.Printf("Failed to create namespace: %s\n", err)
			return err
		}
	}

	netns.Set(oldNs)

	return nil
}

func setLinksUp(iface ...string) error {
	for _, link := range iface {
		if err := setLinkUp(link); err != nil {
			return err
		}
	}
	return nil
}

func setLinkUp(iface string) error {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		fmt.Printf("Link is not found %s\n", err)
		return nil
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}

	return nil
}

func CreateVethPair(prefix string) (*netlink.Veth, error) {
	// Create veth pair and add it to ns
	name, peerName := "veth-"+prefix+"-ns", "veth-"+prefix+"-gl"

	veth := &netlink.Veth{
		LinkAttrs: netlink.LinkAttrs{
			Name: name,
			MTU:  1500},
		PeerName: peerName,
	}
	if err := netlink.LinkAdd(veth); err != nil {
		if errors.Is(err, syscall.EEXIST) {
			fmt.Printf("Veth pair already exists, nothing to do: %s\n", err)
		} else {
			fmt.Printf("could not create veth pair %s-%s: %s", "test", "test\n", err)
			return nil, err
		}
	}

	if err := setLinksUp(name, peerName); err != nil {
		return nil, err
	}
	return veth, nil
}

func (vr *virtualRouter) WithNetNS(task func() error) error {
	return WithNetNS(vr.Name, task)
}

func WithNetNS(name string, task func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	ns, _ := netns.GetFromName(name)

	oldNs, err := netns.Get()
	if err == nil {
		defer oldNs.Close()

		err = netns.Set(ns)
		if err == nil {
			defer ns.Close()

			err = task()
		}
	}
	netns.Set(oldNs)

	return err
}

/*
func main() {

	vrouter := NewVirtualRouter("new-10")
	if err := vrouter.Create(); err != nil {
		fmt.Printf("Failed to create vrouter %s\n", err)
	}

	veth, err := CreateVethPair(vrouter.Name)
	if err != nil {
		fmt.Printf("Failed to create veth pair %s\n", err)
	}
		if err = vrouter.AttachInterface(veth); err != nil {
			fmt.Printf("Failed to add interface to namespace %s\n", err)
		}
	br, err := netlink.LinkByName("br-mgmt")
	if err != nil {
		fmt.Printf("Failed to get linux bridge %s\n", err)
	}

	peerName, err := netlink.LinkByName(veth.PeerName)
	if err != nil {
		fmt.Printf("Failed to get linux veth pair %s\n", err)
	}

	// Attach vlan interface to the linux bridge
	if err := netlink.LinkSetMaster(peerName, br); err != nil {
		fmt.Println(err)
	}

	WithNetNS("new-10", func() error {
		fmt.Println(net.Interfaces())
		return nil
	})
}
*/
