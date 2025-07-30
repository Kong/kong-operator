package e2e

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "E2E Suite")
}

var _ = Describe("E2E Tests", func() {
	Context("When testing e2e functionality", func() {
		It("Should run basic tests", func() {
			// Add your e2e tests here
			Expect(true).To(BeTrue())
		})
	})
})
