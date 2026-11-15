package network

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

func parseRoutes(ctx context.Context, data, family string) ([]Route, error) {
	routes := []Route{}
	var firstErr error
	scanner := bufio.NewScanner(strings.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return routes, err
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		if family == "ipv4" && fields[0] == "Iface" && len(fields) > 1 && fields[1] == "Destination" {
			continue
		}
		route, err := parseRoute(fields, family)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("line %d: %w", line, err)
			}
			continue
		}
		routes = append(routes, route)
	}
	return routes, errors.Join(firstErr, scanner.Err())
}

func parseRoute(f []string, family string) (Route, error) {
	r := Route{Family: family, State: "inactive"}
	var destination, gateway netip.Addr
	var bits int
	var metric, flags uint64
	var err error
	if family == "ipv4" {
		if len(f) != 11 {
			return r, errors.New("malformed IPv4 route")
		}
		destination, err = ipv4Hex(f[1])
		if err != nil {
			return r, err
		}
		gateway, err = ipv4Hex(f[2])
		if err != nil {
			return r, err
		}
		mask, maskErr := ipv4Hex(f[7])
		if maskErr != nil {
			return r, maskErr
		}
		maskBytes := mask.As4()
		var length int
		bits, length = net.IPMask(maskBytes[:]).Size()
		if length != 32 {
			return r, errors.New("noncontiguous IPv4 route mask")
		}
		flags, err = strconv.ParseUint(f[3], 16, 32)
		if err != nil {
			return r, errors.New("invalid route flags")
		}
		metric, err = strconv.ParseUint(f[6], 10, 32)
		if err != nil {
			return r, errors.New("invalid route metric")
		}
		r.Interface = f[0]
		for _, index := range []int{4, 5, 8, 9, 10} {
			if _, err := strconv.ParseUint(f[index], 10, 64); err != nil {
				return r, errors.New("invalid IPv4 route counter/value")
			}
		}
	} else {
		if len(f) != 10 {
			return r, errors.New("malformed IPv6 route")
		}
		destination, err = ipv6Hex(f[0])
		if err != nil {
			return r, err
		}
		length, lengthErr := strconv.ParseUint(f[1], 16, 8)
		if lengthErr != nil || length > 128 {
			return r, errors.New("invalid IPv6 destination prefix length")
		}
		bits = int(length)
		src, srcErr := ipv6Hex(f[2])
		if srcErr != nil {
			return r, srcErr
		}
		srcBits, srcErr := strconv.ParseUint(f[3], 16, 8)
		if srcErr != nil || srcBits > 128 {
			return r, errors.New("invalid IPv6 source prefix length")
		}
		if srcBits > 0 {
			r.Source = netip.PrefixFrom(src, int(srcBits)).Masked().String()
		} else if !src.IsUnspecified() {
			return r, errors.New("inconsistent IPv6 source prefix")
		}
		gateway, err = ipv6Hex(f[4])
		if err != nil {
			return r, err
		}
		metric, err = strconv.ParseUint(f[5], 16, 32)
		if err != nil {
			return r, errors.New("invalid route metric")
		}
		flags, err = strconv.ParseUint(f[8], 16, 32)
		if err != nil {
			return r, errors.New("invalid route flags")
		}
		r.Interface = f[9]
		for _, index := range []int{6, 7} {
			if _, err := strconv.ParseUint(f[index], 16, 32); err != nil {
				return r, errors.New("invalid IPv6 route counter")
			}
		}
	}
	prefix := netip.PrefixFrom(destination, bits)
	if prefix.Masked().Addr() != destination {
		return r, errors.New("route destination has host bits set")
	}
	r.Destination = prefix.String()
	r.Default = bits == 0
	if !gateway.IsUnspecified() {
		r.Gateway = gateway.String()
	}
	if r.Interface == "*" {
		r.Interface = ""
	}
	value := uint32(metric)
	r.Metric = &value
	if flags&1 != 0 {
		r.State = "active"
	}
	if flags&0x200 != 0 {
		r.State = "rejected"
	}
	return r, nil
}

// IPv4 procfs prints network-order words as host integers; decode using the
// native byte order rather than reversing every address unconditionally.
func ipv4Hex(value string) (netip.Addr, error) {
	if len(value) != 8 {
		return netip.Addr{}, errors.New("invalid IPv4 route address")
	}
	n, err := strconv.ParseUint(value, 16, 32)
	if err != nil {
		return netip.Addr{}, errors.New("invalid IPv4 route hex")
	}
	var bytes [4]byte
	binary.NativeEndian.PutUint32(bytes[:], uint32(n))
	return netip.AddrFrom4(bytes), nil
}
func ipv6Hex(value string) (netip.Addr, error) {
	if len(value) != 32 {
		return netip.Addr{}, errors.New("invalid IPv6 route address")
	}
	bytes, err := hex.DecodeString(value)
	if err != nil {
		return netip.Addr{}, errors.New("invalid IPv6 route hex")
	}
	var address [16]byte
	copy(address[:], bytes)
	return netip.AddrFrom16(address), nil
}
