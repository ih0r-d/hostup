package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/ih0r-d/hostup/internal/setup"
	"github.com/spf13/cobra"
)

func newSetupCommand(engine *setup.Engine, prompt func(io.Reader, io.Writer, string) (bool, error), terminal func(io.Reader) bool) *cobra.Command {
	var hostname, timezone string
	command := &cobra.Command{Use: "setup", Short: "Plan and apply Linux setup", Args: cobra.NoArgs}
	command.PersistentFlags().StringVar(&hostname, "hostname", "", "Desired lowercase hostname (omit to keep current)")
	command.PersistentFlags().StringVar(&timezone, "timezone", "", "Desired IANA timezone (omit to keep current)")
	planFor := func(cmd *cobra.Command) (setup.Plan, error) {
		desired := setup.Desired{}
		if cmd.Flags().Changed("hostname") {
			desired.Hostname = &hostname
		}
		if cmd.Flags().Changed("timezone") {
			desired.Timezone = &timezone
		}
		return engine.Plan(cmd.Context(), desired)
	}
	var jsonOutput bool
	plan := &cobra.Command{Use: "plan", Short: "Preview setup without changing the system", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		plan, err := planFor(cmd)
		if err != nil {
			return err
		}
		if jsonOutput {
			encoder := json.NewEncoder(cmd.OutOrStdout())
			encoder.SetIndent("", "  ")
			return encoder.Encode(plan)
		}
		return writeSetupPlan(cmd.OutOrStdout(), plan)
	}}
	plan.Flags().BoolVar(&jsonOutput, "json", false, "Output the plan as JSON")
	var yes bool
	apply := &cobra.Command{Use: "apply", Short: "Confirm and apply a setup plan", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		plan, err := planFor(cmd)
		if err != nil {
			return err
		}
		if err := writeSetupPlan(cmd.OutOrStdout(), plan); err != nil {
			return err
		}
		if !yes {
			if !terminal(cmd.InOrStdin()) {
				return fmt.Errorf("setup apply requires an interactive terminal; pass --yes to confirm automation")
			}
			if plan.HasChanges() {
				confirmed, err := prompt(cmd.InOrStdin(), cmd.OutOrStdout(), "Apply this setup? [y/N] ")
				if err != nil {
					return fmt.Errorf("confirm setup: %w", err)
				}
				if !confirmed {
					return fmt.Errorf("setup apply cancelled")
				}
			}
		}
		result, applyErr := engine.Apply(cmd.Context(), plan)
		return errors.Join(applyErr, writeSetupResult(cmd.OutOrStdout(), result))
	}}
	apply.Flags().BoolVarP(&yes, "yes", "y", false, "Confirm without prompting")
	command.AddCommand(plan, apply)
	return command
}

func setupIsTerminal(reader io.Reader) bool {
	file, ok := reader.(*os.File)
	return ok && isTerminalFile(file)
}

func writeSetupPlan(out io.Writer, plan setup.Plan) error {
	if _, err := fmt.Fprintln(out, "Hostup Setup Plan\nSETTING\tCURRENT\tDESIRED\tACTION"); err != nil {
		return err
	}
	for _, item := range []struct {
		name   string
		change setup.Change
	}{{"hostname", plan.Hostname}, {"timezone", plan.Timezone}} {
		action := "unchanged"
		if item.change.Changed {
			action = "change"
		}
		if _, err := fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", item.name, item.change.Current, item.change.Desired, action); err != nil {
			return err
		}
	}
	return nil
}

func writeSetupResult(out io.Writer, result setup.Result) error {
	for _, group := range []struct {
		label  string
		fields []string
	}{{"Applied", result.Applied}, {"Unchanged", result.Unchanged}, {"Not attempted", result.Pending}} {
		for _, field := range group.fields {
			if _, err := fmt.Fprintf(out, "%s: %s\n", group.label, field); err != nil {
				return err
			}
		}
	}
	if result.Failed != "" {
		_, err := fmt.Fprintf(out, "Failed: %s (may have changed; inspect current configuration)\n", result.Failed)
		return err
	}
	return nil
}
