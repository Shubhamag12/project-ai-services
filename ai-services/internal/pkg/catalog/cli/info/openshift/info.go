package openshift

import (
	"fmt"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/helpers"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/templates"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	oc "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
)

// DisplayCatalogInfo displays detailed information about the catalog service on OpenShift.
func DisplayCatalogInfo() error {
	// Initialize OpenShift client scoped to the catalog namespace
	runtime, err := oc.NewOpenshiftClientWithNamespace(constants.CatalogAppName)
	if err != nil {
		return fmt.Errorf("failed to initialize openshift client: %w", err)
	}

	// Check if any catalog pods exist in the namespace
	listFilters := map[string][]string{
		"label": {fmt.Sprintf("ai-services.io/application=%s", constants.CatalogAppName)},
	}

	pods, err := runtime.ListPods(listFilters)
	if err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods) == 0 {
		logger.Infof("Catalog service is not configured or running.\n")
		logger.Infof("Run 'ai-services catalog configure --runtime openshift' to set up the catalog service.\n")

		return nil
	}

	logger.Infoln("Catalog Service Name: " + constants.CatalogAppName)

	tp := templates.NewEmbedTemplateProvider(&assets.CatalogFS, "")

	if err := helpers.PrintInfo(tp, runtime, constants.CatalogAppName, constants.CatalogAppTemplate); err != nil {
		// not failing overall info command if we cannot display Info
		logger.Errorf("failed to display info: %v\n", err)
	}

	return nil
}
