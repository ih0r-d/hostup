package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ih0r-d/hostup/src/internal/network"
)

func cliNetworkReport() network.Report {
	metric := uint32(100)
	return network.Report{Interfaces: []network.Interface{{Name: "enp1s0", Index: 2, AdministrativeState: "up", OperationalState: "up", Type: "ethernet", MTU: 1500, IPv4: []string{"192.0.2.9/24"}, IPv6: []string{"2001:db8::9/64"}}}, Routes: []network.Route{{Family: "ipv4", Destination: "0.0.0.0/0", Gateway: "192.0.2.1", Interface: "enp1s0", Default: true, State: "active", Metric: &metric}}, DNS: network.DNS{Source: "/etc/resolv.conf", ResolvedSource: "/run/systemd/resolve/stub-resolv.conf", Nameservers: []network.Nameserver{{Address: "127.0.0.53", LocalStub: true}}, SearchDomains: []string{"example.test"}, UpstreamStatus: "unknown"}, Sources: []network.Source{{Component: "interfaces", Status: "available", Path: "Go interface API"}, {Component: "routes_ipv4", Status: "available", Path: "/proc/net/route"}, {Component: "dns", Status: "available", Path: "/etc/resolv.conf"}}, Limitations: []string{"No policy-routing completeness or connectivity claim."}}
}

func TestNetworkCLIOutput(t *testing.T) {
	for _, name := range []string{"status", "interfaces", "routes", "dns"} {
		for _, asJSON := range []bool{false, true} {
			t.Run(name+map[bool]string{true: "/json", false: "/text"}[asJSON], func(t *testing.T) {
				ctx := context.WithValue(context.Background(), struct{}{}, "fixture")
				r := cliNetworkReport()
				calls := 0
				command := newNetworkCommand(func(got context.Context) (network.Report, error) {
					calls++
					if got != ctx {
						t.Fatal("lost caller context")
					}
					return r, nil
				})
				var out bytes.Buffer
				command.SetOut(&out)
				command.SetErr(io.Discard)
				args := []string{name}
				if asJSON {
					args = append(args, "--json")
				}
				command.SetArgs(args)
				if err := command.ExecuteContext(ctx); err != nil || calls != 1 {
					t.Fatalf("calls %d err %v", calls, err)
				}
				first := out.String()
				out.Reset()
				command.SetArgs(args)
				if err := command.ExecuteContext(ctx); err != nil || out.String() != first {
					t.Fatal("output is not deterministic", err)
				}
				if asJSON {
					var object map[string]json.RawMessage
					if err := json.Unmarshal(out.Bytes(), &object); err != nil {
						t.Fatal(err)
					}
					field := name
					if name == "status" {
						field = "interfaces"
					}
					if object[field] == nil || object["sources"] == nil {
						t.Fatal("missing JSON section")
					}
					if name == "dns" {
						var dns network.DNS
						if err := json.Unmarshal(object["dns"], &dns); err != nil || !reflect.DeepEqual(dns, r.DNS) {
							t.Fatalf("DNS JSON %+v %v", dns, err)
						}
					}
				} else {
					for _, value := range map[string][]string{"status": {"enp1s0", "192.0.2.9/24", "2001:db8::9/64", "192.0.2.1", "local stub", "Upstream DNS: unknown"}, "interfaces": {"enp1s0", "state=up", "MTU=1500", "2001:db8::9/64"}, "routes": {"ipv4 0.0.0.0/0", "metric=100", "default"}, "dns": {"127.0.0.53 (local stub; upstream unknown)", "example.test", "stub-resolv.conf"}}[name] {
						if !strings.Contains(out.String(), value) {
							t.Fatalf("missing %s: %s", value, out.String())
						}
					}
					if strings.Contains(out.String(), "\x1b[") {
						t.Fatal("CLI needs no color")
					}
				}
			})
		}
	}
}

func TestNetworkCLIEmptyPartialAndErrors(t *testing.T) {
	for _, partial := range []bool{false, true} {
		r := network.Report{Interfaces: []network.Interface{}, Routes: []network.Route{}, DNS: network.DNS{Source: "/etc/resolv.conf", Nameservers: []network.Nameserver{}, SearchDomains: []string{}, UpstreamStatus: "unknown"}, Sources: []network.Source{}}
		if partial {
			r = cliNetworkReport()
			r.Sources = append(r.Sources, network.Source{Component: "routes_ipv6", Path: "/proc/net/ipv6_route", Status: "unavailable", Message: "file missing"})
		}
		cmd := newNetworkCommand(func(context.Context) (network.Report, error) { return r, nil })
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"status"})
		if err := cmd.Execute(); err != nil {
			t.Fatal("optional source caused entire command failure", err)
		}
		if partial && !strings.Contains(out.String(), "unavailable") {
			t.Fatal("missing partial warning")
		}
		if !partial && (!strings.Contains(out.String(), "No interfaces observed") || !strings.Contains(out.String(), "No matching routes observed")) {
			t.Fatal(out.String())
		}
	}
	for _, cause := range []error{network.ErrUnsupportedPlatform, context.Canceled, errors.New("collection failed")} {
		cmd := newNetworkCommand(func(context.Context) (network.Report, error) { return network.Report{}, cause })
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(io.Discard)
		cmd.SetArgs([]string{"status", "--json"})
		if err := cmd.Execute(); !errors.Is(err, cause) || out.Len() != 0 {
			t.Fatalf("err %v output %s", err, out.String())
		}
	}
	cmd := newNetworkCommand(func(context.Context) (network.Report, error) { return cliNetworkReport(), nil })
	cmd.SetOut(&setupOutput{allowed: 0, err: io.ErrClosedPipe})
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"dns"})
	if err := cmd.Execute(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("write error lost", err)
	}
}

func TestNetworkCommandRegistrationAndCompletion(t *testing.T) {
	for _, name := range []string{"status", "interfaces", "routes", "dns"} {
		cmd, _, err := NewRootCommand().Find([]string{"network", name})
		if err != nil || cmd.Name() != name || cmd.Flags().Lookup("json") == nil {
			t.Fatalf("missing command %s %v", name, err)
		}
	}
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(io.Discard)
	root.SetArgs([]string{"__complete", "network", "st"})
	if err := root.Execute(); err != nil || !strings.Contains(out.String(), "status") {
		t.Fatal("completion regression", err)
	}
	for _, args := range [][]string{{"interfaces", "extra"}, {"status", "--yes"}, {"dns", "--unknown"}} {
		cmd := newNetworkCommand(func(context.Context) (network.Report, error) {
			t.Fatal("invalid arguments called backend")
			return network.Report{}, nil
		})
		cmd.SetOut(io.Discard)
		cmd.SetErr(io.Discard)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatal("invalid arguments accepted", args)
		}
	}
	if runtime.GOOS != "linux" {
		root := NewRootCommand()
		root.SetOut(io.Discard)
		root.SetErr(io.Discard)
		root.SetArgs([]string{"network", "status"})
		if err := root.Execute(); !errors.Is(err, network.ErrUnsupportedPlatform) {
			t.Fatal(err)
		}
	}
}
