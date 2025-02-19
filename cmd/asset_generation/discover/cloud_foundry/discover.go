package cloud_foundry

import (
	"fmt"
	"io"
	"os"

	discover "github.com/gciavarrini/cf-application-discovery/pkg/discover/cloud_foundry"
	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

var (
	useLive bool
	input   string
	output  string
)

func NewDiscoverCloudFoundryCommand(log logr.Logger) (string, *cobra.Command) {

	cmd := &cobra.Command{
		Aliases: []string{"cf"},
		Use:     "cloud-foundry",
		Short:   "Discover Cloud Foundry applications",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := cmd.ParseFlags(args); err != nil {
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return discoverManifest(cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&input, "input", "", "specify the location of the manifest.yaml to analyze.")
	cmd.Flags().StringVar(&output, "output", "", "output file (default: standard output).")
	cmd.Flags().BoolVar(&useLive, "use-live-connection", false, "uses live platform connections for real-time discovery (not implemented)")
	cmd.MarkFlagFilename("input", "yaml", "yml")
	cmd.MarkFlagFilename("output")
	cmd.MarkFlagRequired("input")

	return "Cloud Foundry V3 (local manifest)", cmd
}

func discoverManifest(writer io.Writer) error {
	b, err := os.ReadFile(input)
	if err != nil {
		return err
	}

	ma := discover.AppManifest{}
	err = yaml.Unmarshal(b, &ma)
	if err != nil {
		return err
	}
	a, err := discover.Discover(ma, "1", "default")
	if err != nil {
		return err

	}

	b, err = yaml.Marshal(a)
	if err != nil {
		return err

	}
	if output == "" {
		fmt.Fprintf(writer, "%s\n", b)
		return nil
	}
	return os.WriteFile(output, b, 0444)
}
