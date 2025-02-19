package generate_test

import (
	"bufio"
	"bytes"

	"github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	"github.com/konveyor-ecosystem/kantra/cmd/asset_generation/generate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var _ = Describe("Generate Command", func() {

	var _ = Context("Flag Behavior Verification", func() {

		var (
			logger logr.Logger
			out    bytes.Buffer
			err    bytes.Buffer
			writer *bufio.Writer = bufio.NewWriter(&out)
			c      *cobra.Command
		)

		var _ = BeforeEach(func() {
			// Reset buffers before each test
			writer.Reset(&out)

			// Set up logger
			logrusLog := logrus.New()
			logrusLog.SetOutput(&out)
			logrusLog.SetFormatter(&logrus.TextFormatter{})
			logger = logrusr.New(logrusLog)

			// Create command instance
			c = generate.NewGenerateCommand(logger)
			c.SetOut(writer)
			c.SetErr(&err)
		})

		DescribeTable("Command Flag Behavior",
			func(args []string, expectedOutput string, expectError bool, expectedError string) {
				c.SetArgs(args)
				err := c.Execute()
				writer.Flush()
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(err).Should(MatchError(expectedError))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(out.String()).To(ContainSubstring(expectedOutput))

			},
			Entry("should print the help message when no flags are used containing entries for each template engine",
				[]string{}, "helm        generate the helm template manifests", false, ""),

			Entry("should return an error message for an invalid command",
				[]string{"invalid-command"}, "", true, "unknown command \"invalid-command\" for \"generate\""),

			Entry("should return an error message for an invalid flag",
				[]string{"--invalid-flag"}, "Usage:\n", true, "unknown flag: --invalid-flag"),
		)
	})
})
