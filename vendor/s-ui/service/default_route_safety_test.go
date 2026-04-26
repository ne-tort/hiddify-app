package service

import "testing"

func TestDefaultRouteLineDev(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"default via 10.0.0.1 dev ens3 proto static", "ens3"},
		{"default dev wg0 metric 10", "wg0"},
		{"default via fe80::1 dev eth0 metric 1024", "eth0"},
		{"", ""},
	}
	for _, tc := range tests {
		got := defaultRouteLineDev(tc.line)
		if got != tc.want {
			t.Fatalf("line %q: got %q want %q", tc.line, got, tc.want)
		}
	}
}

func TestIsTunnelLikeInterface(t *testing.T) {
	type tc struct {
		iface string
		tun   bool
	}
	cases := []tc{
		{"wg0", true},
		{"awg1", true},
		{"tun0", true},
		{"utun4", true},
		{"ens3", false},
		{"eth0", false},
		{"wlan0", false},
		{"bond0", false},
		{"ifb0", true},
	}
	for _, c := range cases {
		got := isTunnelLikeInterface(c.iface)
		if got != c.tun {
			t.Fatalf("%s: got %v want %v", c.iface, got, c.tun)
		}
	}
}

func TestParseDefaultRouteDevsForTest(t *testing.T) {
	out := `default via 89.125.17.1 dev eth0 proto static
default via 10.0.0.1 dev wg0 metric 5
`
	devs := parseDefaultRouteDevsForTest(out)
	if len(devs) != 2 || devs[0] != "eth0" || devs[1] != "wg0" {
		t.Fatalf("devs: %#v", devs)
	}
}
