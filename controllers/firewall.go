package controllers

import (
	"fmt"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/NCCloud/tabby-cni/pkg/ebtables"
	"github.com/NCCloud/tabby-cni/pkg/iptables"
	"github.com/containernetworking/plugins/pkg/utils/sysctl"
)

const (
	IPv4InterfaceArpProxySysctlTemplate string = "net.ipv4.conf.%s.proxy_arp"
	ipv4Forward                         string = "net.ipv4.ip_forward"
	virtualIpaddress                    string = "169.254.1.1"
)

func EnableMasquerade(ipmasq *networkv1alpha1.Masquerade) error {

	ipv4SysctlValueName := fmt.Sprintf(IPv4InterfaceArpProxySysctlTemplate, ipmasq.Bridge)
	if _, err := sysctl.Sysctl(ipv4SysctlValueName, "1"); err != nil {
		return fmt.Errorf("failed to set proxy_arp on newly added interface %s: %v", ipmasq.Bridge, err)
	}

	if _, err := sysctl.Sysctl(ipv4Forward, "1"); err != nil {
		return fmt.Errorf("failed to set ip_forward=1: %v", err)
	}

	// Make sure arp request won't go outside of compute node
	// ebtables-nft -I FORWARD -p ARP -o br2710 --arp-ip-dst 169.254.1.1 -j DROP
	rule := []string{"-p", "ARP", "-o", ipmasq.Bridge, "--arp-ip-dst", virtualIpaddress, "-j", "DROP"}

	if err := ebtables.AddRule(rule...); err != nil {
		return err
	}

	if err := iptables.AddRule(ipmasq.Bridge, ipmasq.Source, ipmasq.Ignore, ipmasq.Outface); err != nil {
		return err
	}

	return nil
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
