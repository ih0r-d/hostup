package network

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

func parseDNS(ctx context.Context, data string, dns *DNS) error {
	var firstErr error
	scanner := bufio.NewScanner(strings.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		text := scanner.Text()
		if index := strings.IndexAny(text, "#;"); index >= 0 {
			text = text[:index]
		}
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			var addr netip.Addr
			var err error
			if len(fields) == 2 {
				addr, err = netip.ParseAddr(fields[1])
			} else {
				err = errors.New("invalid nameserver directive")
			}
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("line %d: invalid nameserver address", line)
				}
				continue
			}
			dns.Nameservers = append(dns.Nameservers, Nameserver{Address: addr.String(), LocalStub: addr.IsLoopback()})
		case "search", "domain":
			if len(fields) < 2 || (fields[0] == "domain" && len(fields) != 2) {
				if firstErr == nil {
					firstErr = fmt.Errorf("line %d: invalid search/domain directive", line)
				}
				continue
			}
			// Preserve configured order and last-directive semantics, not a guessed
			// resolver search path derived from the local hostname.
			dns.SearchDomains = append([]string{}, fields[1:]...)
		}
	}
	return errors.Join(firstErr, scanner.Err())
}
