package generate

import (
	"github.com/go-logr/logr"
	"github.com/konveyor-ecosystem/kantra/cmd/asset_generation/generate/helm"
	"github.com/spf13/cobra"
)

func NewGenerateCommand(log logr.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "generate",
		GroupID: "assetGeneration",
		Short:   "Analyze the source platform and/or application and output discovery manifest.",
		Run: func(cmd *cobra.Command, _ []string) {
			cmd.Help()
		},
	}
	cmd.AddCommand(helm.NewGenerateHelmCommand(log))

	return cmd
}
