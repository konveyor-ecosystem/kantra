package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"path"

	"path/filepath"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/konveyor/analyzer-lsp/engine"
	outputv1 "github.com/konveyor/analyzer-lsp/output/v1/konveyor"
	"github.com/konveyor/analyzer-lsp/provider"
	"go.lsp.dev/uri"
	"gopkg.in/yaml.v2"

	"github.com/spf13/cobra"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
)

var (
	// application source path inside the container
	SourceMountPath = path.Join(InputPath, "source")
	// analyzer config files
	ConfigMountPath = path.Join(InputPath, "config")
	// user provided rules path
	RulesMountPath = path.Join(RulesetPath, "input")
	// paths to files in the container
	AnalysisOutputMountPath   = path.Join(OutputPath, "output.yaml")
	DepsOutputMountPath       = path.Join(OutputPath, "dependencies.yaml")
	ProviderSettingsMountPath = path.Join(ConfigMountPath, "settings.json")
)

// kantra analyze flags
type analyzeCommand struct {
	listSources           bool
	listTargets           bool
	skipStaticReport      bool
	analyzeKnownLibraries bool
	jsonOutput            bool
	mavenSettingsFile     string
	sources               []string
	targets               []string
	input                 string
	output                string
	mode                  string
	rules                 []string

	// tempDirs list of temporary dirs created, used for cleanup
	tempDirs []string
	log      logr.Logger
	// isFileInput is set when input points to a file and not a dir
	isFileInput bool
}

// analyzeCmd represents the analyze command
func NewAnalyzeCmd(log logr.Logger) *cobra.Command {
	analyzeCmd := &analyzeCommand{
		log: log,
	}

	analyzeCommand := &cobra.Command{
		Use:   "analyze",
		Short: "Analyze application source code",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// TODO (pgaikwad): this is nasty
			if !cmd.Flags().Lookup("list-sources").Changed &&
				!cmd.Flags().Lookup("list-targets").Changed {
				cmd.MarkFlagRequired("input")
				cmd.MarkFlagRequired("output")
				if err := cmd.ValidateRequiredFlags(); err != nil {
					return err
				}
			}
			err := analyzeCmd.Validate()
			if err != nil {
				log.Error(err, "failed to validate flags")
				return err
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if analyzeCmd.listSources || analyzeCmd.listTargets {
				err := analyzeCmd.ListLabels(cmd.Context())
				if err != nil {
					log.Error(err, "failed to list rule labels")
					return err
				}
				return nil
			}
			xmlOutputDir, err := analyzeCmd.ConvertXML(cmd.Context())
			if err != nil {
				log.Error(err, "failed to convert xml rules")
				return err
			}
			err = analyzeCmd.RunAnalysis(cmd.Context(), xmlOutputDir)
			if err != nil {
				log.Error(err, "failed to run analysis")
				return err
			}
			err = analyzeCmd.CreateJSONOutput()
			if err != nil {
				log.Error(err, "failed to create json output file")
				return err
			}
			err = analyzeCmd.GenerateStaticReport(cmd.Context())
			if err != nil {
				log.Error(err, "failed to generate static report")
				return err
			}
			return nil
		},
		PostRunE: func(cmd *cobra.Command, args []string) error {
			if cmd.PersistentFlags().Changed(noCleanupFlag) {
				return nil
			}
			err := analyzeCmd.Clean(cmd.Context())
			if err != nil {
				log.Error(err, "failed to clean temporary container resources")
				return err
			}
			return nil
		},
	}
	analyzeCommand.Flags().BoolVar(&analyzeCmd.listSources, "list-sources", false, "list rules for available migration sources")
	analyzeCommand.Flags().BoolVar(&analyzeCmd.listTargets, "list-targets", false, "list rules for available migration targets")
	analyzeCommand.Flags().StringArrayVarP(&analyzeCmd.sources, "source", "s", []string{}, "source technology to consider for analysis")
	analyzeCommand.Flags().StringArrayVarP(&analyzeCmd.targets, "target", "t", []string{}, "target technology to consider for analysis")
	analyzeCommand.Flags().StringArrayVar(&analyzeCmd.rules, "rules", []string{}, "filename or directory containing rule files")
	analyzeCommand.Flags().StringVarP(&analyzeCmd.input, "input", "i", "", "path to application source code or a binary")
	analyzeCommand.Flags().StringVarP(&analyzeCmd.output, "output", "o", "", "path to the directory for analysis output")
	analyzeCommand.Flags().BoolVar(&analyzeCmd.skipStaticReport, "skip-static-report", false, "do not generate static report")
	analyzeCommand.Flags().BoolVar(&analyzeCmd.analyzeKnownLibraries, "analyze-known-libraries", false, "analyze known open-source libraries")
	analyzeCommand.Flags().StringVar(&analyzeCmd.mavenSettingsFile, "maven-settings", "", "path to a custom maven settings file to use")
	analyzeCommand.Flags().StringVarP(&analyzeCmd.mode, "mode", "m", string(provider.FullAnalysisMode), "analysis mode. Must be one of 'full' or 'source-only'")
	analyzeCommand.Flags().BoolVar(&analyzeCmd.jsonOutput, "json-output", false, "create analysis and dependency output as json")

	return analyzeCommand
}

func (a *analyzeCommand) Validate() error {
	if a.listSources || a.listTargets {
		return nil
	}
	stat, err := os.Stat(a.output)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			err = os.MkdirAll(a.output, os.ModePerm)
			if err != nil {
				return fmt.Errorf("%w failed to create output dir %s", err, a.output)
			}
		} else {
			return fmt.Errorf("failed to stat output directory %s", a.output)
		}
	}
	if stat != nil && !stat.IsDir() {
		return fmt.Errorf("output path %s is not a directory", a.output)
	}
	stat, err = os.Stat(a.input)
	if err != nil {
		return fmt.Errorf("%w failed to stat input path %s", err, a.input)
	}
	// when input isn't a dir, it's pointing to a binary
	// we need abs path to mount the file correctly
	if !stat.Mode().IsDir() {
		a.input, err = filepath.Abs(a.input)
		if err != nil {
			return fmt.Errorf("%w failed to get absolute path for input file %s", err, a.input)
		}
		// make sure we mount a file and not a dir
		SourceMountPath = path.Join(SourceMountPath, filepath.Base(a.input))
		a.isFileInput = true
	}
	if a.mode != string(provider.FullAnalysisMode) &&
		a.mode != string(provider.SourceOnlyAnalysisMode) {
		return fmt.Errorf("mode must be one of 'full' or 'source-only'")
	}
	if _, err := os.Stat(a.mavenSettingsFile); a.mavenSettingsFile != "" && err != nil {
		return fmt.Errorf("%w failed to stat maven settings file at path %s", err, a.mavenSettingsFile)
	}
	// try to get abs path, if not, continue with relative path
	if absPath, err := filepath.Abs(a.output); err == nil {
		a.output = absPath
	}
	if absPath, err := filepath.Abs(a.input); err == nil {
		a.input = absPath
	}
	if absPath, err := filepath.Abs(a.mavenSettingsFile); a.mavenSettingsFile != "" && err == nil {
		a.mavenSettingsFile = absPath
	}
	return nil
}

func (a *analyzeCommand) ListLabels(ctx context.Context) error {
	// reserved labels
	sourceLabel := outputv1.SourceTechnologyLabel
	targetLabel := outputv1.TargetTechnologyLabel
	runMode := "RUN_MODE"
	runModeContainer := "container"
	if os.Getenv(runMode) == runModeContainer {
		if a.listSources {
			sourceSlice, err := readRuleFilesForLabels(sourceLabel)
			if err != nil {
				a.log.Error(err, "failed to read rule labels")
				return err
			}
			listOptionsFromLabels(sourceSlice, sourceLabel)
			return nil
		}
		if a.listTargets {
			targetsSlice, err := readRuleFilesForLabels(targetLabel)
			if err != nil {
				a.log.Error(err, "failed to read rule labels")
				return err
			}
			listOptionsFromLabels(targetsSlice, targetLabel)
			return nil
		}
	} else {
		volumes, err := a.getRulesVolumes()
		if err != nil {
			a.log.Error(err, "failed getting rules volumes")
			return err
		}
		args := []string{"analyze"}
		if a.listSources {
			args = append(args, "--list-sources")
		} else {
			args = append(args, "--list-targets")
		}
		err = NewContainer(a.log).Run(
			ctx,
			WithEnv(runMode, runModeContainer),
			WithVolumes(volumes),
			WithEntrypointBin("/usr/local/bin/kantra"),
			WithEntrypointArgs(args...),
		)
		if err != nil {
			a.log.Error(err, "failed listing labels")
			return err
		}
	}
	return nil
}

func readRuleFilesForLabels(label string) ([]string, error) {
	labelsSlice := []string{}
	err := filepath.WalkDir(RulesetPath, walkRuleSets(RulesetPath, label, &labelsSlice))
	if err != nil {
		return nil, err
	}
	return labelsSlice, nil
}

func walkRuleSets(root string, label string, labelsSlice *[]string) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			*labelsSlice, err = readRuleFile(path, labelsSlice, label)
			if err != nil {
				return err
			}
		}
		return err
	}
}

func readRuleFile(filePath string, labelsSlice *[]string, label string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanWords)

	for scanner.Scan() {
		// add source/target labels to slice
		label := getSourceOrTargetLabel(scanner.Text(), label)
		if len(label) > 0 && !slices.Contains(*labelsSlice, label) {
			*labelsSlice = append(*labelsSlice, label)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return *labelsSlice, nil
}

func getSourceOrTargetLabel(text string, label string) string {
	if strings.Contains(text, label) {
		return text
	}
	return ""
}

func listOptionsFromLabels(sl []string, label string) {
	var newSl []string
	l := label + "="

	for _, label := range sl {
		newSt := strings.TrimPrefix(label, l)

		if newSt != label {
			newSl = append(newSl, newSt)
		}
	}
	sort.Strings(newSl)

	if label == outputv1.SourceTechnologyLabel {
		fmt.Println("available source technologies:")
	} else {
		fmt.Println("available target technologies:")
	}
	for _, tech := range newSl {
		fmt.Println(tech)
	}
}

func (a *analyzeCommand) getConfigVolumes() (map[string]string, error) {
	tempDir, err := os.MkdirTemp("", "analyze-config-")
	if err != nil {
		a.log.V(1).Error(err, "failed creating temp dir", "dir", tempDir)
		return nil, err
	}
	a.log.V(1).Info("created directory for provider settings", "dir", tempDir)
	a.tempDirs = append(a.tempDirs, tempDir)

	otherProvsMountPath := SourceMountPath
	// when input is a file, it means it's probably a binary
	// only java provider can work with binaries, all others
	// continue pointing to the directory instead of file
	if a.isFileInput {
		otherProvsMountPath = path.Dir(otherProvsMountPath)
	}

	javaConfig := provider.Config{
		Name:       "java",
		BinaryPath: "/jdtls/bin/jdtls",
		InitConfig: []provider.InitConfig{
			{
				Location:     SourceMountPath,
				AnalysisMode: provider.AnalysisMode(a.mode),
				ProviderSpecificConfig: map[string]interface{}{
					"bundles":                       "/jdtls/java-analyzer-bundle/java-analyzer-bundle.core/target/java-analyzer-bundle.core-1.0.0-SNAPSHOT.jar",
					"depOpenSourceLabelsFile":       "/usr/local/etc/maven.default.index",
					provider.LspServerPathConfigKey: "/jdtls/bin/jdtls",
				},
			},
		},
	}
	if a.mavenSettingsFile != "" {
		err := copyFileContents(a.mavenSettingsFile, filepath.Join(tempDir, "settings.xml"))
		if err != nil {
			a.log.V(1).Error(err, "failed copying maven settings file", "path", a.mavenSettingsFile)
			return nil, err
		}
		javaConfig.InitConfig[0].ProviderSpecificConfig["mavenSettingsFile"] = fmt.Sprintf("%s/%s", ConfigMountPath, "settings.xml")
	}

	provConfig := []provider.Config{
		{
			Name:       "go",
			BinaryPath: "/usr/bin/generic-external-provider",
			InitConfig: []provider.InitConfig{
				{
					Location:     otherProvsMountPath,
					AnalysisMode: provider.AnalysisMode(a.mode),
					ProviderSpecificConfig: map[string]interface{}{
						"name":                          "go",
						"dependencyProviderPath":        "/usr/bin/golang-dependency-provider",
						provider.LspServerPathConfigKey: "/root/go/bin/gopls",
					},
				},
			},
		},
		javaConfig,
		{
			Name: "builtin",
			InitConfig: []provider.InitConfig{
				{
					Location:     otherProvsMountPath,
					AnalysisMode: provider.AnalysisMode(a.mode),
				},
			},
		},
	}
	jsonData, err := json.MarshalIndent(&provConfig, "", "	")
	if err != nil {
		a.log.V(1).Error(err, "failed to marshal provider config")
		return nil, err
	}
	err = ioutil.WriteFile(filepath.Join(tempDir, "settings.json"), jsonData, os.ModePerm)
	if err != nil {
		a.log.V(1).Error(err, "failed to write provider config", "dir", tempDir, "file", "settings.json")
		return nil, err
	}
	return map[string]string{
		tempDir: ConfigMountPath,
	}, nil
}

func (a *analyzeCommand) getRulesVolumes() (map[string]string, error) {
	if a.rules == nil || len(a.rules) == 0 {
		return nil, nil
	}
	rulesVolumes := make(map[string]string)
	rulesetNeeded := false
	tempDir, err := os.MkdirTemp("", "analyze-rules-")
	if err != nil {
		a.log.V(1).Error(err, "failed to create temp dir", "path", tempDir)
		return nil, err
	}
	a.log.V(1).Info("created directory for rules", "dir", tempDir)
	a.tempDirs = append(a.tempDirs, tempDir)
	for i, r := range a.rules {
		stat, err := os.Stat(r)
		if err != nil {
			a.log.V(1).Error(err, "failed to stat rules", "path", r)
			return nil, err
		}
		// move rules files passed into dir to mount
		if !stat.IsDir() {
			// XML rules are handled outside of this func
			if isXMLFile(r) {
				continue
			}
			rulesetNeeded = true
			destFile := filepath.Join(tempDir, fmt.Sprintf("rules%d.yaml", i))
			err := copyFileContents(r, destFile)
			if err != nil {
				a.log.V(1).Error(err, "failed to move rules file", "src", r, "dest", destFile)
				return nil, err
			}

		} else {
			rulesVolumes[r] = path.Join(RulesMountPath, filepath.Base(r))
		}
	}
	if rulesetNeeded {
		tempRulesetPath := filepath.Join(tempDir, "ruleset.yaml")
		err = createTempRuleSet(tempRulesetPath)
		if err != nil {
			a.log.V(1).Error(err, "failed to create temp ruleset", "path", tempRulesetPath)
			return nil, err
		}
		rulesVolumes[tempDir] = path.Join(RulesMountPath, filepath.Base(tempDir))
	}
	return rulesVolumes, nil
}

func copyFileContents(src string, dst string) (err error) {
	source, err := os.Open(src)
	if err != nil {
		return nil
	}
	defer source.Close()
	destination, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	if err != nil {
		return err
	}
	return nil
}

func createTempRuleSet(path string) error {
	tempRuleSet := engine.RuleSet{
		Name:        "ruleset",
		Description: "temp ruleset",
	}
	yamlData, err := yaml.Marshal(&tempRuleSet)
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(path, yamlData, os.ModePerm)
	if err != nil {
		return err
	}
	return nil
}

func (a *analyzeCommand) RunAnalysis(ctx context.Context, xmlOutputDir string) error {
	volumes := map[string]string{
		// application source code
		a.input: SourceMountPath,
		// output directory
		a.output: OutputPath,
	}

	if xmlOutputDir != "" {
		convertPath := path.Join(RulesetPath, "convert")
		volumes[xmlOutputDir] = convertPath
		// for cleanup purposes
		a.tempDirs = append(a.tempDirs, xmlOutputDir)
	}

	configVols, err := a.getConfigVolumes()
	if err != nil {
		a.log.V(1).Error(err, "failed to get config volumes for analysis")
		return err
	}
	maps.Copy(volumes, configVols)

	if len(a.rules) > 0 {
		ruleVols, err := a.getRulesVolumes()
		if err != nil {
			a.log.V(1).Error(err, "failed to get rule volumes for analysis")
			return err
		}
		maps.Copy(volumes, ruleVols)
	}

	args := []string{
		fmt.Sprintf("--provider-settings=%s", ProviderSettingsMountPath),
		fmt.Sprintf("--rules=%s/", RulesetPath),
		fmt.Sprintf("--output-file=%s", AnalysisOutputMountPath),
		fmt.Sprintf("--context-lines=%d", 100),
	}
	if !a.analyzeKnownLibraries {
		args = append(args,
			fmt.Sprintf("--dep-label-selector=(!%s=open-source)", provider.DepSourceLabel))
	}
	labelSelector := a.getLabelSelector()
	if labelSelector != "" {
		args = append(args, fmt.Sprintf("--label-selector=%s", labelSelector))
	}

	analysisLogFilePath := filepath.Join(a.output, "analysis.log")
	depsLogFilePath := filepath.Join(a.output, "dependency.log")
	// create log files
	analysisLog, err := os.Create(analysisLogFilePath)
	if err != nil {
		return fmt.Errorf("failed creating analysis log file at %s", analysisLogFilePath)
	}
	defer analysisLog.Close()
	dependencyLog, err := os.Create(depsLogFilePath)
	if err != nil {
		return fmt.Errorf("failed creating dependency analysis log file %s", depsLogFilePath)
	}
	defer dependencyLog.Close()

	a.log.Info("running source code analysis", "log", analysisLogFilePath,
		"input", a.input, "output", a.output, "args", strings.Join(args, " "), "volumes", volumes)
	// TODO (pgaikwad): run analysis & deps in parallel
	err = NewContainer(a.log).Run(
		ctx,
		WithVolumes(volumes),
		WithStdout(os.Stdout, analysisLog),
		WithStderr(os.Stdout, analysisLog),
		WithEntrypointArgs(args...),
		WithEntrypointBin("/usr/bin/konveyor-analyzer"),
	)
	if err != nil {
		return err
	}

	a.log.Info("running dependency analysis",
		"log", depsLogFilePath, "input", a.input, "output", a.output, "args", strings.Join(args, " "))
	err = NewContainer(a.log).Run(
		ctx,
		WithStdout(os.Stdout, dependencyLog),
		WithStderr(os.Stderr, dependencyLog),
		WithVolumes(volumes),
		WithEntrypointBin("/usr/bin/konveyor-analyzer-dep"),
		WithEntrypointArgs(
			fmt.Sprintf("--output-file=%s", DepsOutputMountPath),
			fmt.Sprintf("--provider-settings=%s", ProviderSettingsMountPath),
		),
	)
	if err != nil {
		return err
	}

	return nil
}

func (a *analyzeCommand) CreateJSONOutput() error {
	if !a.jsonOutput {
		return nil
	}
	outputPath := filepath.Join(a.output, "output.yaml")
	depPath := filepath.Join(a.output, "dependencies.yaml")

	data, err := ioutil.ReadFile(outputPath)
	if err != nil {
		return err
	}
	ruleOutput := &[]outputv1.RuleSet{}
	err = yaml.Unmarshal(data, ruleOutput)
	if err != nil {
		a.log.V(1).Error(err, "failed to unmarshal output yaml")
		return err
	}

	jsonData, err := json.MarshalIndent(ruleOutput, "", "	")
	if err != nil {
		a.log.V(1).Error(err, "failed to marshal output file to json")
		return err
	}
	err = ioutil.WriteFile(filepath.Join(a.output, "output.json"), jsonData, os.ModePerm)
	if err != nil {
		a.log.V(1).Error(err, "failed to write json output", "dir", a.output, "file", "output.json")
		return err
	}

	depData, err := ioutil.ReadFile(depPath)
	if err != nil {
		return err
	}
	depOutput := &[]outputv1.DepsFlatItem{}
	err = yaml.Unmarshal(depData, depOutput)
	if err != nil {
		a.log.V(1).Error(err, "failed to unmarshal dependencies yaml")
		return err
	}

	jsonDataDep, err := json.MarshalIndent(depOutput, "", "	")
	if err != nil {
		a.log.V(1).Error(err, "failed to marshal dependencies file to json")
		return err
	}
	err = ioutil.WriteFile(filepath.Join(a.output, "dependencies.json"), jsonDataDep, os.ModePerm)
	if err != nil {
		a.log.V(1).Error(err, "failed to write json dependencies output", "dir", a.output, "file", "dependencies.json")
		return err
	}

	return nil
}

func (a *analyzeCommand) GenerateStaticReport(ctx context.Context) error {
	if a.skipStaticReport {
		return nil
	}
	volumes := map[string]string{
		a.input:  SourceMountPath,
		a.output: OutputPath,
	}
	args := []string{}
	staticReportArgs := []string{"/usr/local/bin/js-bundle-generator",
		fmt.Sprintf("--analysis-output-list=%s", AnalysisOutputMountPath),
		fmt.Sprintf("--deps-output-list=%s", DepsOutputMountPath),
		fmt.Sprintf("--output-path=%s", path.Join("/usr/local/static-report/output.js")),
		fmt.Sprintf("--application-name-list=%s", filepath.Base(a.input)),
	}
	cpArgs := []string{"&& cp -r",
		"/usr/local/static-report", OutputPath}

	args = append(args, staticReportArgs...)
	args = append(args, cpArgs...)

	joinedArgs := strings.Join(args, " ")
	staticReportCmd := []string{joinedArgs}

	container := NewContainer(a.log)
	a.log.Info("generating static report",
		"output", a.output, "args", strings.Join(staticReportCmd, " "))
	err := container.Run(
		ctx,
		WithEntrypointBin("/bin/sh"),
		WithEntrypointArgs(staticReportCmd...),
		WithVolumes(volumes),
		WithcFlag(true),
	)
	if err != nil {
		return err
	}
	uri := uri.File(filepath.Join(a.output, "static-report", "index.html"))
	cleanedURI := filepath.Clean(string(uri))
	a.log.Info("Static report created. Access it at this URL:", "URL", cleanedURI)

	return nil
}

func (a *analyzeCommand) Clean(ctx context.Context) error {
	for _, path := range a.tempDirs {
		err := os.RemoveAll(path)
		if err != nil {
			a.log.V(1).Error(err, "failed to delete temporary dir", "dir", path)
			continue
		}
	}
	return nil
}

func (a *analyzeCommand) getLabelSelector() string {
	if (a.sources == nil || len(a.sources) == 0) &&
		(a.targets == nil || len(a.targets) == 0) {
		return ""
	}
	// default labels are applied everytime either a source or target is specified
	defaultLabels := []string{"discovery"}
	targets := []string{}
	for _, target := range a.targets {
		targets = append(targets,
			fmt.Sprintf("%s=%s", outputv1.TargetTechnologyLabel, target))
	}
	sources := []string{}
	for _, source := range a.sources {
		sources = append(sources,
			fmt.Sprintf("%s=%s", outputv1.SourceTechnologyLabel, source))
	}
	targetExpr := ""
	if len(targets) > 0 {
		targetExpr = fmt.Sprintf("(%s)", strings.Join(targets, " || "))
	}
	sourceExpr := ""
	if len(sources) > 0 {
		sourceExpr = fmt.Sprintf("(%s)", strings.Join(sources, " || "))
	}
	if targetExpr != "" {
		if sourceExpr != "" {
			// when both targets and sources are present, AND them
			return fmt.Sprintf("(%s && %s) || (%s)",
				targetExpr, sourceExpr, strings.Join(defaultLabels, " || "))
		} else {
			// when target is specified, but source is not
			// use a catch-all expression for source
			return fmt.Sprintf("(%s && %s) || (%s)",
				targetExpr, outputv1.SourceTechnologyLabel, strings.Join(defaultLabels, " || "))
		}
	}
	if sourceExpr != "" {
		// when only source is specified, OR them all
		return fmt.Sprintf("%s || (%s)",
			sourceExpr, strings.Join(defaultLabels, " || "))
	}
	return ""
}

func isXMLFile(rule string) bool {
	extension := path.Ext(rule)
	if extension == ".xml" {
		return true
	}
	return false
}

func (a *analyzeCommand) getXMLRulesVolumes(tempRuleDir string) (map[string]string, error) {
	rulesVolumes := make(map[string]string)
	mountTempDir := false
	for _, r := range a.rules {
		stat, err := os.Stat(r)
		if err != nil {
			a.log.V(1).Error(err, "failed to stat rules")
			return nil, err
		}
		// move xml rule files from user into dir to mount
		if !stat.IsDir() {
			if !isXMLFile(r) {
				continue
			}
			mountTempDir = true
			xmlFileName := filepath.Base(r)
			destFile := filepath.Join(tempRuleDir, xmlFileName)
			err := copyFileContents(r, destFile)
			if err != nil {
				a.log.V(1).Error(err, "failed to move rules file from source to destination", "src", r, "dest", destFile)
				return nil, err
			}
		} else {
			rulesVolumes[r] = path.Join(XMLRulePath, filepath.Base(r))
		}
	}
	if mountTempDir {
		rulesVolumes[tempRuleDir] = XMLRulePath
	}
	return rulesVolumes, nil
}

func (a *analyzeCommand) ConvertXML(ctx context.Context) (string, error) {
	if a.rules == nil || len(a.rules) == 0 {
		return "", nil
	}
	tempDir, err := os.MkdirTemp("", "transform-rules-")
	if err != nil {
		a.log.V(1).Error(err, "failed to create temp dir for rules")
		return "", err
	}
	a.log.V(1).Info("created directory for XML rules", "dir", tempDir)
	tempOutputDir, err := os.MkdirTemp("", "transform-output-")
	if err != nil {
		a.log.V(1).Error(err, "failed to create temp dir for rules")
		return "", err
	}
	a.log.V(1).Info("created directory for converted XML rules", "dir", tempDir)
	defer os.RemoveAll(tempDir)
	volumes := map[string]string{
		tempOutputDir: ShimOutputPath,
	}

	ruleVols, err := a.getXMLRulesVolumes(tempDir)
	if err != nil {
		a.log.V(1).Error(err, "failed to get XML rule volumes for analysis")
		return "", err
	}
	maps.Copy(volumes, ruleVols)

	a.log.Info("running windup shim", "output", a.output)
	args := []string{"convert",
		fmt.Sprintf("--outputdir=%v", ShimOutputPath),
		XMLRulePath,
	}
	err = NewContainer(a.log).Run(
		ctx,
		WithVolumes(volumes),
		WithEntrypointArgs(args...),
		WithEntrypointBin("/usr/local/bin/windup-shim"),
	)
	if err != nil {
		return "", err
	}

	return tempOutputDir, nil
}
