package network

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type fixtureAddr string

func (a fixtureAddr) Network() string { return "ip" }
func (a fixtureAddr) String() string  { return string(a) }

func fixtures(items []net.Interface, addresses map[string][]net.Addr) sources {
	return sources{interfaces: func() ([]net.Interface, error) { return items, nil }, addresses: func(i net.Interface) ([]net.Addr, error) { return addresses[i.Name], nil }, readFile: func(path string) ([]byte, error) {
		switch {
		case path == "/proc/net/route":
			return []byte("Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n"), nil
		case path == "/proc/net/ipv6_route", path == "/etc/resolv.conf":
			return []byte{}, nil
		case strings.HasSuffix(path, "/type"):
			if strings.Contains(path, "/lo/") {
				return []byte("772\n"), nil
			}
			return []byte("1\n"), nil
		case strings.HasSuffix(path, "/operstate"):
			return []byte("down\n"), nil
		default:
			return nil, os.ErrNotExist
		}
	}, resolve: func(path string) (string, error) { return path, nil }}
}

func TestInterfaces(t *testing.T) {
	for _, tt := range []struct {
		name      string
		items     []net.Interface
		addresses map[string][]net.Addr
	}{
		{"IPv4", []net.Interface{{Index: 2, Name: "enp4s0", MTU: 1500, Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 1}}}, map[string][]net.Addr{"enp4s0": {fixtureAddr("192.0.2.5/24")}}},
		{"IPv6", []net.Interface{{Index: 2, Name: "enp4s0", MTU: 1500}}, map[string][]net.Addr{"enp4s0": {fixtureAddr("2001:db8::5/64"), fixtureAddr("fe80::5/64")}}},
		{"dual stack", []net.Interface{{Index: 7, Name: "wlan0", MTU: 1500}}, map[string][]net.Addr{"wlan0": {fixtureAddr("2001:db8::2/64"), fixtureAddr("192.0.2.9/24"), fixtureAddr("192.0.2.2/24")}}},
		{"loopback", []net.Interface{{Index: 1, Name: "lo", Flags: net.FlagLoopback | net.FlagUp, MTU: 65536}}, map[string][]net.Addr{"lo": {fixtureAddr("127.0.0.1/8"), fixtureAddr("::1/128")}}},
		{"disabled no addresses", []net.Interface{{Index: 4, Name: "ens3", MTU: 1500}}, nil},
		{"virtual without MAC", []net.Interface{{Index: 8, Name: "veth42", MTU: 1500}}, nil},
		{"multiple", []net.Interface{{Index: 9, Name: "ens9"}, {Index: 1, Name: "lo", Flags: net.FlagLoopback}}, nil},
		{"no interfaces", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := inspect(context.Background(), "linux", fixtures(tt.items, tt.addresses))
			if err != nil {
				t.Fatal(err)
			}
			if len(r.Interfaces) != len(tt.items) {
				t.Fatal(r.Interfaces)
			}
			for n, iface := range r.Interfaces {
				if n > 0 && r.Interfaces[n-1].Index > iface.Index {
					t.Fatal("unstable order")
				}
				if iface.Loopback != (iface.Name == "lo") {
					t.Fatal("loopback identity")
				}
				if iface.Name == "lo" && iface.Type != "loopback" {
					t.Fatal("link type")
				}
				if iface.Name == "veth42" && (iface.MAC != "" || iface.Type != "ethernet") {
					t.Fatal("virtual interface was fabricated as hardware")
				}
				want := tt.addresses[iface.Name]
				if len(iface.IPv4)+len(iface.IPv6) != len(want) {
					t.Fatal(iface)
				}
				for _, address := range append(append([]string{}, iface.IPv4...), iface.IPv6...) {
					if _, err := netip.ParsePrefix(address); err != nil {
						t.Fatal(err)
					}
				}
				if iface.Name == "ens3" && (iface.AdministrativeState != "down" || iface.OperationalState != "down") {
					t.Fatal(iface)
				}
			}
			if r.Interfaces == nil || r.Routes == nil || r.DNS.Nameservers == nil {
				t.Fatal("empty lists must encode as arrays")
			}
		})
	}
}

func TestPartialSourcesAndDisappearingInterfaces(t *testing.T) {
	s := fixtures([]net.Interface{{Index: 2, Name: "enp1s0"}}, nil)
	s.addresses = func(net.Interface) ([]net.Addr, error) { return nil, errors.New("interface disappeared") }
	s.readFile = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	r, err := inspect(context.Background(), "linux", s)
	if err != nil || len(r.Interfaces) != 1 || r.Interfaces[0].OperationalState != "unknown" {
		t.Fatalf("%+v %v", r, err)
	}
	statuses := map[string]string{}
	for _, source := range r.Sources {
		statuses[source.Component] = source.Status
	}
	for _, name := range []string{"routes_ipv4", "routes_ipv6", "dns"} {
		if statuses[name] != "unavailable" {
			t.Fatal(statuses)
		}
	}
	if statuses["interface enp1s0 addresses"] != "failed" {
		t.Fatal(statuses)
	}
	s = fixtures(nil, nil)
	s.interfaces = func() ([]net.Interface, error) { return nil, os.ErrPermission }
	r, err = inspect(context.Background(), "linux", s)
	if err != nil || len(r.Interfaces) != 0 {
		t.Fatal(err)
	}
	for _, source := range r.Sources {
		if source.Component == "interfaces" && source.Status != "failed" {
			t.Fatal(source)
		}
	}
}

func TestInspectionCancellationAndUnsupported(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		calls := 0
		s := fixtures(nil, nil)
		s.interfaces = func() ([]net.Interface, error) { calls++; return nil, nil }
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := inspect(ctx, goos, s)
		want := ErrUnsupportedPlatform
		if goos == "linux" {
			want = context.Canceled
		}
		if !errors.Is(err, want) || calls != 0 {
			t.Fatalf("%s: %v calls %d", goos, err, calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := fixtures(nil, nil)
	s.interfaces = func() ([]net.Interface, error) { cancel(); return nil, nil }
	s.readFile = func(string) ([]byte, error) { t.Fatal("read after cancellation"); return nil, nil }
	if _, err := inspect(ctx, "linux", s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func ipv4Word(address string) string {
	bytes := netip.MustParseAddr(address).As4()
	return fmt.Sprintf("%08X", binary.NativeEndian.Uint32(bytes[:]))
}
func route4(iface, dest, gateway, mask string, metric int) string {
	return fmt.Sprintf("%s %s %s 0003 0 0 %d %s 0 0 0\n", iface, ipv4Word(dest), ipv4Word(gateway), metric, ipv4Word(mask))
}

func TestRoutesAndDeterminism(t *testing.T) {
	s := fixtures([]net.Interface{{Index: 2, Name: "ens3"}}, nil)
	read := s.readFile
	s.readFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/net/route":
			return []byte("Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n" + route4("ens3", "0.0.0.0", "192.0.2.1", "0.0.0.0", 100) + route4("unknown0", "0.0.0.0", "192.0.2.254", "0.0.0.0", 10) + route4("ens3", "192.0.2.0", "0.0.0.0", "255.255.255.0", 0)), nil
		case "/proc/net/ipv6_route":
			return []byte("00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000064 00000000 00000000 00000003 ens3\n"), nil
		}
		return read(path)
	}
	r, err := inspect(context.Background(), "linux", s)
	if err != nil || len(r.Routes) != 4 {
		t.Fatalf("%+v %v", r, err)
	}
	if r.Routes[0].Interface != "unknown0" || r.Routes[0].InterfaceIndex != nil || *r.Routes[0].Metric != 10 {
		t.Fatal(r.Routes)
	}
	if r.Routes[1].Gateway != "192.0.2.1" || r.Routes[1].Destination != "0.0.0.0/0" || *r.Routes[1].InterfaceIndex != 2 {
		t.Fatal(r.Routes[1])
	}
	if r.Routes[2].Gateway != "" || r.Routes[2].Default {
		t.Fatal(r.Routes[2])
	}
	if r.Routes[3].Gateway != "fe80::1" || r.Routes[3].Destination != "::/0" || !r.Routes[3].Default {
		t.Fatal(r.Routes[3])
	}
	first, _ := json.Marshal(r)
	again, _ := inspect(context.Background(), "linux", s)
	second, _ := json.Marshal(again)
	if string(first) != string(second) {
		t.Fatal("nondeterministic JSON")
	}
}

func TestMalformedRoutesKeepValidEntries(t *testing.T) {
	good4 := strings.Fields(route4("ens3", "192.0.2.0", "0.0.0.0", "255.255.255.0", 100))
	good6 := strings.Fields("20010db8000000000000000000000000 40 00000000000000000000000000000000 00 00000000000000000000000000000000 0000000a 00000000 00000000 00000001 missing0")
	for _, tt := range []struct {
		family string
		fields []string
		index  int
		bad    string
	}{
		{"ipv4", good4, 1, "NOTHEX00"}, {"ipv4", good4, 2, "00"}, {"ipv4", good4, 6, "-1"}, {"ipv4", good4, 7, "00FF00FF"}, {"ipv4", good4, 3, "ZZZZ"}, {"ipv4", good4, 4, "bad"},
		{"ipv6", good6, 0, "Z0010db8000000000000000000000000"}, {"ipv6", good6, 1, "ff"}, {"ipv6", good6, 3, "81"}, {"ipv6", good6, 4, "bad"}, {"ipv6", good6, 5, "metric"}, {"ipv6", good6, 6, "invalid"}, {"ipv6", good6, 8, "flags"},
	} {
		t.Run(tt.family+fmt.Sprint(tt.index), func(t *testing.T) {
			bad := append([]string{}, tt.fields...)
			bad[tt.index] = tt.bad
			routes, err := parseRoutes(context.Background(), strings.Join(bad, " ")+"\n"+strings.Join(tt.fields, " ")+"\n", tt.family)
			if err == nil || len(routes) != 1 {
				t.Fatalf("routes %v error %v", routes, err)
			}
		})
	}
	for _, family := range []string{"ipv4", "ipv6"} {
		if _, err := parseRoutes(context.Background(), "too few fields", family); err == nil {
			t.Fatal("malformed route accepted")
		}
		if routes, err := parseRoutes(context.Background(), "\n", family); err != nil || len(routes) != 0 {
			t.Fatal("empty route table")
		}
	}
	good6[2] = "20010db8000000000000000000000000"
	good6[3] = "20"
	good6[8] = "00000200"
	r, err := parseRoute(good6, "ipv6")
	if err != nil || r.Source != "2001:db8::/32" || r.State != "rejected" {
		t.Fatalf("source-specific reject route %+v %v", r, err)
	}
}

func TestDNSConfigurations(t *testing.T) {
	for _, tt := range []struct {
		name, text string
		servers    []Nameserver
		search     []string
		bad        bool
	}{
		{"ordinary", "nameserver 192.0.2.53\nsearch example.test lab.test\n", []Nameserver{{"192.0.2.53", false}}, []string{"example.test", "lab.test"}, false},
		{"multiple IPv4 IPv6", "nameserver 192.0.2.53\nnameserver 2001:db8::53\n", []Nameserver{{"192.0.2.53", false}, {"2001:db8::53", false}}, nil, false},
		{"stub comments whitespace", " # comment\n nameserver\t127.0.0.53 # local\nnameserver ::1 ; comment\n", []Nameserver{{"127.0.0.53", true}, {"::1", true}}, nil, false},
		{"empty", "", nil, nil, false},
		{"last search directive", "search old.test\ndomain newer.test\nsearch final.test\n", nil, []string{"final.test"}, false},
		{"invalid address", "nameserver garbage\nnameserver 192.0.2.53\n", []Nameserver{{"192.0.2.53", false}}, nil, true},
		{"invalid nameserver field count", "nameserver\n", nil, nil, true},
		{"scoped IPv6", "nameserver fe80::1%ens3\n", []Nameserver{{"fe80::1%ens3", false}}, nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dns := emptyReport().DNS
			err := parseDNS(context.Background(), tt.text, &dns)
			if (err != nil) != tt.bad || !reflect.DeepEqual(append([]Nameserver{}, dns.Nameservers...), append([]Nameserver{}, tt.servers...)) || !reflect.DeepEqual(append([]string{}, dns.SearchDomains...), append([]string{}, tt.search...)) || dns.UpstreamStatus != "unknown" {
				t.Fatalf("DNS %+v error %v", dns, err)
			}
		})
	}
}

func TestSymlinkedResolverAndPartialParsing(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "resolved.conf")
	link := filepath.Join(root, "resolv.conf")
	if err := os.WriteFile(target, []byte("nameserver 127.0.0.53\nnameserver malformed\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	s := fixtures(nil, nil)
	read := s.readFile
	s.readFile = func(path string) ([]byte, error) {
		if path == "/etc/resolv.conf" {
			return os.ReadFile(link)
		}
		if path == "/proc/net/route" {
			return []byte("bad\n" + route4("unknown0", "0.0.0.0", "192.0.2.1", "0.0.0.0", 0)), nil
		}
		return read(path)
	}
	s.resolve = func(string) (string, error) { return filepath.EvalSymlinks(link) }
	r, err := inspect(context.Background(), "linux", s)
	wantTarget, resolveErr := filepath.EvalSymlinks(target)
	if resolveErr != nil {
		t.Fatal(resolveErr)
	}
	if err != nil || r.DNS.ResolvedSource != wantTarget || len(r.DNS.Nameservers) != 1 || !r.DNS.Nameservers[0].LocalStub {
		t.Fatalf("%+v %v", r, err)
	}
	for _, source := range r.Sources {
		if (source.Component == "dns" || source.Component == "routes_ipv4") && source.Status != "partial" {
			t.Fatal(source)
		}
	}
}

func TestIPv6OnlyAndPartialInterfaceMetadata(t *testing.T) {
	s := fixtures([]net.Interface{{Index: 2, Name: "ens3", Flags: net.FlagUp}}, map[string][]net.Addr{"ens3": {fixtureAddr("bad"), fixtureAddr("2001:db8::5/64")}})
	read := s.readFile
	s.readFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/net/route":
			return nil, os.ErrNotExist
		case "/proc/net/ipv6_route":
			return []byte("00000000000000000000000000000000 00 00000000000000000000000000000000 00 fe800000000000000000000000000001 00000001 00000000 00000000 00000003 ens3\n"), nil
		case "/sys/class/net/ens3/type":
			return []byte("65534"), nil
		case "/sys/class/net/ens3/operstate":
			return []byte("not-a-state"), nil
		case "/etc/resolv.conf":
			return nil, os.ErrPermission
		}
		return read(path)
	}
	r, err := inspect(context.Background(), "linux", s)
	if err != nil || len(r.Routes) != 1 || r.Routes[0].Family != "ipv6" || len(r.Interfaces[0].IPv6) != 1 || r.Interfaces[0].Type != "unknown" || r.Interfaces[0].OperationalState != "unknown" {
		t.Fatalf("report %+v err %v", r, err)
	}
	statuses := map[string]string{}
	for _, source := range r.Sources {
		statuses[source.Component] = source.Status
	}
	if statuses["dns"] != "failed" || statuses["interface ens3 addresses"] != "partial" || statuses["interface ens3 operstate"] != "failed" {
		t.Fatal(statuses)
	}
}
