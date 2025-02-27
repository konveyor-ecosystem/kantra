package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	"github.com/konveyor-ecosystem/kantra/pkg/container"
	"github.com/konveyor/analyzer-lsp/output/v1/konveyor"
	"github.com/konveyor/analyzer-lsp/provider"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

// Runner given a list of TestsFile and a TestOptions
// runs the tests, computes and returns results
type Runner interface {
	Run([]TestsFile, TestOptions) ([]Result, bool, error)
}

type TestOptions struct {
	TempDir            string
	LoudOutput         bool
	BaseProviderConfig []provider.Config
	RunLocal           bool
	ContainerImage     string
	ContainerToolBin   string
	ProgressPrinter    ResultPrinter
	Log                logr.Logger
}

// TODO (pgaikwad): we need to move the default config to a common place
// to be shared between kantra analyze command and this
var defaultProviderConfig = []provider.Config{
	{
		Name:       "java",
		BinaryPath: "/usr/local/bin/java-external-provider",
		InitConfig: []provider.InitConfig{
			{
				AnalysisMode: provider.FullAnalysisMode,
				ProviderSpecificConfig: map[string]interface{}{
					"lspServerName":                 "java",
					"bundles":                       "/jdtls/java-analyzer-bundle/java-analyzer-bundle.core/target/java-analyzer-bundle.core-1.0.0-SNAPSHOT.jar",
					"depOpenSourceLabelsFile":       "/usr/local/etc/maven.default.index",
					provider.LspServerPathConfigKey: "/jdtls/bin/jdtls",
				},
			},
		},
	},
	{
		Name:       "builtin",
		InitConfig: []provider.InitConfig{{Location: ""}},
	},
	{
		Name:       "go",
		BinaryPath: "/usr/local/bin/generic-external-provider",
		InitConfig: []provider.InitConfig{
			{
				AnalysisMode: provider.FullAnalysisMode,
				ProviderSpecificConfig: map[string]interface{}{
					"lspServerName":                 "generic",
					provider.LspServerPathConfigKey: "/root/go/bin/gopls",
					"lspServerArgs":                 []string{},
					"dependencyProviderPath":        "/usr/local/bin/golang-dependency-provider",
				},
			},
		},
	},
	{
		Name:       "python",
		BinaryPath: "/usr/local/bin/generic-external-provider",
		InitConfig: []provider.InitConfig{
			{
				AnalysisMode: provider.FullAnalysisMode,
				ProviderSpecificConfig: map[string]interface{}{
					"lspServerName":                 "pylsp",
					provider.LspServerPathConfigKey: "/usr/local/bin/pylsp",
					"lspServerArgs":                 []string{},
					"workspaceFolders":              []string{},
					"dependencyFolders":             []string{},
				},
			},
		},
	},
	{
		Name:       "nodejs",
		BinaryPath: "/usr/local/bin/generic-external-provider",
		InitConfig: []provider.InitConfig{
			{
				AnalysisMode: provider.FullAnalysisMode,
				ProviderSpecificConfig: map[string]interface{}{
					"lspServerName":                 "nodejs",
					provider.LspServerPathConfigKey: "/usr/local/bin/typescript-language-server",
					"lspServerArgs":                 []string{"--stdio"},
					"workspaceFolders":              []string{},
					"dependencyFolders":             []string{},
				},
			},
		},
	},
	{
		Name:       "yaml",
		BinaryPath: "/usr/local/bin/yq-external-provider",
		InitConfig: []provider.InitConfig{
			{
				AnalysisMode: provider.FullAnalysisMode,
				ProviderSpecificConfig: map[string]interface{}{
					"name":                          "yq",
					provider.LspServerPathConfigKey: "/usr/bin/yq",
				},
			},
		},
	},
}

func NewRunner() Runner {
	return defaultRunner{}
}

// defaultRunner runs tests one file at a time
// groups tests within a file by analysisParams
type defaultRunner struct{}

func (r defaultRunner) Run(testFiles []TestsFile, opts TestOptions) ([]Result, bool, error) {
	if opts.Log.GetSink() == nil {
		opts.Log = logr.Discard()
	}

	allResults := []Result{}
	anyFailed := false
	anyErrored := false
	for idx := range testFiles {
		testsFile := testFiles[idx]
		// users can override the base provider settings file
		baseProviderConfig := defaultProviderConfig
		if opts.BaseProviderConfig != nil {
			baseProviderConfig = opts.BaseProviderConfig
		}
		// within a tests file, we group tests by analysis params
		testGroups := groupTestsByAnalysisParams(testsFile.Tests)
		results := []Result{}
		for _, tests := range testGroups {
			tempDir, err := os.MkdirTemp(opts.TempDir, "rules-test-")
			if err != nil {
				results = append(results, Result{
					TestsFilePath: testsFile.Path,
					Error:         fmt.Errorf("failed creating temp dir - %w", err)})
				continue
			}
			opts.Log.Info("created temporary directory", "dir", tempDir, "tests", testsFile.Path)
			// print analysis logs to a file
			logFile, err := os.OpenFile(filepath.Join(tempDir, "analysis.log"),
				os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
			if err != nil {
				results = append(results, Result{
					TestsFilePath: testsFile.Path,
					Error:         fmt.Errorf("failed creating a log file - %w", err)})
				logFile.Close()
				continue
			}
			baseLogger := logrus.New()
			baseLogger.SetOutput(logFile)
			baseLogger.SetLevel(logrus.InfoLevel)
			logger := logrusr.New(baseLogger)
			// write rules
			err = ensureRules(testsFile.RulesPath, tempDir, tests)
			if err != nil {
				results = append(results, Result{
					TestsFilePath: testsFile.Path,
					Error:         fmt.Errorf("failed writing rules - %w", err)})
				logFile.Close()
				continue
			}
			// we already know in this group, all tcs have same params, use any
			analysisParams := tests[0].TestCases[0].AnalysisParams
			// write provider settings file
			volumes, err := ensureProviderSettings(
				tempDir, opts.RunLocal, testsFile, baseProviderConfig, analysisParams)
			if err != nil {
				results = append(results, Result{
					TestsFilePath: testsFile.Path,
					Error:         fmt.Errorf("failed writing provider settings - %w", err)})
				logFile.Close()
				continue
			}
			volumes[tempDir] = "/shared/"
			reproducerCmd := ""
			switch {
			case opts.RunLocal:
				if reproducerCmd, err = runLocal(logFile, tempDir, analysisParams); err != nil {
					results = append(results, Result{
						TestsFilePath: testsFile.Path,
						Error:         err})
					logFile.Close()
					continue
				}
			default:
				if reproducerCmd, err = runInContainer(
					logger, opts.ContainerImage, opts.ContainerToolBin, logFile, volumes, analysisParams); err != nil {
					results = append(results, Result{
						TestsFilePath: testsFile.Path,
						Error:         err})
					logFile.Close()
					continue
				}
			}
			// write reproducer command to a file
			os.WriteFile(filepath.Join(tempDir, "reproducer.sh"), []byte(reproducerCmd), 0755)
			// process output
			outputRulesets := []konveyor.RuleSet{}
			content, err := os.ReadFile(filepath.Join(tempDir, "output.yaml"))
			if err != nil {
				results = append(results, Result{
					TestsFilePath: testsFile.Path,
					Error:         fmt.Errorf("failed reading output - %w", err)})
				logFile.Close()
				continue
			}
			err = yaml.Unmarshal(content, &outputRulesets)
			if err != nil {
				results = append(results, Result{
					TestsFilePath: testsFile.Path,
					Error:         fmt.Errorf("failed unmarshaling output %s", filepath.Join(tempDir, "output.yaml"))})
				logFile.Close()
				continue
			}
			anyFailed := false
			groupResults := []Result{}
			for _, test := range tests {
				for _, tc := range test.TestCases {
					result := Result{
						TestsFilePath: testsFile.Path,
						RuleID:        test.RuleID,
						TestCaseName:  tc.Name,
					}
					if len(outputRulesets) > 0 {
						result.FailureReasons = tc.Verify(outputRulesets[0])
					} else {
						result.FailureReasons = []string{"empty output"}
					}
					if len(result.FailureReasons) == 0 {
						result.Passed = true
					} else {
						anyFailed = true
						result.DebugInfo = append(result.DebugInfo,
							fmt.Sprintf("find debug data in %s", tempDir))
					}
					groupResults = append(groupResults, result)
				}
			}
			results = append(results, groupResults...)
			if !anyFailed {
				os.RemoveAll(tempDir)
			}
			logFile.Close()
		}
		// print progress
		if opts.ProgressPrinter != nil {
			opts.ProgressPrinter(os.Stdout, results)
		}
		// result
		for _, r := range results {
			if r.Error != nil {
				anyErrored = true
			}
			if !r.Passed {
				anyFailed = true
			}
		}
		allResults = append(allResults, results...)
	}
	// sorting for stability of unit tests
	defer sort.Slice(allResults, func(i, j int) bool {
		return strings.Compare(allResults[i].RuleID, allResults[j].RuleID) > 0
	})

	if anyErrored {
		return allResults, false, fmt.Errorf("failed to execute one or more tests")
	}
	if anyFailed {
		return allResults, false, fmt.Errorf("one or more tests failed")
	}
	return allResults, true, nil
}

func runLocal(logFile io.Writer, dir string, analysisParams AnalysisParams) (string, error) {
	// run analysis in a container
	args := []string{
		"--provider-settings",
		filepath.Join(dir, "provider_settings.json"),
		"--output-file",
		filepath.Join(dir, "output.yaml"),
		"--rules",
		filepath.Join(dir, "rules.yaml"),
		"--verbose",
		"20",
	}
	if analysisParams.DepLabelSelector != "" {
		args = append(args, []string{
			"--dep-label-selector",
			analysisParams.DepLabelSelector,
		}...)
	}
	cmd := exec.Command("konveyor-analyzer", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	return fmt.Sprintf("konveyor-analyzer %s", strings.Join(args, " ")), cmd.Run()
}

func runInContainer(consoleLogger logr.Logger, image string, containerBin string, logFile io.Writer, volumes map[string]string, analysisParams AnalysisParams) (string, error) {
	if image == "" {
		image = "quay.io/konveyor/analyzer-lsp:latest"
	}
	// run analysis in a container
	args := []string{
		"--provider-settings",
		"/shared/provider_settings.json",
		"--output-file",
		"/shared/output.yaml",
		"--rules",
		"/shared/rules.yaml",
		"--verbose",
		"20",
	}
	if analysisParams.DepLabelSelector != "" {
		args = append(args, []string{
			"--dep-label-selector",
			analysisParams.DepLabelSelector,
		}...)
	}
	reproducerCmd := ""
	err := container.NewContainer().Run(
		context.TODO(),
		container.WithImage(image),
		container.WithLog(consoleLogger),
		container.WithEntrypointBin("konveyor-analyzer"),
		container.WithContainerToolBin(containerBin),
		container.WithEntrypointArgs(args...),
		container.WithVolumes(volumes),
		container.WithWorkDir("/shared/"),
		container.WithStderr(logFile),
		container.WithStdout(logFile),
		container.WithReproduceCmd(&reproducerCmd),
	)
	if err != nil {
		return reproducerCmd, fmt.Errorf("failed running analysis - %w", err)
	}
	return reproducerCmd, nil
}

func ensureRules(rulesPath string, tempDirPath string, group []Test) error {
	allRules := []map[string]interface{}{}
	neededRules := map[string]interface{}{}
	for _, test := range group {
		neededRules[test.RuleID] = nil
	}
	content, err := os.ReadFile(rulesPath)
	if err != nil {
		return fmt.Errorf("failed to read rules file %s (%w)", rulesPath, err)
	}
	err = yaml.Unmarshal(content, &allRules)
	if err != nil {
		return fmt.Errorf("error unmarshaling rules at path %s (%w)", rulesPath, err)
	}
	foundRules := []map[string]interface{}{}
	for neededRule := range neededRules {
		found := false
		for _, foundRule := range allRules {
			if foundRule["ruleID"] == neededRule {
				found = true
				foundRules = append(foundRules, foundRule)
				break
			}
		}
		if !found {
			return fmt.Errorf("rule %s not found in file %s", neededRule, rulesPath)
		}
	}

	content, err = yaml.Marshal(foundRules)
	if err != nil {
		return fmt.Errorf("failed marshaling rules - %w", err)
	}
	err = os.WriteFile(filepath.Join(tempDirPath, "rules.yaml"), content, 0644)
	if err != nil {
		return fmt.Errorf("failed writing rules file - %w", err)
	}
	return nil
}

func ensureProviderSettings(tempDirPath string, runLocal bool, testsFile TestsFile, baseProviders []provider.Config, params AnalysisParams) (map[string]string, error) {
	final := []provider.Config{}
	volumes := map[string]string{}
	// we need to get data paths defined in the tests file to populate location fields in provider settings
	// depending on whether we run locally, or in a container, we will either use local paths or mounted paths
	switch {
	case runLocal:
		for _, override := range testsFile.Providers {
			// when running locally, we use the paths as-is
			dataPath := filepath.Join(filepath.Dir(testsFile.Path), filepath.Clean(override.DataPath))
			final = append(final,
				getMergedProviderConfig(override.Name, baseProviders, params, dataPath, tempDirPath)...)
		}
	default:
		// in containers, we need to make sure we only mount unique path trees
		// to avoid mounting a directory and its subdirectory to two different paths
		uniqueTrees := map[string]bool{}
		toDelete := []string{}
		for _, prov := range testsFile.Providers {
			found := false
			for tree := range uniqueTrees {
				if tree != prov.DataPath && (strings.Contains(tree, prov.DataPath) || strings.Contains(prov.DataPath, tree)) {
					found = true
					if len(tree) > len(prov.DataPath) {
						toDelete = append(toDelete, tree)
						uniqueTrees[prov.DataPath] = true
					} else {
						toDelete = append(toDelete, prov.DataPath)
						uniqueTrees[tree] = true
					}
				}
			}
			if !found {
				uniqueTrees[prov.DataPath] = true
			}
		}
		for _, key := range toDelete {
			delete(uniqueTrees, key)
		}
		for uniquePath := range uniqueTrees {
			volumes[filepath.Join(filepath.Dir(testsFile.Path), uniquePath)] = path.Join("/data", uniquePath)
		}
		for _, override := range testsFile.Providers {
			// when running in the container, we use the mounted path
			dataPath := filepath.Join("/data", filepath.Clean(override.DataPath))
			final = append(final,
				getMergedProviderConfig(override.Name, baseProviders, params, dataPath, "/shared")...)
		}
	}
	content, err := json.Marshal(final)
	if err != nil {
		return nil, fmt.Errorf("failed marshaling provider settings - %w", err)
	}
	err = os.WriteFile(filepath.Join(tempDirPath, "provider_settings.json"), content, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed writing provider settings file - %w", err)
	}
	return volumes, nil
}

// getMergedProviderConfig for a given provider in the tests file, find a base provider config and
// merge values as per precedance (values in tests file take precedance)
func getMergedProviderConfig(name string, baseConfig []provider.Config, params AnalysisParams, dataPath string, outputPath string) []provider.Config {
	merged := []provider.Config{}
	for idx := range baseConfig {
		base := &baseConfig[idx]
		base.ContextLines = 100
		if base.Name == name {
			initConf := &base.InitConfig[0]
			if params.Mode != "" {
				initConf.AnalysisMode = params.Mode
			}
			switch base.Name {
			// languages enabled via generic provide use workspaceFolders instead of location
			// we also enable detailed logging for different providers
			case "python":
				initConf.ProviderSpecificConfig["workspaceFolders"] = []string{dataPath}
				// log things in the output directory for debugging
				lspArgs, ok := initConf.ProviderSpecificConfig["lspServerArgs"].([]string)
				if ok {
					initConf.ProviderSpecificConfig["lspServerArgs"] = append(lspArgs,
						"--log-file", path.Join(outputPath, "python-server.log"), "-vv")
				}
			case "go":
				initConf.ProviderSpecificConfig["workspaceFolders"] = []string{dataPath}
				lspArgs, ok := initConf.ProviderSpecificConfig["lspServerArgs"].([]string)
				if ok {
					initConf.ProviderSpecificConfig["lspServerArgs"] = append(lspArgs,
						"--logfile", path.Join(outputPath, "go-server.log"), "-vv")
				}
			case "nodejs":
				initConf.ProviderSpecificConfig["workspaceFolders"] = []string{dataPath}
			default:
				initConf.Location = dataPath
			}
			merged = append(merged, *base)
		}
	}
	return merged
}

func groupTestsByAnalysisParams(tests []Test) [][]Test {
	grouped := map[string]map[string]*Test{}
	for _, t := range tests {
		testKey := t.RuleID
		for _, tc := range t.TestCases {
			paramsKey := fmt.Sprintf("%s-%s",
				tc.AnalysisParams.DepLabelSelector, tc.AnalysisParams.Mode)
			if _, ok := grouped[paramsKey]; !ok {
				grouped[paramsKey] = map[string]*Test{}
			}
			if _, ok := grouped[paramsKey][testKey]; !ok {
				grouped[paramsKey][testKey] = &Test{
					RuleID:    t.RuleID,
					TestCases: []TestCase{},
				}
			}
			grouped[paramsKey][testKey].TestCases = append(
				grouped[paramsKey][testKey].TestCases, tc)
		}
	}
	groupedList := [][]Test{}
	for _, tests := range grouped {
		currentGroup := []Test{}
		for _, v := range tests {
			currentGroup = append(currentGroup, *v)
		}
		groupedList = append(groupedList, currentGroup)
	}
	return groupedList
}
