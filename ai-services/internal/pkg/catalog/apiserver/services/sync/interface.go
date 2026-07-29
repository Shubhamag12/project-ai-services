package sync

import (
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
//   - OpenShift: ai-services.io/template=<CatalogID>  (e.g. "digitize", "vector-store-opensearch")
type ResourceRef struct {
	UUID      string // DB record UUID: used by Podman
	CatalogID string // Catalog identifier: used by OpenShift
}

// RuntimeSync is the interface that every runtime-specific sync backend must implement.
type RuntimeSync interface {
	// FetchPodStatuses returns the status of all pods for the given resource reference.
	// Each runtime picks the correct identifier from ResourceRef to match the
	// ai-services.io/template label value used in its deployed pods.
	FetchPodStatuses(rt runtime.Runtime, ref ResourceRef) ([]*PodStatus, error)
}
