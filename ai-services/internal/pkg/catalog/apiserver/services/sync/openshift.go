package sync

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
)

// openShiftSync implements RuntimeSync for the OpenShift runtime.
type openShiftSync struct{}

func newOpenShiftSync() *openShiftSync {
	return &openShiftSync{}
}

// FetchPodStatuses fetches all pods labelled with the given templateID using the OpenShift runtime.
func (s *openShiftSync) FetchPodStatuses(rt runtime.Runtime, templateID string) ([]*PodStatus, error) {
	filteredPods, err := common.FetchFilteredPods(rt, templateID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", templateID)
	}

	podStatuses := make([]*PodStatus, 0, len(filteredPods))
	for _, pod := range filteredPods {
		podStatuses = append(podStatuses, &PodStatus{
			State:   pod.State,
			Health:  pod.Health,
			PodName: pod.Name,
		})
	}

	return podStatuses, nil
}

// ValidateResources checks that the expected Kubernetes Secrets and PVCs (volumes) exist.
func (s *openShiftSync) ValidateResources(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string {
	return validateResources(ctx, expectedSecretNames, expectedVolumeNames, rt)
}

// Ensure openShiftSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*openShiftSync)(nil)
