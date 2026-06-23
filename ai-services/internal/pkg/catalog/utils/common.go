package utils

import (
	"fmt"
	"strconv"

	catalogConstants "github.com/project-ai-services/ai-services/internal/pkg/catalog/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// CatalogPodConfig holds configuration retrieved from a catalog pod.
type CatalogPodConfig struct {
	BaseDir    string
	DomainName string
	HttpsPort  int
}

// GetCatalogPodConfig retrieves catalog pod configuration by inspecting the running pod and its containers.
// It extracts environment variables like AI_SERVICES_BASE_DIR, DOMAIN_SUFFIX, and CADDY_HTTPS_PORT.
func GetCatalogPodConfig(rt runtime.Runtime) (*CatalogPodConfig, string, error) {
	// Build filter to find all pods using the catalog secret via label
	logger.Debugf("Getting catalog pod configuration")
	filter := map[string][]string{
		"label": {fmt.Sprintf(
			"%s=%s",
			catalogConstants.CatalogSecretLabel,
			catalogConstants.CatalogSecretName,
		)},
	}

	// List all pods that reference the catalog secret
	pods, err := rt.ListPods(filter)
	if err != nil {
		return nil, "", fmt.Errorf("failed to list pods: %w", err)
	}
	if len(pods) == 0 {
		return nil, "", fmt.Errorf("no catalog pod found")
	}

	// Inspect catalog pod
	pod := pods[0]
	pInfo, err := rt.InspectPod(pod.ID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
	}

	config := &CatalogPodConfig{}

	for _, container := range pInfo.Containers {
		// Inspect container to get environment variables
		cInfo, err := rt.InspectContainer(container.ID)
		if err != nil {
			return nil, "", fmt.Errorf("failed to inspect container %s: %w", container.Name, err)
		}
		extractConfigFromEnv(cInfo.Env, config)
	}

	return config, pod.ID, nil
}

// extractConfigFromEnv extracts configuration values from container environment variables.
func extractConfigFromEnv(podEnv map[string]string, config *CatalogPodConfig) {
	if value, ok := podEnv["AI_SERVICES_BASE_DIR"]; ok {
		config.BaseDir = value
	}
	if value, ok := podEnv["DOMAIN_SUFFIX"]; ok {
		config.DomainName = value
	}
	if value, ok := podEnv["CADDY_HTTPS_PORT"]; ok {
		config.HttpsPort, _ = strconv.Atoi(value)
	}
}

// Made with Bob
