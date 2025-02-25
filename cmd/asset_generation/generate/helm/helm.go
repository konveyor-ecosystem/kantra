package helm

import (
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"k8s.io/helm/pkg/strvals"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

const (
	konveyorDirectoryName = "files/konveyor"
)

var (
	input      string
	outputDir  string
	chartDir   string
	nonK8SOnly bool
	setValues  []string
)

func NewGenerateHelmCommand(log logr.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "helm",
		Short: "generate the helm template manifests",
		RunE: func(cmd *cobra.Command, args []string) error {
			return helm(cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&chartDir, "chart-dir", "", "Directory to the Helm chart to use for chart generation.")
	cmd.Flags().StringVar(&input, "input", "", "Specifies the discover manifest file")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "Directory to save the generated Helm chart. Defaults to stdout")
	cmd.Flags().BoolVar(&nonK8SOnly, "non-k8s-only", false, "Render only the non-Kubernetes templates located in the files/konveyor directory of the chart")
	cmd.Flags().StringArrayVar(&setValues, "set", []string{}, "Set values on the command line (can specify multiple or separate values with commas: key1=val1,key2=val2)")

	// Validation
	cmd.MarkFlagDirname("chart-dir")
	cmd.MarkFlagDirname("output-dir")
	cmd.MarkFlagFilename("input")
	// Required
	cmd.MarkFlagRequired("chart-dir")
	cmd.MarkFlagRequired("input")

	return cmd
}

func helm(out io.Writer) error {
	chart, err := loadChart()
	if err != nil {
		return err
	}
	values, err := loadDiscoverManifest()
	if err != nil {
		return err
	}
	if len(setValues) > 0 {
		v, err := parseValues()
		if err != nil {
			return err
		}
		maps.Copy(values, v)
	}
	chart.Values = values
	rendered := make(map[string]string)
	if !nonK8SOnly {
		rendered, err = generateK8sTemplates(*chart)
		if err != nil {
			return err
		}
	}
	r, err := generateNonK8sTemplates(*chart)
	if err != nil {
		return err
	}
	maps.Copy(rendered, r)

	o := output{out: out}
	print := o.toStdout
	if outputDir != "" {
		err = os.MkdirAll(outputDir, 0755)
		if err != nil {
			return err
		}
		print = toFile
	}
	for f, c := range rendered {
		err = print(f, c)
		if err != nil {
			return err
		}
	}
	return nil
}

type output struct {
	out io.Writer
}

func (o output) toStdout(filename, contents string) error {
	fmt.Fprintf(o.out, "---\n# Source: %s\n%s", filename, contents)
	return nil
}

func toFile(filename, contents string) error {
	fn := filepath.Base(filename)
	dst := filepath.Join(outputDir, fn)
	// Add an extra line to make it yaml compliant since helm doesn't seem to do it.
	contents = fmt.Sprintln(contents)
	return os.WriteFile(dst, []byte(contents), 0644)
}

func loadChart() (*chart.Chart, error) {
	l, err := loader.Loader(chartDir)
	if err != nil {
		return nil, fmt.Errorf("unable to load chart: %s", err)
	}
	return l.Load()
}

func loadDiscoverManifest() (map[string]interface{}, error) {
	d, err := os.ReadFile(input)
	if err != nil {
		return nil, fmt.Errorf("unable to load discover manifest: %s", err)
	}
	var m map[string]interface{}
	err = yaml.Unmarshal(d, &m)
	return m, err
}

func generateK8sTemplates(chart chart.Chart) (map[string]string, error) {
	return generateTemplates(chart)
}

func generateNonK8sTemplates(chart chart.Chart) (map[string]string, error) {
	chart.Templates = filterTemplatesByPath(konveyorDirectoryName, chart.Files)
	return generateTemplates(chart)
}

func generateTemplates(chart chart.Chart) (map[string]string, error) {
	e := engine.Engine{}
	options := chartutil.ReleaseOptions{
		Name:      chart.Name(),
		Namespace: "",
		Revision:  1,
		IsInstall: false,
		IsUpgrade: false,
	}
	valuesToRender, err := chartutil.ToRenderValues(&chart, chart.Values, options, chartutil.DefaultCapabilities.Copy())
	if err != nil {
		return nil, fmt.Errorf("failed to render the values for chart %s: %s", chart.Name(), err)
	}
	chart.Values = valuesToRender
	rendered, err := e.Render(&chart, valuesToRender)
	if err != nil {
		return nil, fmt.Errorf("failed to render the templates for chart %s: %s", chart.Name(), err)
	}
	return rendered, nil

}

func filterTemplatesByPath(pathPrefix string, files []*chart.File) []*chart.File {
	ret := []*chart.File{}
	for _, f := range files {
		if strings.HasPrefix(f.Name, pathPrefix) {
			ret = append(ret, f)
		}
	}
	return ret
}

func parseValues() (map[string]interface{}, error) {
	// User specified a value via --set
	base := make(map[string]interface{})
	for _, value := range setValues {
		if err := strvals.ParseInto(value, base); err != nil {
			return nil, fmt.Errorf("failed parsing --set data:%s", err)
		}
	}
	return base, nil
}
