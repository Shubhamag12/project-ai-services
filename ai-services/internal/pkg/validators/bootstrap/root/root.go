package root

import (
	"fmt"
	"os"

	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

type RootRule struct{}

func NewRootRule() *RootRule {
	return &RootRule{}
}

func (r *RootRule) String() string {
	return "root"
}

func (r *RootRule) Verify() error {
	euid := os.Geteuid()

	logger.Infoln("Checking root privileges", 2)

	if euid != 0 {
		return fmt.Errorf("current user is not root (EUID: %d)", euid)
	}

	logger.Infoln("Current user is root")
	return nil
}

func (r *RootRule) Hint() string {
	return "Run this command with root privileges using 'sudo' or as the root user."
}
