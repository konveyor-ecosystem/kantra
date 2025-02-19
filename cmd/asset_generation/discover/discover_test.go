package discover

import (
	"bufio"
	"bytes"

	"github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var _ = Describe("Discover Command", func() {

	Context("Flag Behavior Verification", func() {
		var (
			logger logr.Logger
			out    bytes.Buffer
			err    bytes.Buffer
			writer *bufio.Writer = bufio.NewWriter(&out)
			c      *cobra.Command
		)
		BeforeEach(func() {
			// Reset buffers before each test
			writer.Reset(&out)

			// Set up logger
			logrusLog := logrus.StandardLogger()
			logrusLog.SetOutput(writer)
			logger = logrusr.New(logrusLog)

			// Create command instance
			c = NewDiscoverCommand(logger)
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

			Entry("should display help message when no flags are provided",
				[]string{}, "Discover application outputs a YAML representation of source platform resources", false, ""),

			Entry("should return an error message for an invalid command",
				[]string{"invalid-command"}, "", true, "unknown command \"invalid-command\" for \"discover\""),

			Entry("should return an error message for an invalid flag",
				[]string{"--invalid-flag"}, "Usage:\n", true, "unknown flag: --invalid-flag"),

			Entry("should list supported platforms when --list-platforms flag is used",
				[]string{"--list-platforms"}, "Supported platforms:", false, ""),
		)
	})
})
