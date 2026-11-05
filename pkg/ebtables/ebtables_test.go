package ebtables

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/NCCloud/tabby-cni/internal/testutil"
)

func TestCheckIfRuleExists(t *testing.T) {
	list := `Bridge table: filter

Bridge chain: FORWARD, entries: 2, policy: ACCEPT
-p ARP --logical-out br0 --arp-ip-dst 169.254.1.1 -j DROP
-p ARP --logical-out br1 --arp-ip-dst 169.254.1.1 -j DROP
`

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "existing rule", args: []string{"-p", "ARP", "--logical-out", "br0", "--arp-ip-dst", "169.254.1.1", "-j", "DROP"}, want: true},
		{name: "rule for another bridge", args: []string{"-p", "ARP", "--logical-out", "br2", "--arp-ip-dst", "169.254.1.1", "-j", "DROP"}, want: false},
		{name: "partial rule", args: []string{"-p", "ARP"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkIfRuleExists(list, tt.args...); got != tt.want {
				t.Errorf("checkIfRuleExists() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMakeFullArgs(t *testing.T) {
	got := makeFullArgs("filter", "-I", "FORWARD", "-p", "ARP", "-j", "DROP")
	want := []string{"-t", "filter", "-I", "FORWARD", "-p", "ARP", "-j", "DROP"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("makeFullArgs() = %v, want %v", got, want)
	}
}

func TestAddRuleAndDeleteRuleByDevice(t *testing.T) {
	testutil.RequirePrivileged(t)
	if _, err := exec.LookPath(cmdebtables); err != nil {
		t.Skipf("%s is not installed", cmdebtables)
	}
	t.Cleanup(func() { _ = DeleteRuleByDevice("tbreb") })

	rule := []string{"-p", "ARP", "--logical-out", "tbreb", "--arp-ip-dst", "169.254.1.1", "-j", "DROP"}

	if err := AddRule(rule...); err != nil {
		t.Fatalf("AddRule() error = %v", err)
	}
	// Adding the same rule again must not duplicate it.
	if err := AddRule(rule...); err != nil {
		t.Fatalf("second AddRule() error = %v", err)
	}
	if got := countRules(t, "tbreb"); got != 1 {
		t.Errorf("rules for tbreb = %d, want 1", got)
	}

	if err := DeleteRuleByDevice("tbreb"); err != nil {
		t.Fatalf("DeleteRuleByDevice() error = %v", err)
	}
	if got := countRules(t, "tbreb"); got != 0 {
		t.Errorf("rules for tbreb after DeleteRuleByDevice() = %d, want 0", got)
	}
}

func countRules(t *testing.T, bridge string) int {
	t.Helper()

	out, err := exec.Command(cmdebtables, "--list", ChainForward).CombinedOutput()
	if err != nil {
		t.Fatalf("failed to list ebtables rules: %v: %s", err, out)
	}
	return strings.Count(string(out), "--logical-out "+bridge+" ")
}
