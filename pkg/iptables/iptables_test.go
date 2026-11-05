package iptables

import (
	"reflect"
	"strings"
	"testing"

	"github.com/NCCloud/tabby-cni/internal/testutil"
	goiptables "github.com/coreos/go-iptables/iptables"
)

func TestRenderRule(t *testing.T) {
	tests := []struct {
		name string
		rule Rules
		want []string
	}{
		{
			name: "empty rule",
			rule: Rules{},
			want: []string{},
		},
		{
			name: "jump from POSTROUTING",
			rule: Rules{table: "nat", chain: "POSTROUTING", source: "10.0.0.0/24", action: "br0-POSTROUTING"},
			want: []string{"-s", "10.0.0.0/24", "-j", "br0-POSTROUTING"},
		},
		{
			name: "masquerade on an egress interface",
			rule: Rules{table: "nat", chain: "br0-POSTROUTING", outface: "eth0", action: "MASQUERADE"},
			want: []string{"-j", "MASQUERADE", "-o", "eth0"},
		},
		{
			name: "accept an ignored destination",
			rule: Rules{table: "nat", chain: "br0-POSTROUTING", destination: "192.168.0.0/16", action: "ACCEPT"},
			want: []string{"-d", "192.168.0.0/16", "-j", "ACCEPT"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderRule(&tt.rule)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("renderRule() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddRuleAndPurgeChain(t *testing.T) {
	testutil.RequirePrivileged(t)
	testutil.AddDummy(t, "tegress0", "10.250.0.1/24")

	ipt, err := goiptables.NewWithProtocol(goiptables.ProtocolIPv4)
	if err != nil {
		t.Fatalf("failed to init iptables: %v", err)
	}
	t.Cleanup(func() { _ = PurgeChain("tbrnat") })

	ignore := []string{"10.10.0.0/16"}
	if err := AddRule("tbrnat", "10.20.0.0/24", ignore, "10.250.0.0/24"); err != nil {
		t.Fatalf("AddRule() error = %v", err)
	}

	// Adding the same rules again must not duplicate them.
	if err := AddRule("tbrnat", "10.20.0.0/24", ignore, "10.250.0.0/24"); err != nil {
		t.Fatalf("second AddRule() error = %v", err)
	}

	assertRuleCount(t, ipt, "POSTROUTING", "-s 10.20.0.0/24 -j tbrnat-POSTROUTING", 1)
	assertRuleCount(t, ipt, "tbrnat-POSTROUTING", "-o tegress0 -j MASQUERADE", 1)
	assertRuleCount(t, ipt, "tbrnat-POSTROUTING", "-d 10.10.0.0/16 -j ACCEPT", 1)

	if err := PurgeChain("tbrnat"); err != nil {
		t.Fatalf("PurgeChain() error = %v", err)
	}

	assertRuleCount(t, ipt, "POSTROUTING", "tbrnat-POSTROUTING", 0)
	exists, err := ipt.ChainExists("nat", "tbrnat-POSTROUTING")
	if err != nil {
		t.Fatalf("failed to check chain: %v", err)
	}
	if exists {
		t.Errorf("chain tbrnat-POSTROUTING still exists after PurgeChain()")
	}
}

func TestAddRuleUnknownEgressNetwork(t *testing.T) {
	testutil.RequirePrivileged(t)

	if err := AddRule("tbrbad", "10.20.0.0/24", nil, "not-a-cidr"); err == nil {
		t.Fatal("AddRule() with an invalid egress network should fail")
	}
}

// assertRuleCount checks how many rules in a nat chain contain the given text.
func assertRuleCount(t *testing.T, ipt *goiptables.IPTables, chain string, contains string, want int) {
	t.Helper()

	rules, err := ipt.List("nat", chain)
	if err != nil {
		t.Fatalf("failed to list nat %s: %v", chain, err)
	}

	got := 0
	for _, r := range rules {
		if strings.Contains(r, contains) {
			got++
		}
	}
	if got != want {
		t.Errorf("rules in nat %s containing %q = %d, want %d (rules: %v)", chain, contains, got, want, rules)
	}
}
