package cli

import (
	"encoding/json"
	"fmt"

	"github.com/ih0r-d/hostup/src/internal/system"
	"github.com/spf13/cobra"
)

func newDoctorCommand(diagnose func() (system.Diagnostics, error)) *cobra.Command {
	var jsonOutput bool
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Display read-only Linux diagnostics",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			report, err := diagnose()
			if err != nil {
				return err
			}
			if jsonOutput {
				encoder := json.NewEncoder(cmd.OutOrStdout())
				encoder.SetIndent("", "  ")
				return encoder.Encode(report)
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(),
				"Hostup Diagnostics\nOS:           %s (%s %s)\nArchitecture: %s\nHostname:     %s\nCPU:          %d logical CPUs\nMemory:       %.2f GiB available / %.2f GiB total\nDisk (%s):     %.2f GiB used / %.2f GiB total (%.2f GiB available)\n",
				report.Platform.OS, report.Platform.Distribution, report.Platform.Version,
				report.Platform.Architecture, report.Platform.Hostname, report.CPU.LogicalCPUs,
				gibibytes(report.Memory.AvailableBytes), gibibytes(report.Memory.TotalBytes),
				report.Disk.Path, gibibytes(report.Disk.UsedBytes), gibibytes(report.Disk.TotalBytes), gibibytes(report.Disk.AvailableBytes))
			return err
		},
	}
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func gibibytes(bytes uint64) float64 {
	return float64(bytes) / (1 << 30)
}
