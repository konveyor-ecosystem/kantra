package helm_test

import (
	"bytes"
	"os"
	"path"
	"strings"

	"github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	"github.com/konveyor-ecosystem/kantra/cmd/asset_generation/generate/helm"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var _ = Describe("Helm command", func() {

	type cmdFlags struct {
		input    string
		chartDir string
		output   string
		nonK8s   bool
		set      []string
	}

	type testCase struct {
		args          []string
		expectedError string
		expectedOut   string
	}

	var (
		logger    logr.Logger
		outBuffer bytes.Buffer
		errBuffer bytes.Buffer
		c         *cobra.Command
	)

	const (
		testDiscoverPath = "../../../../test-data/asset_generation/helm/discover.yaml"
		chartDir         = "../../../../test-data/asset_generation/helm/"
	)

	var _ = BeforeEach(func() {
		logrusLog := logrus.New()
		logrusLog.SetOutput(&outBuffer)
		logrusLog.SetFormatter(&logrus.TextFormatter{})
		logger = logrusr.New(logrusLog)
		outBuffer = bytes.Buffer{}
		errBuffer = bytes.Buffer{}
		c = helm.NewGenerateHelmCommand(logger)
		c.SetOut(&outBuffer)
		c.SetErr(&errBuffer)
	})
	DescribeTable("validating the execution when not generating templates",
		func(tc testCase) {
			c.SetArgs(tc.args)
			err := c.Execute()

			if tc.expectedError != "" {
				Expect(err).To(HaveOccurred())
				Expect(err).Should(MatchError(tc.expectedError))

			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(outBuffer.String()).To(ContainSubstring(tc.expectedOut))
			}
		},

		Entry("shows the help message when the -h flag is used",
			testCase{args: []string{"-h"}, expectedError: "", expectedOut: "generate the helm template manifests"}),

		Entry("shows the error message when no flags are provided",
			testCase{args: []string{}, expectedError: `required flag(s) "chart-dir", "input" not set`}),

		Entry("shows the error message when only the input flag is provided",
			testCase{args: []string{"--input", "discover.yaml"}, expectedError: `required flag(s) "chart-dir" not set`}),

		Entry("shows the error message when only the chart-dir flag is provided",
			testCase{args: []string{"--chart-dir", chartDir}, expectedError: `required flag(s) "input" not set`}),

		Entry("shows the error message when only the non-required flags are provided",
			testCase{args: []string{"--non-k8s-only", "--output-dir", "./", "--set", "foo=bar"}, expectedError: `required flag(s) "chart-dir", "input" not set`}),

		Entry("shows an error message when the input flag points to an invalid file",
			testCase{args: []string{"--input", "nonexistent.file", "--chart-dir", path.Join(chartDir, "k8s_only")},
				expectedError: `unable to load discover manifest: open nonexistent.file: no such file or directory`}),

		Entry("shows an error message when the chart-dir flag points to an invalid directory",
			testCase{args: []string{"--input", testDiscoverPath, "--chart-dir", "nonexistent.dir"},
				expectedError: `unable to load chart: stat nonexistent.dir: no such file or directory`}),

		Entry("shows an error message when the set flag contains an invalid k/v pair",
			testCase{args: []string{"--input", testDiscoverPath, "--chart-dir", path.Join(chartDir, "k8s_only"), "--set", "a,1"},
				expectedError: `failed parsing --set data:key "a" has no value (cannot end with ,)`}),
	)

	var _ = When("validating the execution when generating templates", func() {

		Context("when using the output flag", func() {
			var (
				manifests = map[string]string{
					"configmap.yaml": `apiVersion: v1
data:
  chartName: hello world!
kind: ConfigMap
metadata:
  name: sample

`, "Dockerfile": `FROM python:3

RUN echo hello world!
`}
			)
			It("generates the manifest files in the specified directory", func() {

				tmpDir, err := os.MkdirTemp("", "generate")
				Expect(err).NotTo(HaveOccurred())
				defer os.RemoveAll(tmpDir)
				c.SetArgs([]string{
					"--input", testDiscoverPath,
					"--chart-dir", path.Join(chartDir, "mixed_templates"),
					"--output-dir", tmpDir})
				err = c.Execute()
				Expect(err).NotTo(HaveOccurred())
				Expect(outBuffer.String()).To(BeEmpty())
				Expect(errBuffer.String()).To(BeEmpty())
				f, err := os.ReadDir(tmpDir)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(f)).To(Equal(len(manifests)))
				for k, v := range manifests {
					m, err := os.ReadFile(path.Join(tmpDir, k))
					Expect(err).NotTo(HaveOccurred())
					Expect(string(m)).To(Equal(v))
				}
			})
		})

		DescribeTable("when sending the results to stdout", func(flags cmdFlags, expected string) {
			args := []string{}
			if flags.input != "" {
				args = append(args, []string{"--input", flags.input}...)
			}
			if flags.chartDir != "" {
				args = append(args, []string{"--chart-dir", flags.chartDir}...)
			}
			if len(flags.set) > 0 {
				for _, s := range flags.set {
					args = append(args, append([]string{"--set"}, s)...)
				}
			}
			if flags.nonK8s {
				args = append(args, "--non-k8s-only")
			}
			c.SetArgs(args)
			err := c.Execute()
			Expect(err).NotTo(HaveOccurred())
			results := strings.Split(outBuffer.String(), "---\n")
			se := strings.Split(expected, "---\n")
			Expect(len(results)).To(Equal(len(se)))
			for _, v := range results {
				Expect(v).To(BeElementOf(se))
			}
		},
			Entry("generates the manifests for a K8s chart using the discover manifest as input",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "k8s_only"),
				}, `---
# Source: k8s_only/templates/configmap.yaml
apiVersion: v1
data:
  chartName: hello world!
kind: ConfigMap
metadata:
  name: sample
`),
			Entry("generates the manifests for a K8s chart while overriding the variable in the discover.yaml",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "k8s_only"),
					set:      []string{"foo.bar=bar.foo"},
				}, `---
# Source: k8s_only/templates/configmap.yaml
apiVersion: v1
data:
  chartName: bar.foo
kind: ConfigMap
metadata:
  name: sample
`),
			Entry("generates the manifests for a K8s chart while adding a new variable that is interpreted in the template",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "k8s_only"),
					set:      []string{"extra.value=Lorem Ipsum"},
				}, `---
# Source: k8s_only/templates/configmap.yaml
apiVersion: v1
data:
  chartName: hello world!
  extraValue: Lorem Ipsum
kind: ConfigMap
metadata:
  name: sample
`),
			Entry("generates no manifest in a K8s chart when specifying the flag to generate only the non-K8s templates",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "k8s_only"),
					nonK8s:   true,
				}, "",
			),
			Entry("generates both non-K8s and K8s manifests in a chart that contains both type of templates with the discover manifest as input",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "mixed_templates"),
				}, `---
# Source: mixed_templates/templates/configmap.yaml
apiVersion: v1
data:
  chartName: hello world!
kind: ConfigMap
metadata:
  name: sample
---
# Source: mixed_templates/files/konveyor/Dockerfile
FROM python:3

RUN echo hello world!`),
			Entry("with a chart with mixed templates and overriding the variable in the values.yaml",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "mixed_templates"),
					set:      []string{"foo.bar=bar.foo"},
				}, `---
# Source: mixed_templates/templates/configmap.yaml
apiVersion: v1
data:
  chartName: bar.foo
kind: ConfigMap
metadata:
  name: sample
---
# Source: mixed_templates/files/konveyor/Dockerfile
FROM python:3

RUN echo bar.foo`),
			Entry("with a chart with mixed templates and adding a new variable that is captured in the template",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "mixed_templates"),
					set:      []string{"extra.value=Lorem Ipsum"},
				}, `---
# Source: mixed_templates/files/konveyor/Dockerfile
FROM python:3

RUN echo hello world!
RUN echo Lorem Ipsum
---
# Source: mixed_templates/templates/configmap.yaml
apiVersion: v1
data:
  chartName: hello world!
  extraValue: Lorem Ipsum
kind: ConfigMap
metadata:
  name: sample
`),
			Entry("with a chart with mixed templates with multiple variables as input",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "mixed_templates"),
					set:      []string{"extra.value=Lorem Ipsum", "foo.bar=bar foo"},
				}, `---
# Source: mixed_templates/files/konveyor/Dockerfile
FROM python:3

RUN echo bar foo
RUN echo Lorem Ipsum
---
# Source: mixed_templates/templates/configmap.yaml
apiVersion: v1
data:
  chartName: bar foo
  extraValue: Lorem Ipsum
kind: ConfigMap
metadata:
  name: sample
`),
			Entry("with a chart with mixed templates with multiple variables as input in a single set flag",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "mixed_templates"),
					set:      []string{"extra.value=Lorem Ipsum,foo.bar=bar foo"},
				}, `---
# Source: mixed_templates/files/konveyor/Dockerfile
FROM python:3

RUN echo bar foo
RUN echo Lorem Ipsum
---
# Source: mixed_templates/templates/configmap.yaml
apiVersion: v1
data:
  chartName: bar foo
  extraValue: Lorem Ipsum
kind: ConfigMap
metadata:
  name: sample
`),
			Entry("only generates the non-K8s manifests in a chart that contains both type of templates",
				cmdFlags{
					input:    testDiscoverPath,
					chartDir: path.Join(chartDir, "mixed_templates"),
					nonK8s:   true,
				}, `---
# Source: mixed_templates/files/konveyor/Dockerfile
FROM python:3

RUN echo hello world!`,
			),
		)
	})
})
