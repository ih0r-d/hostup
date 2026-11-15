package cli

import (
	"github.com/ih0r-d/hostup/src/internal/setup"
	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/spf13/cobra"
)

var version = "dev"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "hostup",
		Short:         "A lightweight Linux setup and management toolkit",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       version,
	}

	root.SetVersionTemplate("hostup {{.Version}}\n")
	root.AddCommand(
		newVersionCommand(),
		newInfoCommand(),
		newDoctorCommand(system.Diagnose),
		newServiceCommand(system.NewServiceManager(nil), confirmService),
		newSetupCommand(setup.NewEngine(nil, nil), confirmService, setupIsTerminal),
	)

	return root
}
