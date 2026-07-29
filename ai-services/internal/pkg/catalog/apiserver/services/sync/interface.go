package sync

import (
	"context"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
)

// PodStatus holds the relevant pod status information returned by a RuntimeSync.
type PodStatus struct {
	State   string
	Health  string
	PodName string
}

// ResourceRef identifies a service or component resource for pod lookup.
// Each runtime uses a different label value on pods:
//   - Podman:    ai-services.io/template=<UUID>
//   - OpenShift: ai-services.io/template=<CatalogID>  (e.g. "digitize", "llm/vllm-cpu")
type ResourceRef struct {
	UUID      string // DB record UUID: used by Podman
	CatalogID string // Catalog identifier: used by OpenShift
}

// RuntimeSync is the interface that every runtime-specific sync backend must implement.
type RuntimeSync interface {
	// FetchPodStatuses returns the status of all pods associated with the given template ID.
	// The templateID is the service or component UUID used as the "ai-services.io/template" label value.
	// Returns an error when no pods are found or when the runtime call fails.
	FetchPodStatuses(rt runtime.Runtime, ref ResourceRef) ([]*PodStatus, error)

	// ValidateResources checks that the expected secrets and volumes exist in the runtime.
	// Returns a non-empty error message string if any resource is missing; empty string means all present.
	ValidateResources(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string
}
