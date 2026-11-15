package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ih0r-d/hostup/src/internal/setup"
	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/ih0r-d/hostup/src/internal/tui"
	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCommand() *cobra.Command {
	return newRootCommand(tui.Run, tui.IsInteractive)
}

func newRootCommand(run func(context.Context, io.Reader, io.Writer) error, interactive func(io.Reader, io.Writer) bool) *cobra.Command {
	launch := func(cmd *cobra.Command, _ []string) error {
		if !interactive(cmd.InOrStdin(), cmd.OutOrStdout()) {
			return fmt.Errorf("hostup tui requires interactive input and output terminals; TERM=dumb is unsupported")
		}
		return run(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
	}
	root := &cobra.Command{
		Use:           "hostup",
		Short:         "A lightweight Linux setup and management toolkit",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if interactive(cmd.InOrStdin(), cmd.OutOrStdout()) {
				return launch(cmd, args)
			}
			var names []string
			for _, command := range cmd.Commands() {
				if command.IsAvailableCommand() {
					names = append(names, command.Name())
				}
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "Usage: hostup [command]\n\nCommands: %s\nRun hostup --help for details.\n", strings.Join(names, ", "))
			return err
		},
	}

	root.SetVersionTemplate("hostup {{.Version}}\n")
	root.AddCommand(
		&cobra.Command{Use: "tui", Short: "Open the interactive Hostup dashboard", Args: cobra.NoArgs, RunE: launch},
		newVersionCommand(),
		newInfoCommand(),
		newDoctorCommand(system.Diagnose),
		newServiceCommand(system.NewServiceManager(nil), confirmService),
		newSetupCommand(setup.NewEngine(nil, nil), confirmService, setupIsTerminal),
	)

	return root
}
