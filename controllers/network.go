package controllers

import (
	"fmt"
	"syscall"

	"net"

	"github.com/vishvananda/netlink"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
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

func addRoute(route networkv1alpha1.Route) error {
	var src_ip net.IP

	iface, err := netlink.LinkByName(route.Device)
	if err != nil {
		return err
	}
	_, dst, err := net.ParseCIDR(route.Destination)
	if err != nil {
		return err
	}

	_, src, err := net.ParseCIDR(route.Source)
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
		return fmt.Errorf(fmt.Sprintf("Could not find src ip for network %s on host", route.Source))
	}

	err = netlink.RouteAdd(&netlink.Route{
		LinkIndex: iface.Attrs().Index,
		Scope:     netlink.SCOPE_LINK,
		Dst:       dst,
		Src:       src_ip,
	})
	if err != nil && err != syscall.EEXIST {
		return err
	}

	return nil
}
