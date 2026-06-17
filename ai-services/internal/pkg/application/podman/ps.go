package podman

import (
	"fmt"

	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
)

// List returns information about running applications.
func (p *PodmanApplication) List(opts appTypes.ListOptions) ([]appTypes.ApplicationInfo, error) {
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create application client: %w", err)
	}

	applicationList, err := cliUtils.FetchApplications(appClient, opts.ApplicationName)
	if err != nil {
		return nil, err
	}

	if len(applicationList) == 0 {
		logger.Warningln("No Application found")

		return nil, nil
	}

	// Create table writer
	printer := utils.NewTableWriter()
	defer printer.CloseTableWriter()

	// Set table headers based on output format
	setApplicationPSTableHeaders(printer, opts.OutputWide)

	// Process each application ID
	for _, app := range applicationList {
		// Get PS information for the application
		psResp, err := appClient.GetApplicationPS(app.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch application: %w", err)
		}

		// Process services pods
		for _, pod := range psResp.Services {
			rows := cliUtils.BuildPodRowFromAPI(psResp.Name, pod, opts.OutputWide)
			printer.AppendRow(rows...)
		}

		// Process components pods
		for _, pod := range psResp.Components {
			rows := cliUtils.BuildPodRowFromAPI(psResp.Name, pod, opts.OutputWide)
			printer.AppendRow(rows...)
		}
	}

	return nil, nil
}

// setApplicationPSTableHeaders sets the table headers based on output format.
func setApplicationPSTableHeaders(printer *utils.Printer, outputWide bool) {
	if outputWide {
		printer.SetHeaders("APPLICATION NAME", "POD ID", "POD NAME", "STATUS", "CREATED", "CONTAINERS")
	} else {
		printer.SetHeaders("APPLICATION NAME", "POD NAME", "STATUS")
	}
}

// Made with Bob
