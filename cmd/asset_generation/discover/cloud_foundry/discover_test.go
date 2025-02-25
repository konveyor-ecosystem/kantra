package cloud_foundry

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/spf13/cobra"
)

var _ = Describe("Discover Manifest", func() {
	var (
		out          bytes.Buffer
		writer       *bufio.Writer = bufio.NewWriter(&out)
		tempDir      string
		manifestPath string
		// useLive bool
	)

	BeforeEach(func() {
		tempDir, err := os.MkdirTemp("", "cloud_foundry_test")
		Expect(err).NotTo(HaveOccurred())
		manifestPath = filepath.Join(tempDir, "manifest.yaml")
		input = manifestPath
		output = ""
		// Reset buffers before each test
		writer.Reset(&out)
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	DescribeTable("Manifest validation",
		func(manifestContent string, expectedErrorMessage string) {
			err := helperCreateTestManifest(manifestPath, manifestContent, 0644)
			Expect(err).ToNot(HaveOccurred(), "Unable to create manifest.yaml")
			input = manifestPath
			output = ""
			err = discoverManifest(writer)
			writer.Flush()
			if expectedErrorMessage != "" {
				Expect(err).To(HaveOccurred(), "Expected an error due to invalid manifest content, got none")
				Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
			} else {
				Expect(err).ToNot(HaveOccurred(), "Expected no error for invalid manifest, but got one")
			}
		},
		Entry("with an empty manifest", "", "field validation for 'Name' failed on the 'required' tag"),
		Entry("with invalid YAML content", "invalid content", "cannot unmarshal !!str `invalid...` into cloud_foundry.AppManifest"),
		Entry("with a valid manifest", `name: test-app`, ""),
	)

	DescribeTable("Manifest creation",
		func(manifestContent string, perm os.FileMode, expectedErrorMessage string) {
			err := helperCreateTestManifest(manifestPath, manifestContent, perm)
			Expect(err).ToNot(HaveOccurred(), "Unable to create manifest.yaml")

			err = discoverManifest(writer)
			if expectedErrorMessage != "" {
				Expect(err).ToNot(HaveOccurred(), "Expected an error due to invalid manifest content, got none")
				Expect(err.Error()).To(ContainSubstring(expectedErrorMessage))
			} else {
				Expect(err).ToNot(HaveOccurred(), "Expected no error for valid manifest, but got one")
			}
		},
		Entry("with readonly permission", `name: test-app`, os.FileMode(0444), ""),
		Entry("withouth read permission", `name: test-app`, os.FileMode(0000), "manifest.yaml: permission denied"),
	)
})
var _ = Describe("Discover command", func() {

	var (
		log    logr.Logger
		out    bytes.Buffer
		err    bytes.Buffer
		writer *bufio.Writer = bufio.NewWriter(&out)

		tempDir         string
		cmd             *cobra.Command
		outputPath      string
		manifestContent []byte
		manifestPath    string
	)

	BeforeEach(func() {
		log = logr.Discard()

		// Reset buffers before each test
		writer.Reset(&out)

		// Create a temporary directory for test files
		tempDir, err := os.MkdirTemp("", "cloud_foundry_test")
		Expect(err).NotTo(HaveOccurred())

		manifestContent = []byte(`---
name: test-app
memory: 256M
instances: 1
`)

		manifestPath = filepath.Join(tempDir, "manifest.yaml")
		Expect(os.WriteFile(manifestPath, manifestContent, 0644)).To(Succeed())
		Expect(manifestPath).ToNot(BeEmpty())
		outputPath = filepath.Join(tempDir, "output.yaml")
	})

	AfterEach(func() {
		os.RemoveAll(tempDir)
	})

	type flagFile struct {
		setFlag  bool
		filePath string
	}
	type flagTest struct {
		Input                  flagFile
		Output                 flagFile
		ExpectSuccess          bool
		ExpectedOut            string
		ExpectErr              bool
		ExpectedErrMessage     string
		OutputFileVerification bool
	}

	DescribeTable("flag behavior",
		func(flags flagTest) {
			_, cmd = NewDiscoverCloudFoundryCommand(log)

			cmd.SetOut(&out)
			cmd.SetErr(&err)
			args := []string{}

			if flags.Input.setFlag {
				args = append(args, "--input")
				if len(flags.Input.filePath) != 0 {
					args = append(args, flags.Input.filePath)
				} else {
					// Default
					args = append(args, manifestPath)
				}
			}
			if flags.Output.setFlag {
				args = append(args, "--output")
				if len(flags.Output.filePath) != 0 {
					args = append(args, flags.Output.filePath)
					defer os.Remove(flags.Output.filePath)
				} else {
					// Default
					args = append(args, outputPath)
				}
			}

			cmd.SetArgs(args)

			e := cmd.Execute()
			writer.Flush()

			if flags.ExpectSuccess {
				Expect(e).To(Succeed())
			} else {
				Expect(e).To(HaveOccurred())
			}

			if flags.ExpectErr {
				Expect(err.String()).To(ContainSubstring(flags.ExpectedErrMessage)) // Check STDERR for errors
			}

			if flags.OutputFileVerification {
				outputContent, err := os.ReadFile(outputPath)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(outputContent)).To(ContainSubstring(flags.ExpectedOut))
				Expect(out.String()).To(BeEmpty()) // Standard output should be empty
			} else {
				Expect(out.String()).To(ContainSubstring(flags.ExpectedOut)) // Check STDOUT for normal output
			}
		},

		Entry("discovers manifest and prints output to standard output when flags are valid",
			flagTest{
				Input:                  flagFile{setFlag: true},
				Output:                 flagFile{setFlag: false},
				ExpectSuccess:          true,
				ExpectedOut:            "test-app",
				ExpectErr:              false,
				ExpectedErrMessage:     "",
				OutputFileVerification: false},
		),

		Entry("writes to output file when --output flag is given",
			flagTest{
				Input:                  flagFile{setFlag: true},
				Output:                 flagFile{setFlag: true},
				ExpectSuccess:          true,
				ExpectedOut:            "",
				ExpectErr:              false,
				ExpectedErrMessage:     "",
				OutputFileVerification: false},
		),
		Entry("returns an error when input file is missing",
			flagTest{
				Input:                  flagFile{setFlag: false},
				Output:                 flagFile{setFlag: false},
				ExpectSuccess:          false,
				ExpectedOut:            "",
				ExpectErr:              true,
				ExpectedErrMessage:     "required flag",
				OutputFileVerification: false},
		),

		Entry("returns an error when input file does not exist",

			flagTest{
				Input:                  flagFile{setFlag: true, filePath: "nonexistent.yaml"},
				Output:                 flagFile{setFlag: false},
				ExpectSuccess:          false,
				ExpectedOut:            "",
				ExpectErr:              true,
				ExpectedErrMessage:     "no such file or directory",
				OutputFileVerification: false},
		),
	)
})

func helperCreateTestManifest(manifestPAth string, content string, perm os.FileMode) error {
	err := os.WriteFile(manifestPAth, []byte(content), perm) //0644
	if err != nil {
		return err
	}
	return nil
}
