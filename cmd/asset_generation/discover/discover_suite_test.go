package discover_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDiscover(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Discover CloudFoundry Suite")
}
