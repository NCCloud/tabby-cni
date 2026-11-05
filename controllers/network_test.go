package controllers

import (
	"context"
	"net"
	"os"
	"strings"
	"testing"

	networkv1alpha1 "github.com/NCCloud/tabby-cni/api/v1alpha1"
	"github.com/NCCloud/tabby-cni/internal/testutil"
	"github.com/NCCloud/tabby-cni/pkg/bridge"
	"github.com/vishvananda/netlink"
)

func findRoute(t *testing.T, dst string) *netlink.Route {
	t.Helper()

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
		&netlink.Route{Dst: testutil.MustParseCIDR(t, dst)}, netlink.RT_FILTER_DST)
	if err != nil {
		t.Fatalf("failed to list routes: %v", err)
	}
	if len(routes) == 0 {
		return nil
	}
	return &routes[0]
}

func deleteRoute(dst string) {
	_, n, err := net.ParseCIDR(dst)
	if err != nil {
		return
	}
	routes, _ := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Dst: n}, netlink.RT_FILTER_DST)
	for i := range routes {
		_ = netlink.RouteDel(&routes[i])
	}
}

func TestAddRoute(t *testing.T) {
	testutil.RequireIntegration(t)
	dev := testutil.AddDummy(t, "trt0", "10.251.0.1/24")
	t.Cleanup(func() {
		deleteRoute("10.252.0.0/16")
		deleteRoute("10.253.0.0/16")
		deleteRoute("10.254.0.0/16")
	})

	t.Run("via gateway", func(t *testing.T) {
		if err := addRoute(networkv1alpha1.Route{Via: "10.251.0.254", Destination: "10.252.0.0/16"}); err != nil {
			t.Fatalf("addRoute() error = %v", err)
		}
		r := findRoute(t, "10.252.0.0/16")
		if r == nil {
			t.Fatal("route was not added")
		}
		if r.Gw.String() != "10.251.0.254" {
			t.Errorf("gateway = %v, want 10.251.0.254", r.Gw)
		}

		// Adding an existing route is not an error.
		if err := addRoute(networkv1alpha1.Route{Via: "10.251.0.254", Destination: "10.252.0.0/16"}); err != nil {
			t.Fatalf("second addRoute() error = %v", err)
		}
	})

	t.Run("via device", func(t *testing.T) {
		if err := addRoute(networkv1alpha1.Route{Via: "trt0", Destination: "10.253.0.0/16"}); err != nil {
			t.Fatalf("addRoute() error = %v", err)
		}
		r := findRoute(t, "10.253.0.0/16")
		if r == nil {
			t.Fatal("route was not added")
		}
		if r.LinkIndex != dev.Attrs().Index {
			t.Errorf("route link index = %d, want %d (trt0)", r.LinkIndex, dev.Attrs().Index)
		}
		if r.Scope != netlink.SCOPE_LINK {
			t.Errorf("route scope = %v, want link", r.Scope)
		}
	})

	t.Run("with source network", func(t *testing.T) {
		route := networkv1alpha1.Route{Via: "10.251.0.254", Destination: "10.254.0.0/16", Source: "10.251.0.0/24"}
		if err := addRoute(route); err != nil {
			t.Fatalf("addRoute() error = %v", err)
		}
		r := findRoute(t, "10.254.0.0/16")
		if r == nil {
			t.Fatal("route was not added")
		}
		if r.Src.String() != "10.251.0.1" {
			t.Errorf("route source = %v, want 10.251.0.1", r.Src)
		}
	})

	t.Run("source network not on host", func(t *testing.T) {
		route := networkv1alpha1.Route{Via: "10.251.0.254", Destination: "10.255.0.0/16", Source: "192.0.2.0/24"}
		err := addRoute(route)
		if err == nil {
			t.Fatal("addRoute() should fail when the source network is not on the host")
		}
		if !strings.Contains(err.Error(), "192.0.2.0/24") {
			t.Errorf("error = %q, want it to mention the source network", err)
		}
	})

	t.Run("invalid destination", func(t *testing.T) {
		if err := addRoute(networkv1alpha1.Route{Via: "trt0", Destination: "not-a-cidr"}); err == nil {
			t.Fatal("addRoute() with an invalid destination should fail")
		}
	})

	t.Run("unknown device", func(t *testing.T) {
		if err := addRoute(networkv1alpha1.Route{Via: "tmissing0", Destination: "10.250.0.0/16"}); err == nil {
			t.Fatal("addRoute() via a missing device should fail")
		}
	})
}

func TestCreateAndDeleteNetwork(t *testing.T) {
	testutil.RequireIntegration(t)
	testutil.AddDummy(t, "tcn0", "")
	t.Cleanup(func() {
		testutil.DeleteLink("tcn0.30")
		testutil.DeleteLink("tcn0.31")
		testutil.DeleteLink("tbr-cn")
		deleteRoute("10.249.0.0/16")
	})

	spec := &networkv1alpha1.NetworkAttachmentSpec{
		Bridge: []networkv1alpha1.Bridge{{
			Name: "tbr-cn",
			Mtu:  1450,
			Ports: []networkv1alpha1.Port{
				{Name: "tcn0", Vlan: 30, Mtu: 1450},
				{Name: "tcn0", Vlan: 31, Mtu: 1450},
			},
		}},
		Routes: []networkv1alpha1.Route{{Via: "tbr-cn", Destination: "10.249.0.0/16"}},
	}

	if err := CreateNetwork(context.Background(), spec); err != nil {
		t.Fatalf("CreateNetwork() error = %v", err)
	}

	br, err := netlink.LinkByName("tbr-cn")
	if err != nil {
		t.Fatalf("bridge was not created: %v", err)
	}
	for _, name := range []string{"tcn0.30", "tcn0.31"} {
		link, err := netlink.LinkByName(name)
		if err != nil {
			t.Fatalf("vlan %s was not created: %v", name, err)
		}
		if link.Attrs().MasterIndex != br.Attrs().Index {
			t.Errorf("vlan %s is not attached to tbr-cn", name)
		}
	}
	if r := findRoute(t, "10.249.0.0/16"); r == nil || r.LinkIndex != br.Attrs().Index {
		t.Errorf("route 10.249.0.0/16 via tbr-cn was not added: %+v", r)
	}

	// Creating the same network again is idempotent.
	if err := CreateNetwork(context.Background(), spec); err != nil {
		t.Fatalf("second CreateNetwork() error = %v", err)
	}

	if err := DeleteNetwork(context.Background(), spec); err != nil {
		t.Fatalf("DeleteNetwork() error = %v", err)
	}
	for _, name := range []string{"tcn0.30", "tcn0.31", "tbr-cn"} {
		if testutil.LinkExists(t, name) {
			t.Errorf("%s still exists after DeleteNetwork()", name)
		}
	}
	if !testutil.LinkExists(t, "tcn0") {
		t.Errorf("parent interface tcn0 was removed")
	}
}

func TestEnableAndDeleteMasquerade(t *testing.T) {
	testutil.RequireIntegration(t)
	testutil.AddDummy(t, "tmq-egress", "10.248.0.1/24")
	t.Cleanup(func() { testutil.DeleteLink("tbr-mq") })

	if _, err := (&bridge.Bridge{Name: "tbr-mq"}).Create(); err != nil {
		t.Fatalf("failed to create bridge: %v", err)
	}

	ipmasq := &networkv1alpha1.Masquerade{
		Enabled:       true,
		Source:        "10.247.0.0/24",
		Ignore:        []string{"10.246.0.0/16"},
		Bridge:        "tbr-mq",
		EgressNetwork: "10.248.0.0/24",
	}
	t.Cleanup(func() { _ = DeleteMasquerade(ipmasq) })

	if err := EnableMasquerade(ipmasq); err != nil {
		t.Fatalf("EnableMasquerade() error = %v", err)
	}

	for path, want := range map[string]string{
		"/proc/sys/net/ipv4/conf/tbr-mq/proxy_arp":    "1",
		"/proc/sys/net/ipv4/neigh/tbr-mq/proxy_delay": "0",
		"/proc/sys/net/ipv4/ip_forward":               "1",
	} {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		if strings.TrimSpace(string(got)) != want {
			t.Errorf("%s = %q, want %q", path, strings.TrimSpace(string(got)), want)
		}
	}

	if err := DeleteMasquerade(ipmasq); err != nil {
		t.Fatalf("DeleteMasquerade() error = %v", err)
	}
}
