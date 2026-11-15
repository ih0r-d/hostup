package network

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
)

var ErrUnsupportedPlatform = errors.New("network inspection requires Linux")

// These function dependencies are private: native collection and fixture tests
// exercise the same implementation, without a provider framework.
type sources struct {
	interfaces func() ([]net.Interface, error)
	addresses  func(net.Interface) ([]net.Addr, error)
	readFile   func(string) ([]byte, error)
	resolve    func(string) (string, error)
}

// Inspect returns useful partial data with source-level diagnostics. Only an
// unsupported platform or cancellation/deadline returns a top-level error.
// Standard-library/kernel reads are local; no DNS resolution is performed.
func Inspect(ctx context.Context) (Report, error) {
	return inspect(ctx, runtime.GOOS, sources{net.Interfaces, func(i net.Interface) ([]net.Addr, error) { return i.Addrs() }, os.ReadFile, filepath.EvalSymlinks})
}

func inspect(ctx context.Context, goos string, s sources) (Report, error) {
	r := emptyReport()
	if goos != "linux" {
		r.Sources = append(r.Sources, Source{Component: "network", Path: goos, Status: "unsupported", Message: ErrUnsupportedPlatform.Error()})
		return r, fmt.Errorf("%w: %s", ErrUnsupportedPlatform, goos)
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return r, err
	}
	r.collectInterfaces(ctx, s)
	for _, family := range []string{"ipv4", "ipv6"} {
		if err := ctx.Err(); err != nil {
			sortReport(&r)
			return r, err
		}
		path := "/proc/net/route"
		if family == "ipv6" {
			path = "/proc/net/ipv6_route"
		}
		data, err := s.readFile(path)
		var routes []Route
		if err == nil {
			routes, err = parseRoutes(ctx, string(data), family)
		}
		state := source("routes_"+family, path, err)
		if len(routes) > 0 && err != nil {
			state.Status = "partial"
		}
		r.Routes = append(r.Routes, routes...)
		r.Sources = append(r.Sources, state)
	}
	if err := ctx.Err(); err != nil {
		sortReport(&r)
		return r, err
	}
	data, err := s.readFile(r.DNS.Source)
	if err == nil {
		resolved, resolveErr := s.resolve(r.DNS.Source)
		if resolveErr == nil {
			r.DNS.ResolvedSource = resolved
		} else {
			r.Sources = append(r.Sources, source("dns_source", r.DNS.Source, resolveErr))
		}
		err = parseDNS(ctx, string(data), &r.DNS)
	}
	state := source("dns", r.DNS.Source, err)
	if err != nil && (len(r.DNS.Nameservers) > 0 || len(r.DNS.SearchDomains) > 0) {
		state.Status = "partial"
	}
	r.Sources = append(r.Sources, state)
	indices := make(map[string]int, len(r.Interfaces))
	for _, iface := range r.Interfaces {
		indices[iface.Name] = iface.Index
	}
	for i := range r.Routes {
		if index, ok := indices[r.Routes[i].Interface]; ok {
			r.Routes[i].InterfaceIndex = &index
		}
	}
	sortReport(&r)
	return r, ctx.Err()
}

func source(component, path string, err error) Source {
	result := Source{Component: component, Path: path, Status: "available"}
	if err != nil {
		result.Status = "failed"
		if errors.Is(err, os.ErrNotExist) {
			result.Status = "unavailable"
		}
		result.Message = err.Error()
	}
	return result
}

func (r *Report) collectInterfaces(ctx context.Context, s sources) {
	items, err := s.interfaces()
	r.Sources = append(r.Sources, source("interfaces", "Go interface API", err))
	if err != nil {
		return
	}
	for _, item := range items {
		if ctx.Err() != nil {
			return
		}
		if item.Index <= 0 || item.Name == "" || item.Name == "." || item.Name == ".." || filepath.Base(item.Name) != item.Name {
			r.Sources = append(r.Sources, source("interfaces", "Go interface API", errors.New("invalid interface identity")))
			continue
		}
		iface := Interface{Name: item.Name, Index: item.Index, Type: "unknown", AdministrativeState: "down", OperationalState: "unknown", MTU: item.MTU, MAC: item.HardwareAddr.String(), IPv4: []string{}, IPv6: []string{}, Loopback: item.Flags&net.FlagLoopback != 0}
		if item.Flags&net.FlagUp != 0 {
			iface.AdministrativeState = "up"
		}
		if iface.Loopback {
			iface.Type = "loopback"
		}
		root := "/sys/class/net/" + item.Name + "/"
		for _, field := range []string{"type", "operstate"} {
			data, err := s.readFile(root + field)
			if err == nil {
				value := strings.TrimSpace(string(data))
				if field == "type" {
					var code uint64
					code, err = strconv.ParseUint(value, 10, 16)
					if err == nil {
						switch code {
						case 1:
							iface.Type = "ethernet"
						case 772:
							iface.Type = "loopback"
						case 512:
							iface.Type = "ppp"
						}
					}
				} else {
					switch value {
					case "unknown", "notpresent", "down", "lowerlayerdown", "testing", "dormant", "up":
						iface.OperationalState = value
					default:
						err = errors.New("invalid operational state")
					}
				}
			}
			r.Sources = append(r.Sources, source("interface "+item.Name+" "+field, root+field, err))
		}
		if ctx.Err() != nil {
			return
		}
		addresses, err := s.addresses(item)
		if err == nil {
			for _, address := range addresses {
				if address == nil {
					err = errors.Join(err, errors.New("missing interface address"))
					continue
				}
				prefix, parseErr := netip.ParsePrefix(address.String())
				if parseErr != nil {
					err = errors.Join(err, errors.New("invalid interface address prefix"))
					continue
				}
				if prefix.Addr().Is4() {
					iface.IPv4 = append(iface.IPv4, prefix.String())
				} else {
					iface.IPv6 = append(iface.IPv6, prefix.String())
				}
			}
		}
		state := source("interface "+item.Name+" addresses", "Go interface address API", err)
		if err != nil && (len(iface.IPv4) > 0 || len(iface.IPv6) > 0) {
			state.Status = "partial"
		}
		r.Sources = append(r.Sources, state)
		slices.Sort(iface.IPv4)
		iface.IPv4 = slices.Compact(iface.IPv4)
		slices.Sort(iface.IPv6)
		iface.IPv6 = slices.Compact(iface.IPv6)
		r.Interfaces = append(r.Interfaces, iface)
	}
}
