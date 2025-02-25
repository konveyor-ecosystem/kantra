package cloud_foundry_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCloudFoundry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CloudFoundry Suite")
}
