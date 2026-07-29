package openshift

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/project-ai-services/ai-services/assets"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	deploymenttypes "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment/types"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/repository"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/helm"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	openshiftruntime "github.com/project-ai-services/ai-services/internal/pkg/runtime/openshift"
	helmchart "helm.sh/helm/v4/pkg/chart"
	"helm.sh/helm/v4/pkg/chart/loader/archive"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
)

const (
	defaultHelmTimeout    = 10 * time.Minute
	endpointTypeLabelKey  = "ai-services.io/endpoint-type"
	defaultEndpointType   = "service"
)

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
	ns := catalogutils.AppNamespace(plan.ApplicationID)

	logger.InfofCtx(ctx, "Starting OpenShift deployment for '%s' in namespace '%s'\n",
		plan.ApplicationName, ns)

	// Update application status to Deploying
	if err := catalogutils.UpdateApplicationStatus(ctx, d.appRepo, plan.ApplicationID, models.ApplicationStatusDeploying, catalogutils.DeployingStatusMessage(plan.IsArchitecture)); err != nil {
		logger.ErrorfCtx(ctx, "Failed to update application status to Deploying: %v\n", err)
	}

	// Phase 0: Deploy prerequisites (ServingRuntimes etc.), idempotent, once per namespace
	if err := d.deployPrerequisites(ctx, ns); err != nil {
		catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Prerequisites deployment failed", err)

		return err
	}

	// Phase 1: Deploy components concurrently via Helm
	if err := d.deployComponentsConcurrently(ctx, ns, plan); err != nil {
		catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Component deployment failed", err)

		return err
	}

	// Phase 2: Deploy services concurrently via Helm.
	if err := d.deployServicesConcurrently(ctx, ns, plan); err != nil {
		catalogutils.HandleDeploymentStepError(ctx, d.appRepo, plan.ApplicationID, "Service deployment failed", err)

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
	// Build hash → dbID index so RunConcurrently can track status per component.
	items := make(map[string]uuid.UUID, len(plan.Components))
	for hash, comp := range plan.Components {
		items[hash] = comp.DatabaseID
	}

	return catalogutils.RunConcurrently(
		ctx,
		items,
		func(ctx context.Context, hash string) error {
			return d.deployComponent(ctx, ns, plan, plan.Components[hash])
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateComponentStatus(ctx, d.componentRepo, dbID, models.ComponentStatusError, msg)
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateComponentStatus(ctx, d.componentRepo, dbID, models.ComponentStatusRunning, msg)
		},
	)
}

// deployComponent installs or upgrades the Helm chart for a single component,
// then registers the deterministic KServe predictor endpoint in the database.
func (d *OpenShiftDeployer) deployComponent(ctx context.Context, ns string, plan *DeploymentPlan, comp *ComponentPlan) error {
	if err := helmInstallOrUpgrade(ctx, ns, catalogutils.HelmReleaseName(plan.ApplicationID, strings.ReplaceAll(comp.ComponentType, "_", "-")), comp.CatalogPath, comp.Values); err != nil {
		return err
	}

	if err := d.updateComponentEndpoint(ctx, ns, comp); err != nil {
		// Non-fatal: log and continue — deployment itself succeeded.
		logger.ErrorfCtx(ctx, "Failed to update component %s endpoint in DB: %v\n", comp.ComponentType, err)
	}

	return nil
}

// deployServicesConcurrently installs all service Helm charts in parallel and waits for all pods
// to become Ready.
func (d *OpenShiftDeployer) deployServicesConcurrently(ctx context.Context, ns string, plan *DeploymentPlan) error {
	// Build id → dbID index so RunConcurrently can track status per service.
	items := make(map[string]uuid.UUID, len(plan.Services))
	for id, svc := range plan.Services {
		items[id] = svc.DatabaseID
	}

	return catalogutils.RunConcurrently(
		ctx,
		items,
		func(ctx context.Context, id string) error {
			return d.deployService(ctx, ns, plan, plan.Services[id])
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, dbID, models.ServiceStatusError, msg)
		},
		func(ctx context.Context, dbID uuid.UUID, msg string) error {
			return catalogutils.UpdateServiceStatus(ctx, d.serviceRepo, dbID, models.ServiceStatusRunning, msg)
		},
	)
}

// deployService installs or upgrades the Helm chart for a single service,
// then reads the OpenShift Routes for the release and stores endpoints in the database.
func (d *OpenShiftDeployer) deployService(ctx context.Context, ns string, plan *DeploymentPlan, svc *ServicePlan) error {
	releaseName := catalogutils.HelmReleaseName(plan.ApplicationID, svc.CatalogID)

	if err := helmInstallOrUpgrade(ctx, ns, releaseName, svc.CatalogPath, svc.Values); err != nil {
		return err
	}

	if err := d.registerServiceEndpoints(ctx, ns, releaseName, svc); err != nil {
		// Non-fatal: log and continue — deployment itself succeeded.
		logger.ErrorfCtx(ctx, "Failed to register service %s endpoints in DB: %v\n", svc.CatalogID, err)
	}

	return nil
}

// registerServiceEndpoints reads the OpenShift Routes created for the given Helm release
// and writes them as HTTPS endpoints into the service database record.
// Routes are identified by the label "ai-services.io/service: <releaseName>".
func (d *OpenShiftDeployer) registerServiceEndpoints(ctx context.Context, ns, releaseName string, svc *ServicePlan) error {
	rc, err := openshiftruntime.NewOpenshiftClientWithNamespace(ns)
	if err != nil {
		return fmt.Errorf("failed to create OpenShift client for namespace %s: %w", ns, err)
	}

	labelSelector := fmt.Sprintf("ai-services.io/service=%s", releaseName)
	routeList, err := rc.RouteClient.RouteV1().Routes(ns).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return fmt.Errorf("failed to list routes for release %s: %w", releaseName, err)
	}

	if len(routeList.Items) == 0 {
		logger.InfofCtx(ctx, "No routes found for release %s, skipping endpoint registration\n", releaseName)

		return nil
	}

	endpoints := make([]map[string]any, 0, len(routeList.Items))
	for _, route := range routeList.Items {
		if route.Spec.Host == "" {
			continue
		}

		endpointType := route.Labels[endpointTypeLabelKey]
		if endpointType == "" {
			endpointType = defaultEndpointType
		}

		endpoints = append(endpoints, map[string]any{
			"type": endpointType,
			"url":  fmt.Sprintf("https://%s", route.Spec.Host),
		})
	}

	if len(endpoints) == 0 {
		return nil
	}

	if err := d.serviceRepo.UpdateEndpoints(ctx, svc.DatabaseID, endpoints); err != nil {
		return fmt.Errorf("failed to update endpoints for service %s: %w", svc.CatalogID, err)
	}

	logger.InfofCtx(ctx, "Registered %d route endpoint(s) for service %s\n", len(endpoints), svc.CatalogID)

	return nil
}

// updateComponentEndpoint writes the deterministic KServe predictor DNS endpoint
// into the component database record.
// KServe (RawDeployment) creates a Service named "<inferenceServiceName>-predictor" in the namespace.
func (d *OpenShiftDeployer) updateComponentEndpoint(ctx context.Context, ns string, comp *ComponentPlan) error {
	if comp.DatabaseID == uuid.Nil {
		return nil
	}

	inferenceServiceName := inferenceServiceNameForComponent(comp.ComponentType)
	predictorDNS := fmt.Sprintf("%s-predictor.%s.svc.cluster.local", inferenceServiceName, ns)
	url := fmt.Sprintf("http://%s:8080", predictorDNS)

	endpoints := []map[string]any{
		{
			"type": "service",
			"url":  url,
		},
	}

	if err := d.componentRepo.UpdateEndpoints(ctx, comp.DatabaseID, endpoints); err != nil {
		return fmt.Errorf("failed to update endpoints for component %s: %w", comp.ComponentType, err)
	}

	logger.InfofCtx(ctx, "Registered KServe predictor endpoint for component %s: %s\n", comp.ComponentType, url)

	return nil
}

// inferenceServiceNameForComponent returns the InferenceService resource name that KServe will
// use when a component of the given type is deployed. The name is the hardcoded value in the
// component's Helm template (e.g. assets/components/llm/*/openshift/templates/*-inferenceservice.yaml).
func inferenceServiceNameForComponent(componentType string) string {
	// llm components deploy an InferenceService named "instruct" (not "llm").
	// All other component types use their component type name as the InferenceService name.
	if componentType == "llm" {
		return "instruct"
	}

	return componentType
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

	return helmClient.InstallOrUpgrade(release, chart, values, defaultHelmTimeout)
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
