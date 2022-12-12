// Copyright 2019 CNI authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This is a "meta-plugin". It reads in its own netconf.

package main

import (
	"encoding/json"
	"fmt"
	"net"
	"syscall"

	"github.com/tabby-cni/pkg/ebtables"
	"github.com/tabby-cni/pkg/iptables"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

const (
	IPv4InterfaceArpProxySysctlTemplate = "net.ipv4.conf.%s.proxy_arp"
	ipv4Forward                         = "net.ipv4.ip_forward"
	virtualIpaddress                    = "169.254.1.1"
)

type Route struct {
	Dev string `json:"dev"`
	Src string `json:"src"`
	Dst string `json:"dst"`
}

type Masquerade struct {
	Enabled bool     `json:"enabled"`
	Source  string   `json:"source"`
	Ignore  []string `json:"ignore"`
}

type cniArgs struct {
	types.NetConf

	PrevResult *current.Result `json:"-"`

	Mtu       int        `json:"mtu"`
	Vlan      int        `json:"vlan"`
	Bridge    string     `json:"bridge"`
	Interface string     `json:"interface"`
	IpMasq    Masquerade `json:"ipMasq"`
	Routes    []Route    `json:"routes"`
}

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

func parseConf(data []byte, envArgs string) (*cniArgs, error) {
	conf := cniArgs{Mtu: 1500}

	if err := json.Unmarshal(data, &conf); err != nil {
		return nil, fmt.Errorf("failed to load netconf: %v", err)
	}

	return &conf, nil
}

func addRoute(route Route) error {
	var src_ip net.IP

	iface, err := netlink.LinkByName(route.Dev)
	if err != nil {
		return err
	}
	_, dst, err := net.ParseCIDR(route.Dst)
	if err != nil {
		return err
	}

	_, src, err := net.ParseCIDR(route.Src)
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
		return fmt.Errorf(fmt.Sprintf("Could not find src ip for network %s on host", route.Src))
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

func addVlan(iface string, vlanId int, mtu int) (*netlink.Vlan, error) {

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

func addBridge(bridge string, mtu int) (*netlink.Bridge, error) {

	br := &netlink.Bridge{
		LinkAttrs: netlink.LinkAttrs{
			Name: bridge,
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

func EnableMasquerade(name string, ipmasq *Masquerade, bridge string, iface string, vlan int) error {
	vlanIface := fmt.Sprintf("%s.%d", iface, vlan)

	ipv4SysctlValueName := fmt.Sprintf(IPv4InterfaceArpProxySysctlTemplate, bridge)
	if _, err := sysctl.Sysctl(ipv4SysctlValueName, "1"); err != nil {
		return fmt.Errorf("failed to set proxy_arp on newly added interface %s: %v", bridge, err)
	}

	if _, err := sysctl.Sysctl(ipv4Forward, "1"); err != nil {
		return fmt.Errorf("failed to set ip_forward=1: %v", err)
	}

	/*
	 Make sure arp request won't go outside of compute node
	 ebtables-nft -I FORWARD -p ARP -o wlp2s0.2000 --arp-ip-dst 169.254.1.1 -j DROP
	*/
	rule := []string{"-p", "ARP", "-o", vlanIface, "--arp-ip-dst", virtualIpaddress, "-j", "DROP"}

	if err := ebtables.AddRule(rule...); err != nil {
		return err
	}

	if err := iptables.AddRule(name, ipmasq.Source, ipmasq.Ignore); err != nil {
		return err
	}

	return nil
}

func cmdAdd(args *skel.CmdArgs) error {

	conf, err := parseConf(args.StdinData, args.Args)
	if err != nil {
		logrus.WithError(err).Error("failed to parse config")
		return err
	}

	br, err := addBridge(conf.Bridge, conf.Mtu)
	if err != nil {
		logrus.WithError(err).Error("failed to create bridge %s", conf.Bridge)
		return err
	}

	vlan, err := addVlan(conf.Interface, conf.Vlan, conf.Mtu)
	if err != nil {
		logrus.WithError(err).Error("failed to add vlan %d to interface %s", conf.Vlan, conf.Interface)
		return err
	}

	if err := netlink.LinkSetMaster(vlan, br); err != nil {
		logrus.WithError(err).Error("failed to add interface %s to the bridge %s", vlan.Name, br.Name)
		return err
	}

	for _, route := range conf.Routes {
		if err = addRoute(route); err != nil {
			logrus.WithError(err).Error("failed to add route: %v", conf.Routes)
			return err
		}
	}

	if conf.IpMasq.Enabled {
		if err = EnableMasquerade(conf.Name, &conf.IpMasq, conf.Bridge, conf.Interface, conf.Vlan); err != nil {
			logrus.WithError(err).Error("failed to add masquerade: %v", conf.IpMasq)
			return err
		}
	}

	return types.PrintResult(&current.Result{}, "0.3.1")
}

func cmdDel(args *skel.CmdArgs) error {
	return nil
}

func cmdCheck(args *skel.CmdArgs) error {
	return nil
}

func main() {
	skel.PluginMain(cmdAdd, cmdCheck, cmdDel, version.All, "TODO")
}
