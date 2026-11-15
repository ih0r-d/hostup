package cli

import (
	"encoding/json"
	"fmt"

	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/spf13/cobra"
)

func newInfoCommand() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "info",
		Short: "Display Linux system information",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			platform, err := system.DetectPlatform()
			if err != nil {
				return err
			}

			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(platform)
			}

			out := cmd.OutOrStdout()

			fmt.Fprintln(out, "Hostup System Information")
			fmt.Fprintf(out, "OS:           %s\n", platform.OS)
			fmt.Fprintf(out, "Distribution: %s\n", platform.Distribution)
			fmt.Fprintf(out, "Version:      %s\n", platform.Version)
			fmt.Fprintf(out, "Architecture: %s\n", platform.Architecture)
			fmt.Fprintf(out, "Hostname:     %s\n", platform.Hostname)

			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")

	return cmd
}
