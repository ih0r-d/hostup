package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ih0r-d/hostup/internal/system"
	"github.com/spf13/cobra"
)

type servicePrompter func(io.Reader, io.Writer, string) (bool, error)

func newServiceCommand(manager *system.ServiceManager, prompt servicePrompter) *cobra.Command {
	service := &cobra.Command{Use: "service", Short: "Manage systemd services", Args: cobra.NoArgs}
	service.AddCommand(newServiceListCommand(manager), newServiceStatusCommand(manager), newServiceChangeCommand(manager, prompt, "start"), newServiceChangeCommand(manager, prompt, "stop"), newServiceChangeCommand(manager, prompt, "restart"), newServiceChangeCommand(manager, prompt, "enable"), newServiceChangeCommand(manager, prompt, "disable"), newServiceLogsCommand(manager))
	return service
}

func newServiceListCommand(manager *system.ServiceManager) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{Use: "list", Short: "List systemd services", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		services, err := manager.List(cmd.Context())
		if err != nil {
			return err
		}
		if jsonOutput {
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(services)
		}
		if len(services) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), "No services found")
			return err
		}
		if _, err = fmt.Fprintln(cmd.OutOrStdout(), "UNIT\tLOAD\tACTIVE\tSUB"); err != nil {
			return err
		}
		for _, svc := range services {
			if _, err = fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n", svc.Name, svc.Load, svc.Active, svc.Sub); err != nil {
				return err
			}
		}
		return nil
	}}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func newServiceStatusCommand(manager *system.ServiceManager) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{Use: "status <name>", Short: "Display service status", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if jsonOutput {
			status, err := manager.StatusInfo(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(status)
		}
		status, err := manager.Status(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		_, err = io.WriteString(cmd.OutOrStdout(), status)
		return err
	}}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func newServiceChangeCommand(manager *system.ServiceManager, prompt servicePrompter, action string) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: action + " <name>", Short: action + " a systemd service", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := system.ValidateServiceName(args[0]); err != nil {
			return err
		}
		if !yes {
			if !serviceIsTerminal(cmd.InOrStdin()) {
				return fmt.Errorf("%s requires an interactive terminal; pass --yes to confirm automation", action)
			}
			ok, err := prompt(cmd.InOrStdin(), cmd.OutOrStdout(), fmt.Sprintf("%s service %q? [y/N] ", action, args[0]))
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("%s cancelled", action)
			}
		}
		if err := manager.Change(cmd.Context(), action, args[0]); err != nil {
			return err
		}
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Service %s: %s\n", action, args[0])
		return err
	}}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	return cmd
}

func newServiceLogsCommand(manager *system.ServiceManager) *cobra.Command {
	var follow bool
	var lines int
	cmd := &cobra.Command{Use: "logs <name>", Short: "Display service journal logs", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		return manager.Logs(cmd.Context(), cmd.OutOrStdout(), args[0], lines, follow)
	}}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "Follow new log entries")
	cmd.Flags().IntVarP(&lines, "lines", "n", 50, "Number of recent lines (1-10000)")
	return cmd
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && terminalFile(f)
}

var serviceIsTerminal = isTerminal

var terminalFile = func(f *os.File) bool { return isTerminalFile(f) }

func confirmService(in io.Reader, out io.Writer, question string) (bool, error) {
	if _, err := io.WriteString(out, question); err != nil {
		return false, err
	}
	var answer string
	if _, err := fmt.Fscanln(in, &answer); err != nil {
		return false, err
	}
	return strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes"), nil
}
