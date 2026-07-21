package openshift

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

const defaultHelmTimeout = 10 * time.Minute

// Type aliases for deployment plan types.
type (
	DeploymentPlan = deploymenttypes.DeploymentPlan
	ComponentPlan  = deploymenttypes.ComponentPlan
	ServicePlan    = deploymenttypes.ServicePlan
)

// OpenShiftDeployer implements deployment execution for the OpenShift runtime using Helm.
type OpenShiftDeployer struct {
	catalogProvider *catalog.CatalogProvider
	appRepo         repository.ApplicationRepository
	serviceRepo     repository.ServiceRepository
	componentRepo   repository.ComponentRepository
}

// NewOpenShiftDeployer creates a new OpenShiftDeployer instance.
func NewOpenShiftDeployer(
	catalogProvider *catalog.CatalogProvider,
	appRepo repository.ApplicationRepository,
	serviceRepo repository.ServiceRepository,
	componentRepo repository.ComponentRepository,
) *OpenShiftDeployer {
	return &OpenShiftDeployer{
		catalogProvider: catalogProvider,
		appRepo:         appRepo,
		serviceRepo:     serviceRepo,
		componentRepo:   componentRepo,
	}
}

// ExecuteDeployment executes the deployment plan in three ordered phases:
//  0. Prerequisites: pre-requisite charts (ServingRuntimes etc.) installed once per namespace
//  1. Components: main component Helm charts (concurrent, each waits for Ready)
//  2. Services: service Helm charts (concurrent, each waits for Ready)
func (d *OpenShiftDeployer) ExecuteDeployment(
	ctx context.Context,
	plan *DeploymentPlan,
	_ apimodels.CreateApplicationRequest,
) error {
	ns := appNamespace(plan.ApplicationID)

	logger.InfofCtx(ctx, "Starting OpenShift deployment for '%s' in namespace '%s'\n",
		plan.ApplicationName, ns)

	// Update application status to Deploying
	if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusDeploying, deployingStatusMessage(plan)); err != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status to Deploying: %v\n", err)
	}

	// Phase 0: Deploy prerequisites (ServingRuntimes etc.), idempotent, once per namespace
	if err := d.deployPrerequisites(ctx, ns); err != nil {
		d.handleStepError(ctx, plan.ApplicationID, "Prerequisites deployment failed", err)

		return err
	}

	// Phase 1: Deploy components concurrently via Helm
	if err := d.deployComponentsConcurrently(ctx, ns, plan); err != nil {
		d.handleStepError(ctx, plan.ApplicationID, "Component deployment failed", err)

		return err
	}

	// Phase 2: Deploy services concurrently via Helm.
	if err := d.deployServicesConcurrently(ctx, ns, plan); err != nil {
		d.handleStepError(ctx, plan.ApplicationID, "Service deployment failed", err)

		return err
	}

	// Update application status to Running
	if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusRunning, "Deployment completed successfully"); err != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status to Running: %v\n", err)
	}

	logger.InfofCtx(ctx, "OpenShift deployment completed successfully for '%s'\n", plan.ApplicationName)

	return nil
}

// deployPrerequisites installs all Helm charts found under prerequisites/openshift/ into the
// application namespace. Each subdirectory is treated as an independent chart and installed
// with its directory name as the release name.
func (d *OpenShiftDeployer) deployPrerequisites(ctx context.Context, ns string) error {
	prereqRoot := "prerequisites/openshift"

	entries, err := assets.CatalogFS.ReadDir(prereqRoot)
	if err != nil {
		// No prerequisites directory — skip silently
		logger.InfofCtx(ctx, "No prerequisites found at %s, skipping\n", prereqRoot)

		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		chartPath := prereqRoot + "/" + entry.Name()
		release := entry.Name() // e.g. "serving-runtime-cpu"

		if err := helmInstallOrUpgrade(ctx, ns, release, chartPath, map[string]any{}); err != nil {
			return fmt.Errorf("failed to deploy prerequisite '%s': %w", release, err)
		}
	}

	return nil
}

// deployComponentsConcurrently installs all component Helm charts in parallel and waits for all
// to become Ready before returning.
func (d *OpenShiftDeployer) deployComponentsConcurrently(ctx context.Context, ns string, plan *DeploymentPlan) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Components))

	for hash, comp := range plan.Components {
		wg.Add(1)

		go func(h string, c *ComponentPlan) {
			defer wg.Done()

			logger.InfofCtx(ctx, "Deploying component %s/%s (hash=%s)\n", c.ComponentType, c.ProviderID, h)

			if err := d.deployComponent(ctx, ns, plan, c); err != nil {
				errMsg := fmt.Sprintf("Component deployment failed: %v", err)
				if updateErr := catalogutils.UpdateComponentStatus(ctx, d.componentRepo, c.DatabaseID, models.ComponentStatusError, errMsg); updateErr != nil {
					logger.ErrorfCtx(ctx, "Failed to update component status: %v\n", updateErr)
				}
				errCh <- fmt.Errorf("failed to deploy component %s/%s: %w", c.ComponentType, c.ProviderID, err)

				return
			}

			if err := catalogutils.UpdateComponentStatus(ctx, d.componentRepo, c.DatabaseID, models.ComponentStatusRunning, "Component deployed successfully"); err != nil {
				logger.ErrorfCtx(ctx, "Failed to update component status to Running: %v\n", err)
			}
		}(hash, comp)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("component deployment errors: %v", errs)
	}

	return nil
}

// deployComponent installs or upgrades the Helm chart for a single component.
func (d *OpenShiftDeployer) deployComponent(ctx context.Context, ns string, plan *DeploymentPlan, comp *ComponentPlan) error {
	return helmInstallOrUpgrade(ctx, ns, releaseName(plan, strings.ReplaceAll(comp.ComponentType, "_", "-")), comp.CatalogPath, comp.Values)
}

// deployServicesConcurrently installs all service Helm charts in parallel and waits for all pods
// to become Ready.
func (d *OpenShiftDeployer) deployServicesConcurrently(ctx context.Context, ns string, plan *DeploymentPlan) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(plan.Services))

	for id, svc := range plan.Services {
		wg.Add(1)

		go func(serviceID string, s *ServicePlan) {
			defer wg.Done()

			logger.InfofCtx(ctx, "Deploying service %s\n", serviceID)

			if err := d.deployService(ctx, ns, plan, s); err != nil {
				errCh <- fmt.Errorf("failed to deploy service %s: %w", serviceID, err)
			}
		}(id, svc)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return fmt.Errorf("service deployment errors: %v", errs)
	}

	return nil
}

// deployService installs or upgrades the Helm chart for a single service.
func (d *OpenShiftDeployer) deployService(ctx context.Context, ns string, plan *DeploymentPlan, svc *ServicePlan) error {
	return helmInstallOrUpgrade(ctx, ns, releaseName(plan, svc.CatalogID), svc.CatalogPath, svc.Values)
}

// helmInstallOrUpgrade loads the chart at catalogPath from the embedded CatalogFS and
// performs helm install (if the release doesn't exist) or upgrade.
func helmInstallOrUpgrade(ctx context.Context, namespace, release, catalogPath string, values map[string]any) error {
	chart, err := loadChartFromCatalogFS(catalogPath)
	if err != nil {
		return fmt.Errorf("failed to load chart at %s: %w", catalogPath, err)
	}

	logger.InfofCtx(ctx, "Deploying release '%s' in namespace '%s'\n", release, namespace)

	helmClient, err := helm.NewHelm(namespace)
	if err != nil {
		return fmt.Errorf("failed to create Helm client: %w", err)
	}

	exists, err := helmClient.IsReleaseExist(release)
	if err != nil {
		return fmt.Errorf("failed to check release existence: %w", err)
	}

	if !exists {
		return helmClient.Install(release, chart, &helm.InstallOpts{Values: values, Timeout: defaultHelmTimeout})
	}

	return helmClient.Upgrade(release, chart, &helm.UpgradeOpts{Values: values, Timeout: defaultHelmTimeout})
}

// loadChartFromCatalogFS walks assets.CatalogFS at catalogPath and returns a Helm chart.
func loadChartFromCatalogFS(catalogPath string) (helmchart.Charter, error) {
	var files []*archive.BufferedFile

	err := fs.WalkDir(&assets.CatalogFS, catalogPath, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, err := assets.CatalogFS.ReadFile(p)
		if err != nil {
			return err
		}

		rel := strings.TrimPrefix(filepath.ToSlash(p), filepath.ToSlash(catalogPath)+"/")
		files = append(files, &archive.BufferedFile{Name: rel, Data: data})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk chart directory %s: %w", catalogPath, err)
	}

	return loader.LoadFiles(files)
}

// handleStepError updates the application status to Error on a deployment step failure.
func (d *OpenShiftDeployer) handleStepError(ctx context.Context, appID uuid.UUID, stepContext string, err error) {
	errMsg := fmt.Sprintf("%s: %v", stepContext, err)
	if updateErr := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, appID, models.ApplicationStatusError, errMsg); updateErr != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status: %v\n", updateErr)
	}
}

// appNamespace derives the Kubernetes namespace from the application UUID.
// Format: "ai-services-<first 8 chars of UUID>".
func appNamespace(appID interface{ String() string }) string {
	return "ai-services-" + appID.String()[:8]
}

// releaseName builds a Helm release name: "<id>-<first 8 chars of appID>".
// e.g. "llm-2b4410e6", "vector-store-2b4410e6", "chat-c08f9a8b".
func releaseName(plan *DeploymentPlan, id string) string {
	return id + "-" + plan.ApplicationID.String()[:8]
}

// deployingStatusMessage returns the human-readable deploying status message.
func deployingStatusMessage(plan *DeploymentPlan) string {
	if plan.IsArchitecture {
		return "Deploying digital assistant"
	}

	return "Deploying service"
}
