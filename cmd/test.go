package cmd

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	"github.com/konveyor-ecosystem/kantra/pkg/testing"
	"github.com/spf13/cobra"
)

type testCommand struct {
	testFilterString     string
	baseProviderSettings string
}

func NewTestCommand(log logr.Logger) *cobra.Command {
	testCmd := &testCommand{}

	testCobraCommand := &cobra.Command{
		Use:   "test",
		Short: "Test YAML rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			var testFilter testing.TestsFilter
			if testCmd.testFilterString != "" {
				testFilter = testing.NewInlineNameBasedFilter(testCmd.testFilterString)
			}
			tests, err := testing.Parse(args, testFilter)
			if err != nil {
				log.Error(err, "failed parsing rulesets")
				return err
			}
			if len(tests) == 0 {
				log.Info("no tests found")
				return nil
			}
			results, allPass, err := testing.NewRunner().Run(tests, testing.TestOptions{
				RunLocal:         Settings.RunLocal,
				ContainerImage:   Settings.RunnerImage,
				ContainerToolBin: Settings.ContainerBinary,
				ProgressPrinter:  testing.PrintProgress,
				Log:              log.V(3),
			})
			testing.PrintSummary(os.Stdout, results)
			if err != nil {
				log.Error(err, "failed running tests")
				return err
			}
			if !allPass {
				return fmt.Errorf("one or more tests failed")
			}
			return nil
		},
	}
	testCobraCommand.Flags().StringVarP(&testCmd.testFilterString, "test-filter", "t", "", "filter tests / testcases by their names")
	testCobraCommand.Flags().StringVarP(&testCmd.baseProviderSettings, "base-provider-settings", "b", "", "path to a provider settings file the runner will use as base")
	return testCobraCommand
}
