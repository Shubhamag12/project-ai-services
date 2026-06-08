package podman

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/application/types"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
)

// Logs displays logs from an application pod.
func (p *PodmanApplication) Logs(opts types.LogsOptions) error {
	logger.Warningln("Press Ctrl+C to exit the logs and return to the terminal.")

	logger.Infof("Fetching logs for pod: %s", opts.PodName)
	if err := p.runtime.PodLogs(opts.PodName); err != nil {
		return fmt.Errorf("failed to fetch pod: %s logs; err: %w", opts.PodName, err)
	}

	return nil
}
