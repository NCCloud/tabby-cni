package controllers

import (
	"context"
	"fmt"
	"syscall"

	"net"

	"github.com/vishvananda/netlink"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/NCCloud/tabby-cni/pkg/bridge"
)

func EqualCIDR(a, b *net.IPNet) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if !a.IP.Equal(b.IP) {
		return false
	}
	if !net.IP(a.Mask).Equal(net.IP(b.Mask)) {
		return false
	}
	return true
}

func addRoute(r networkv1alpha1.Route) error {
	var src_ip net.IP
	var route netlink.Route

	_, dst, err := net.ParseCIDR(r.Destination)
	if err != nil {
		return err
	}

	gw := net.ParseIP(r.Via)
	// check if via ip address or device
	if gw == nil {
		iface, err := netlink.LinkByName(r.Via)
		if err != nil {
			return err
		}
		route = netlink.Route{LinkIndex: iface.Attrs().Index, Scope: netlink.SCOPE_LINK}
	} else {
		route = netlink.Route{Scope: netlink.SCOPE_UNIVERSE, Gw: gw}
	}

	fmt.Println(len(r.Source))

	if r.Source != "" {
		_, src, err := net.ParseCIDR(r.Source)
		if err != nil {
			return err
		}

		routeList, err := netlink.RouteList(nil, netlink.FAMILY_V4)
		if err != nil {
			return err
		}

		for _, r := range routeList {
			if EqualCIDR(r.Dst, src) {
				src_ip = r.Src
			}
		}

		if src_ip == nil {
			return fmt.Errorf(fmt.Sprintf("Could not find src ip for network %s on host", r.Source))
		}

		route.Src = src_ip
	}

	route.Dst = dst

	err = netlink.RouteAdd(&route)
	if err != nil && err != syscall.EEXIST {
		return err
	}

	return nil
}

func CreateNetwork(ctx context.Context, spec *networkv1alpha1.NetworkAttachmentSpec) error {
	_ = log.FromContext(ctx)

	// Create network resources
	// Create linux bridge
	for _, bridge_spec := range spec.Bridge {
		br, err := (&bridge.Bridge{Name: bridge_spec.Name, Mtu: bridge_spec.Mtu}).Create()
		if err != nil {
			return err
		}
		// Add vlan to the interface
		for _, port_spec := range bridge_spec.Ports {
			vlan, err := bridge.AddVlan(port_spec.Name, port_spec.Vlan, port_spec.Mtu)
			if err != nil {
				log.Log.Error(err, fmt.Sprintf("failed to add vlan %d to interface %s", port_spec.Vlan, port_spec.Name))
				return err
			}

			// Attach vlan interface to the linux bridge
			if err := netlink.LinkSetMaster(vlan, br); err != nil {
				log.Log.Error(err, fmt.Sprintf("failed to add interface %s to the bridge %s", vlan.Name, br.Name))
				return err
			}
		}
	}

	// Add static routes
	for _, route := range spec.Routes {
		if err := addRoute(route); err != nil {
			log.Log.Error(err, "Failed to add static routes")
			return err
		}
	}

	// Add or remove snat firewall rules
	if spec.IpMasq.Enabled {
		if err := EnableMasquerade(&spec.IpMasq); err != nil {
			log.Log.Error(err, fmt.Sprintf("failed to add masquerade: %v", spec.IpMasq))
			return err
		}
	}

	return nil
}

func DeleteNetwork(ctx context.Context, spec *networkv1alpha1.NetworkAttachmentSpec) error {
	var pName string
	// Remove linux bridge
	for _, br := range spec.Bridge {
		for _, port := range br.Ports {
			pName = port.Name

			if port.Vlan != 0 {
				pName = fmt.Sprintf("%s.%d", port.Name, port.Vlan)
			}

			if err := bridge.DeletePort(pName); err != nil {
				log.Log.Error(err, fmt.Sprintf("NetworkAttachment: Unable to delete port from linux bridge %s", port.Name))
				return err
			}
		}

		// TBD check if there is no attached interfaces and only after that remove linux bridge
		if err := (&bridge.Bridge{Name: br.Name}).Remove(); err != nil {
			return err
		}
	}

	// Remove iptables rules
	if spec.IpMasq.Enabled {
		if err := DeleteMasquerade(&spec.IpMasq); err != nil {
			return err
		}
	}

	// TBD delete static routes

	return nil
}
