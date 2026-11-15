package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/ih0r-d/hostup/internal/setup"
	"github.com/spf13/cobra"
)

func newSetupWizardCommand(engine *setup.Engine, run func(context.Context, *setup.Engine, io.Reader, io.Writer) error) *cobra.Command {
	return &cobra.Command{
		Use: "wizard", Short: "Configure this host interactively", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("hostname") || cmd.Flags().Changed("timezone") {
				return fmt.Errorf("wizard edits settings interactively; use setup plan/apply with --hostname or --timezone")
			}
			return run(cmd.Context(), engine, cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
}
