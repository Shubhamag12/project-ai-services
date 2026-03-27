package podman

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/validators"
	"github.com/project-ai-services/ai-services/internal/pkg/validators/podman/spyre"
)

func runServiceReport() error {
	// validate spyre attachment first before running servicereport
	spyreCheck := spyre.NewSpyreRule()
	err := spyreCheck.Verify()
	if err != nil {
		return err
	}

	// Create host directories for vfio
	cmd := `mkdir -p /etc/modules-load.d; mkdir -p /etc/udev/rules.d/`
	_, err = exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to create host volume mounts for servicereport tool %w", err)
	}

	// load vfio kernel modules
	cmd = `modprobe vfio_pci`
	_, err = exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to load vfio kernel modules for spyre %w", err)
	}
	logger.Infoln("VFIO kernel modules loaded on the host", logger.VerbosityLevelDebug)

	if err := helpers.RunServiceReportContainer("servicereport -r -p spyre", "configure"); err != nil {
		return err
	}

	if err := configureUsergroup(); err != nil {
		return err
	}

	if err := reloadUdevRules(); err != nil {
		return err
	}

	cards, err := helpers.ListSpyreCards()
	if err != nil || len(cards) == 0 {
		return fmt.Errorf("❌ failed to list spyre cards on LPAR %w", err)
	}
	num_spyre_cards := len(cards)

	// check if kernel modules for vfio are loaded
	if err := checkKernelModulesLoaded(num_spyre_cards); err != nil {
		return err
	}

	return nil
}

func configureUsergroup() error {
	cmd_str := `groupadd sentient; usermod -aG sentient $USER`
	cmd := exec.Command("bash", "-c", cmd_str)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create sentient group and add current user to the sentient group. Error: %w, output: %s", err, string(out))
	}

	return nil
}

func reloadUdevRules() error {
	cmd := `udevadm control --reload-rules`
	_, err := exec.Command("bash", "-c", cmd).Output()
	if err != nil {
		return fmt.Errorf("failed to reload udev rules. Error: %w", err)
	}

	return nil
}

func checkKernelModulesLoaded(num_spyre_cards int) error {
	vfio_cmd := `lspci -k -d 1014:06a7 | grep "Kernel driver in use: vfio-pci" | wc -l`
	out, err := exec.Command("bash", "-c", vfio_cmd).Output()
	if err != nil {
		return fmt.Errorf("❌ failed to check vfio cards with kernel modules loaded %w", err)
	}

	num_vf_cards, err := strconv.Atoi(strings.TrimSuffix(string(out), "\n"))
	if err != nil {
		return fmt.Errorf("❌ failed to convert number of virtual spyre cards count from string to integer %w", err)
	}

	if num_vf_cards != num_spyre_cards {
		logger.Infof("failed to detect vfio cards, reloading vfio kernel modules..")
		// reload vfio kernel modules
		cmd := `rmmod vfio_pci; modprobe vfio_pci`
		_, err = exec.Command("bash", "-c", cmd).Output()
		if err != nil {
			return fmt.Errorf("❌ failed to reload vfio kernel modules for spyre %w", err)
		}
		logger.Infoln("VFIO kernel modules reloaded on the host", logger.VerbosityLevelDebug)
	}

	return nil
}

func installPodman() error {
	cmd := exec.Command("dnf", "-y", "install", "podman")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to install podman: %v, output: %s", err, string(out))
	}

	return nil
}

func setupPodman() error {
	euid := os.Geteuid()
	sudoUser := os.Getenv("SUDO_USER")

	// if running as root and not via sudo, enable system-wide podman socket
	// else, enable user podman socket for the sudo user
	if euid == 0 && sudoUser == "" {
		if err := systemctl("enable", "podman.socket", "--now"); err != nil {
			return fmt.Errorf("failed to enable podman socket: %w", err)
		}
	} else {
		machineArg := fmt.Sprintf("--machine=%s@.host", sudoUser)
		if err := systemctl("enable", "podman.socket", "--now", machineArg, "--user"); err != nil {
			return fmt.Errorf("failed to enable podman socket: %w", err)
		}
	}

	logger.Infoln("Waiting for podman socket to be ready...", logger.VerbosityLevelDebug)
	time.Sleep(podmanSocketWaitDuration) // wait for socket to be ready

	if err := validators.PodmanHealthCheck(); err != nil {
		return fmt.Errorf("podman health check failed after configuration: %w", err)
	}

	logger.Infof("Podman configured successfully.")

	return nil
}

func systemctl(action, unit string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), contextTimeout)
	defer cancel()

	cmdArgs := []string{action}
	cmdArgs = append(cmdArgs, args...)
	cmdArgs = append(cmdArgs, unit)

	cmd := exec.CommandContext(ctx, "systemctl", cmdArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to %s %s: %v, output: %s", action, unit, err, string(out))
	}

	return nil
}

func setupRequiredDirs() error {
	dirs := utils.RequiredDirs()

	if err := createDirs(dirs); err != nil {
		return err
	}

	sudoUser := os.Getenv("SUDO_USER")
	if sudoUser == "" {
		logger.Infoln("Running as root, directories will be owned by root", logger.VerbosityLevelDebug)

		return nil
	}

	uid, gid, err := lookupUserIDs(sudoUser)
	if err != nil {
		return err
	}

	return setOwnership(dirs, uid, gid, sudoUser)
}

func createDirs(dirs []string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, dirPerm); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		logger.Infof("Created directory: %s", dir, logger.VerbosityLevelDebug)
	}

	return nil
}

func lookupUserIDs(sudoUser string) (int, int, error) {
	u, err := user.Lookup(sudoUser)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to lookup user %s: %w", sudoUser, err)
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse UID: %w", err)
	}

	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse GID: %w", err)
	}

	return uid, gid, nil
}

func setOwnership(dirs []string, uid, gid int, sudoUser string) error {
	for _, dir := range dirs {
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("failed to set ownership for %s: %w", dir, err)
		}
		logger.Infof("Set ownership of %s to %s (UID: %d, GID: %d)", dir, sudoUser, uid, gid, logger.VerbosityLevelDebug)
		if err := chown(dir, uid, gid); err != nil {
			return fmt.Errorf("failed to set ownership for %s: %v", dir, err)
		}
	}
	logger.Infof("Directory setup completed successfully for user: %s", sudoUser)

	return nil
}

func chown(path string, uid, gid int) error {
	return filepath.Walk(path, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		return os.Chown(name, uid, gid)
	})
}
