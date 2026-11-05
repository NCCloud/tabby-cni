// Package testutil holds helpers for tests that change the host network
// (links, routes, iptables and ebtables rules).
package testutil

import (
	"net"
	"os"
	"testing"

	"github.com/vishvananda/netlink"
)

// PrivilegedEnv must be set to "1" to run tests that change the host network.
// Run them with `make test-privileged`, which uses a throwaway privileged
// container so the developer machine or CI runner is never touched.
const PrivilegedEnv = "TABBY_PRIVILEGED_TESTS"

// RequirePrivileged skips the test unless it runs as root with PrivilegedEnv=1.
func RequirePrivileged(t *testing.T) {
	t.Helper()

	if os.Getenv(PrivilegedEnv) != "1" {
		t.Skipf("set %s=1 and run as root (make test-privileged) to run this test", PrivilegedEnv)
	}

	if os.Geteuid() != 0 {
		t.Skip("this test must run as root")
	}
}

// AddDummy creates an up dummy interface, optionally with an IPv4 address in
// CIDR notation, and removes it when the test ends.
func AddDummy(t *testing.T, name string, cidr string) netlink.Link {
	t.Helper()

	link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}
	if err := netlink.LinkAdd(link); err != nil {
		t.Fatalf("failed to add dummy interface %s: %v", name, err)
	}
	t.Cleanup(func() { DeleteLink(name) })

	if err := netlink.LinkSetUp(link); err != nil {
		t.Fatalf("failed to set dummy interface %s up: %v", name, err)
	}

	if cidr != "" {
		addr, err := netlink.ParseAddr(cidr)
		if err != nil {
			t.Fatalf("failed to parse address %s: %v", cidr, err)
		}
		if err := netlink.AddrAdd(link, addr); err != nil {
			t.Fatalf("failed to add address %s to %s: %v", cidr, name, err)
		}
	}

	added, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatalf("failed to find dummy interface %s: %v", name, err)
	}

	return added
}

// DeleteLink removes a link if it exists.
func DeleteLink(name string) {
	if link, err := netlink.LinkByName(name); err == nil {
		_ = netlink.LinkDel(link)
	}
}

// LinkExists reports whether a link with the given name exists.
func LinkExists(t *testing.T, name string) bool {
	t.Helper()

	_, err := netlink.LinkByName(name)
	if err == nil {
		return true
	}
	if _, ok := err.(netlink.LinkNotFoundError); ok {
		return false
	}
	t.Fatalf("failed to look up link %s: %v", name, err)
	return false
}

// MustParseCIDR returns the network of a CIDR or fails the test.
func MustParseCIDR(t *testing.T, cidr string) *net.IPNet {
	t.Helper()

	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("failed to parse CIDR %s: %v", cidr, err)
	}
	return n
}
