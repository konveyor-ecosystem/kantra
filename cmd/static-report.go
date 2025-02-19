package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"text/template"

	"github.com/go-logr/logr"
	"github.com/konveyor/analyzer-lsp/output/v1/konveyor"
	"gopkg.in/yaml.v2"
)

// taken from https://github.com/konveyor/static-report/blob/main/analyzer-output-parser/main.go

type Application struct {
	Id       string                  `yaml:"id" json:"id"`
	Name     string                  `yaml:"name" json:"name"`
	Rulesets []konveyor.RuleSet      `yaml:"rulesets" json:"rulesets"`
	DepItems []konveyor.DepsFlatItem `yaml:"depItems" json:"depItems"`

	analysisPath string `yaml:"-" json:"-"`
	depsPath     string `yaml:"-" json:"-"`
}

func validateFlags(analysisOutputPaths []string, appNames []string, depsOutputs []string, log logr.Logger) ([]*Application, error) {
	var applications []*Application
	if len(analysisOutputPaths) == 0 {
		return nil, fmt.Errorf("analysis output paths required")
	}
	if len(appNames) == 0 {
		return nil, fmt.Errorf("application names required")
	}
	if len(depsOutputs) == 0 {
		log.Info("dependency output path not provided, only parsing analysis output")
	}
	for idx, analysisPath := range analysisOutputPaths {
		currApp := &Application{
			Id:           fmt.Sprintf("%04d", idx),
			Rulesets:     make([]konveyor.RuleSet, 0),
			DepItems:     make([]konveyor.DepsFlatItem, 0),
			analysisPath: analysisPath,
		}
		if len(depsOutputs) > 0 {
			currApp.depsPath = depsOutputs[idx]
		}
		currApp.Name = appNames[idx]
		applications = append(applications, currApp)
	}

	return applications, nil
}

// loadApplications loads applications from provider config
func loadApplications(apps []*Application) error {
	for _, app := range apps {
		analysisReport, err := os.ReadFile(app.analysisPath)
		if err != nil {
			return err
		}
		err = yaml.Unmarshal(analysisReport, &app.Rulesets)
		if err != nil {
			return err
		}
		if app.depsPath != "" {
			depsReport, err := os.ReadFile(app.depsPath)
			if err != nil {
				return err
			}

			err = yaml.Unmarshal(depsReport, &app.DepItems)
			if err != nil {
				return err
			}
			// extras on dependencies trip JSON marshaling
			// we don't need them in the report, ignore them
			for idx := range app.DepItems {
				depItem := &app.DepItems[idx]
				for _, dep := range depItem.Dependencies {
					dep.Extras = make(map[string]interface{})
				}
			}
		}
		// extras on incidents trip JSON marshaling
		// we don't need them in the report, ignore them
		for idx := range app.Rulesets {
			rs := &app.Rulesets[idx]
			for mapKey, violation := range rs.Violations {
				violation.Extras = nil
				for idx := range violation.Incidents {
					inc := &violation.Incidents[idx]
					inc.Variables = make(map[string]interface{})
					// Propagate more detailed description to the Violation/display in UI
					if idx == 0 {
						violation.Description = fmt.Sprintf("%s\n\n%s", violation.Description, inc.Message)
					}
				}
				rs.Violations[mapKey] = violation
			}
		}
	}

	return nil
}

func generateJSBundle(apps []*Application, outputPath string, log logr.Logger) error {
	output, err := json.Marshal(apps)
	if err != nil {
		log.Error(err, "failed to marshal applications")
	}

	tmpl := template.Must(template.New("").Parse(`
window["apps"] = {{.Apps}}
`))
	file, err := os.Create(outputPath)
	if err != nil {
		log.Error(err, "failed to create JS output bundle")
	}
	defer file.Close()
	err = tmpl.Execute(file, struct {
		Apps string
	}{
		Apps: string(output),
	})
	return err
}
