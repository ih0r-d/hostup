// Package network inspects local Linux network configuration without commands,
// privileges, connectivity probes, or configuration changes.
package network

import (
	"cmp"
	"slices"
)

type Interface struct {
	Name                string   `json:"name"`
	Index               int      `json:"index"`
	Type                string   `json:"type"`
	AdministrativeState string   `json:"administrative_state"`
	OperationalState    string   `json:"operational_state"`
	MTU                 int      `json:"mtu"`
	MAC                 string   `json:"mac,omitempty"`
	IPv4                []string `json:"ipv4"`
	IPv6                []string `json:"ipv6"`
	Loopback            bool     `json:"loopback"`
}

type Route struct {
	Family         string  `json:"family"`
	Destination    string  `json:"destination"`
	Source         string  `json:"source,omitempty"`
	Gateway        string  `json:"gateway,omitempty"`
	Interface      string  `json:"interface,omitempty"`
	InterfaceIndex *int    `json:"interface_index"`
	Metric         *uint32 `json:"metric"`
	Default        bool    `json:"default"`
	State          string  `json:"state"`
}

type Nameserver struct {
	Address   string `json:"address"`
	LocalStub bool   `json:"local_stub"`
}

type DNS struct {
	Source         string       `json:"source"`
	ResolvedSource string       `json:"resolved_source,omitempty"`
	Nameservers    []Nameserver `json:"nameservers"`
	SearchDomains  []string     `json:"search_domains"`
	UpstreamStatus string       `json:"upstream_status"`
}

// Source distinguishes available-but-empty data from unavailable, partial,
// failed, or unsupported inspection. Messages never contain raw input lines.
type Source struct {
	Component string `json:"component"`
	Path      string `json:"path"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}

type Report struct {
	Interfaces  []Interface `json:"interfaces"`
	Routes      []Route     `json:"routes"`
	DNS         DNS         `json:"dns"`
	Sources     []Source    `json:"sources"`
	Limitations []string    `json:"limitations"`
}

func emptyReport() Report {
	return Report{Interfaces: []Interface{}, Routes: []Route{}, DNS: DNS{Source: "/etc/resolv.conf", Nameservers: []Nameserver{}, SearchDomains: []string{}, UpstreamStatus: "unknown"}, Sources: []Source{}, Limitations: []string{
		"Routing is limited to procfs views: IPv4 main table; IPv6 entries have no table IDs. Policy rules and complete multipath routing are not inspected.",
		"Resolver configuration describes configured nameservers, not upstream servers behind local stubs. NSS and per-link resolver policies are not inspected.",
		"Local snapshots are not atomic. Interface/route presence does not establish connectivity; no probes are performed.",
	}}
}

func sortReport(r *Report) {
	slices.SortFunc(r.Interfaces, func(a, b Interface) int {
		if c := cmp.Compare(a.Index, b.Index); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	slices.SortFunc(r.Routes, func(a, b Route) int {
		for _, pair := range [][2]string{{a.Family, b.Family}, {a.Destination, b.Destination}, {a.Source, b.Source}} {
			if c := cmp.Compare(pair[0], pair[1]); c != 0 {
				return c
			}
		}
		if a.Metric != nil && b.Metric != nil {
			if c := cmp.Compare(*a.Metric, *b.Metric); c != 0 {
				return c
			}
		} else if a.Metric != nil {
			return -1
		} else if b.Metric != nil {
			return 1
		}
		for _, pair := range [][2]string{{a.Interface, b.Interface}, {a.Gateway, b.Gateway}, {a.State, b.State}} {
			if c := cmp.Compare(pair[0], pair[1]); c != 0 {
				return c
			}
		}
		return 0
	})
	slices.SortFunc(r.Sources, func(a, b Source) int {
		if c := cmp.Compare(a.Component, b.Component); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Status, b.Status); c != 0 {
			return c
		}
		return cmp.Compare(a.Message, b.Message)
	})
}
