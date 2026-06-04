package selinux

import (
	"fmt"
	"os"
	"strings"

	"github.com/project-ai-services/ai-services/internal/pkg/bootstrap/spyreconfig/utils"
)

// ApplyResult represents the result of applying a SELinux policy.
type ApplyResult struct {
	Success bool
	Message string
	Error   error
}

// ApplySELinuxPolicy is a generic helper to apply SELinux policies.
// It checks if SELinux is enabled and active, then builds and installs the policy.
// Returns an ApplyResult indicating success or failure.
func ApplySELinuxPolicy(policyName, policyContent string) ApplyResult {
	enabled, msg := isSELinuxEnabledAndActive()
	if !enabled {
		return ApplyResult{Success: false, Message: msg}
	}

	tmpDir, err := os.MkdirTemp("", "selinux_build")
	if err != nil {
		return ApplyResult{
			Success: false,
			Error:   fmt.Errorf("failed to create temp directory: %w", err),
		}
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temp directory %s: %v\n", tmpDir, err)
		}
	}()

	// Use reinstall=true to ensure policy is updated if it already exists
	if err := buildAndInstallSELinuxPolicy(tmpDir, policyName, policyContent, true); err != nil {
		return ApplyResult{Success: false, Error: err}
	}

	return ApplyResult{Success: true, Message: fmt.Sprintf("SELinux policy '%s' applied successfully", policyName)}
}

// buildAndInstallSELinuxPolicy builds and installs a SELinux policy module.
func buildAndInstallSELinuxPolicy(tmpDir, policyName, teContent string, reinstall bool) error {
	// Write the .te file
	tePath := fmt.Sprintf("%s/%s.te", tmpDir, policyName)
	if err := utils.WriteToFile(tePath, teContent); err != nil {
		return fmt.Errorf("failed to write .te file: %w", err)
	}

	// Compile .te -> .mod
	modPath := fmt.Sprintf("%s/%s.mod", tmpDir, policyName)
	exitCode, _, stderr, err := utils.ExecuteCommand("checkmodule", "-M", "-m", "-o", modPath, tePath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to compile policy module: %v, stderr: %s", err, stderr)
	}

	// Package .mod -> .pp
	ppPath := fmt.Sprintf("%s/%s.pp", tmpDir, policyName)
	exitCode, _, stderr, err = utils.ExecuteCommand("semodule_package", "-o", ppPath, "-m", modPath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to package policy module: %v, stderr: %s", err, stderr)
	}

	// Install or update the module
	if reinstall {
		// Remove old module first
		_, _, _, _ = utils.ExecuteCommand("semodule", "-r", policyName)
	}

	// Install the module
	exitCode, _, stderr, err = utils.ExecuteCommand("semodule", "-i", ppPath)
	if err != nil || exitCode != 0 {
		return fmt.Errorf("failed to install policy module: %v, stderr: %s", err, stderr)
	}

	return nil
}

// isSELinuxEnabledAndActive checks if SELinux is enabled and active.
func isSELinuxEnabledAndActive() (bool, string) {
	exitCode, stdout, _, err := utils.ExecuteCommand("getenforce")
	if err != nil || exitCode != 0 {
		return false, "SELinux not available or not enabled"
	}

	status := strings.TrimSpace(stdout)
	if status == "Disabled" {
		return false, "SELinux is disabled"
	}

	return true, ""
}

// Made with Bob
