package generate_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGenerate(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Generate Suite")
}
