package discover

import (
	"fmt"
	"io"

	"github.com/go-logr/logr"
	"github.com/konveyor-ecosystem/kantra/cmd/asset_generation/discover/cloud_foundry"
	"github.com/spf13/cobra"
)

var (
	platforms []string
)

func NewDiscoverCommand(log logr.Logger) *cobra.Command {
	var (
		showSupportedPlatforms bool
	)
	cmd := &cobra.Command{
		Use:     "discover",
		GroupID: "assetGeneration",
		Short:   "Discover application outputs a YAML representation of source platform resources",
		Run: func(cmd *cobra.Command, _ []string) {
			if showSupportedPlatforms {
				listPlatforms(cmd.OutOrStdout())
			} else {
				cmd.Help()
			}
		},
	}
	cmd.Flags().BoolVar(&showSupportedPlatforms, "list-platforms", false, "List available supported discovery platform.")

	// Cloud Foundry V3
	p, c := cloud_foundry.NewDiscoverCloudFoundryCommand(log)
	cmd.AddCommand(c)
	platforms = append(platforms, p)
	return cmd
}

func listPlatforms(out io.Writer) {
	fmt.Fprintln(out, "Supported platforms:")
	for _, p := range platforms {
		fmt.Fprintf(out, "- %s\n", p)
	}
}
