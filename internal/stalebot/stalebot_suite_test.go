package stalebot_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStalebot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Stalebot Suite")
}
