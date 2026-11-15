package cli

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/spf13/cobra"
)

const completionTimeout = 500 * time.Millisecond

func completeServices(manager *system.ServiceManager) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
		defer cancel()
		if ctx.Err() != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names, _ := manager.UnitNames(ctx)
		return matchingCompletions(ctx, names, prefix), cobra.ShellCompDirectiveNoFileComp
	}
}

type completionRunner func(context.Context, string, ...string) ([]byte, error)

func completionCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 100 * time.Millisecond
	return cmd.Output()
}

// Zone tables are small, local and do not require a daemon. Query timedatectl
// only when both tables are unavailable; UTC remains a useful offline fallback.
func completeTimezones(readFile func(string) ([]byte, error), run completionRunner) cobra.CompletionFunc {
	return func(cmd *cobra.Command, _ []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if cmd.Name() == "wizard" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), completionTimeout)
		defer cancel()
		if ctx.Err() != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		names := []string{"UTC"}
		found := false
		for _, path := range []string{"/usr/share/zoneinfo/zone1970.tab", "/usr/share/zoneinfo/zone.tab"} {
			data, err := readFile(path)
			if err != nil {
				continue
			}
			for line := range strings.SplitSeq(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 3 && !strings.HasPrefix(fields[0], "#") {
					names = append(names, fields[2])
				}
			}
			found = true
			break
		}
		if !found {
			if out, err := run(ctx, "timedatectl", "--no-ask-password", "list-timezones", "--no-pager"); err == nil {
				names = append(names, strings.Fields(string(out))...)
			}
		}
		return matchingCompletions(ctx, names, prefix), cobra.ShellCompDirectiveNoFileComp
	}
}

func matchingCompletions(ctx context.Context, names []string, prefix string) []string {
	var matches []string
	for _, name := range names {
		if ctx.Err() != nil {
			return nil
		}
		if strings.HasPrefix(name, prefix) {
			matches = append(matches, name)
		}
	}
	slices.Sort(matches)
	return slices.Compact(matches)
}
