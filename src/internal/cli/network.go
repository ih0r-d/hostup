package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/ih0r-d/hostup/src/internal/network"
	"github.com/spf13/cobra"
)

func newNetworkCommand(inspect func(context.Context) (network.Report, error)) *cobra.Command {
	group := &cobra.Command{Use: "network", Short: "Inspect local Linux networking without connectivity probes", Args: cobra.NoArgs}
	for _, name := range []string{"status", "interfaces", "routes", "dns"} {
		var jsonOutput bool
		command := &cobra.Command{Use: name, Short: "Inspect network " + name, Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := inspect(cmd.Context())
			if err != nil {
				return err
			}
			sources := networkSources(report.Sources, name)
			if jsonOutput {
				var value any = report
				switch name {
				case "interfaces":
					value = struct {
						Interfaces []network.Interface `json:"interfaces"`
						Sources    []network.Source    `json:"sources"`
					}{report.Interfaces, sources}
				case "routes":
					value = struct {
						Routes      []network.Route  `json:"routes"`
						Sources     []network.Source `json:"sources"`
						Limitations []string         `json:"limitations"`
					}{report.Routes, sources, report.Limitations}
				case "dns":
					value = struct {
						DNS     network.DNS      `json:"dns"`
						Sources []network.Source `json:"sources"`
					}{report.DNS, sources}
				}
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(value)
			}
			return writeNetwork(cmd.OutOrStdout(), name, report, sources)
		}}
		command.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
		group.AddCommand(command)
	}
	return group
}

func networkSources(sources []network.Source, section string) []network.Source {
	result := []network.Source{}
	for _, source := range sources {
		if section == "status" || (section == "interfaces" && strings.HasPrefix(source.Component, "interface")) || (section == "routes" && strings.HasPrefix(source.Component, "routes_")) || (section == "dns" && strings.HasPrefix(source.Component, "dns")) {
			result = append(result, source)
		}
	}
	return result
}

func writeNetwork(out io.Writer, section string, r network.Report, sources []network.Source) error {
	var b bytes.Buffer
	fmt.Fprintln(&b, "Hostup Network — local inspection")
	if section == "status" || section == "interfaces" {
		fmt.Fprintln(&b, "\nInterfaces")
		if len(r.Interfaces) == 0 {
			fmt.Fprintln(&b, "No interfaces observed (check source status below).")
		}
		for _, iface := range r.Interfaces {
			fmt.Fprintf(&b, "%s (index %d)  admin=%s  state=%s  type=%s  MTU=%d", networkText(iface.Name), iface.Index, iface.AdministrativeState, iface.OperationalState, iface.Type, iface.MTU)
			if iface.Loopback {
				fmt.Fprint(&b, "  loopback")
			}
			if iface.MAC != "" {
				fmt.Fprintf(&b, "  MAC=%s", iface.MAC)
			}
			fmt.Fprintln(&b)
			fmt.Fprintf(&b, "  IPv4: %s\n  IPv6: %s\n", networkList(iface.IPv4), networkList(iface.IPv6))
		}
	}
	if section == "status" || section == "routes" {
		title := "Routes"
		if section == "status" {
			title = "Default routes"
		}
		fmt.Fprintln(&b, "\n"+title)
		count := 0
		for _, route := range r.Routes {
			if section == "status" && !route.Default {
				continue
			}
			count++
			metric := "unknown"
			if route.Metric != nil {
				metric = fmt.Sprint(*route.Metric)
			}
			fmt.Fprintf(&b, "%s %s  via=%s  dev=%s  metric=%s  state=%s", route.Family, route.Destination, networkValue(route.Gateway), networkValue(route.Interface), metric, route.State)
			if route.Default {
				fmt.Fprint(&b, "  default")
			}
			if route.Source != "" {
				fmt.Fprintf(&b, "  from=%s", route.Source)
			}
			fmt.Fprintln(&b)
		}
		if count == 0 {
			fmt.Fprintln(&b, "No matching routes observed (check source status below).")
		}
	}
	if section == "status" || section == "dns" {
		fmt.Fprintf(&b, "\nDNS\nSource: %s", r.DNS.Source)
		if r.DNS.ResolvedSource != "" {
			fmt.Fprintf(&b, " -> %s", networkText(r.DNS.ResolvedSource))
		}
		fmt.Fprintln(&b)
		if len(r.DNS.Nameservers) == 0 {
			fmt.Fprintln(&b, "No nameservers observed (check resolver source status below).")
		}
		for _, server := range r.DNS.Nameservers {
			kind := "configured nameserver"
			if server.LocalStub {
				kind = "local stub; upstream unknown"
			}
			fmt.Fprintf(&b, "  %s (%s)\n", server.Address, kind)
		}
		fmt.Fprintf(&b, "Search domains: %s\nUpstream DNS: %s\n", networkList(r.DNS.SearchDomains), r.DNS.UpstreamStatus)
	}
	fmt.Fprintln(&b, "\nSources")
	for _, source := range sources {
		fmt.Fprintf(&b, "%s: %s (%s)", networkText(source.Component), source.Status, networkText(source.Path))
		if source.Message != "" {
			fmt.Fprintf(&b, " — %s", networkText(source.Message))
		}
		fmt.Fprintln(&b)
	}
	if section == "status" || section == "routes" {
		for _, limit := range r.Limitations {
			fmt.Fprintln(&b, limit)
		}
	}
	_, err := io.WriteString(out, b.String())
	return err
}

func networkValue(value string) string {
	if value == "" {
		return "-"
	}
	return networkText(value)
}
func networkList(values []string) string {
	if len(values) == 0 {
		return "none observed"
	}
	return networkText(strings.Join(values, ", "))
}
func networkText(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}
