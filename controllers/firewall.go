package controllers

import (
	"fmt"
	"net"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/NCCloud/tabby-cni/pkg/ebtables"
	"github.com/NCCloud/tabby-cni/pkg/iptables"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
	"github.com/j-keck/arping"
)

const (
	IPv4InterfaceArpProxySysctlTemplate   string = "net.ipv4.conf.%s.proxy_arp"
	IPv4InterfaceDelayProxySysctlTemplate string = "net.ipv4.neigh.%s.proxy_delay"
	ipv4Forward                           string = "net.ipv4.ip_forward"
	virtualIpaddress                      string = "169.254.1.1"
)

func Masquerading(bridge string, source string, ignore []string, outface string) error {
	ipv4SysctlValueName := fmt.Sprintf(IPv4InterfaceArpProxySysctlTemplate, bridge)
	if _, err := sysctl.Sysctl(ipv4SysctlValueName, "1"); err != nil {
		return fmt.Errorf("failed to set proxy_arp on newly added interface %s: %v", bridge, err)
	}

	ipv4SysctlValueName = fmt.Sprintf(IPv4InterfaceDelayProxySysctlTemplate, bridge)
	if _, err := sysctl.Sysctl(ipv4SysctlValueName, "0"); err != nil {
		return fmt.Errorf("failed to set proxy_delay on newly added interface %s: %v", bridge, err)
	}

	if _, err := sysctl.Sysctl(ipv4Forward, "1"); err != nil {
		return fmt.Errorf("failed to set ip_forward=1: %v", err)
	}

	if err := iptables.AddRule(bridge, source, ignore, outface); err != nil {
		return err
	}

	// After applying ebtables arp rules, it's better to send arp gratuitous request to make sure all overcloud
	// nodes use proper mac for default gateway.
	if err := arping.GratuitousArpOverIfaceByName(net.ParseIP(virtualIpaddress), bridge); err != nil {
		return err
	}

	return nil
}

func EnableMasquerade(ipmasq *networkv1alpha1.Masquerade) error {
	// Make sure arp request won't go outside of compute node
	// ebtables-nft -I FORWARD -p ARP --logical-out br2710 --arp-ip-dst 169.254.1.1 -j DROP
	rule := []string{"-p", "ARP", "--logical-out", ipmasq.Bridge, "--arp-ip-dst", virtualIpaddress, "-j", "DROP"}

	if err := ebtables.AddRule(rule...); err != nil {
		return err
	}

	return Masquerading(ipmasq.Bridge, ipmasq.Source, ipmasq.Ignore, ipmasq.Outface)
}

func DeleteMasquerade(ipmasq *networkv1alpha1.Masquerade) error {

	if err := ebtables.DeleteRuleByDevice(ipmasq.Bridge); err != nil {
		return err
	}

	if err := iptables.PurgeChain(ipmasq.Bridge); err != nil {
		return err
	}

	return nil
}
