package sync

import (
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
)

// podmanSync implements RuntimeSync for the Podman runtime.
type podmanSync struct{}

func newPodmanSync() *podmanSync {
	return &podmanSync{}
}

// FetchPodStatuses fetches all pods labelled with ref.UUID and returns their statuses.
// It uses InspectPod and InspectContainer (via common.ProcessPod) to derive state and health.
func (s *podmanSync) FetchPodStatuses(rt runtime.Runtime, ref ResourceRef) ([]*PodStatus, error) {
	filteredPods, err := common.FetchFilteredPods(rt, ref.UUID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with template ID: %s", ref.UUID)
	}

	var podStatuses []*PodStatus
	for _, pod := range filteredPods {
		processedPod, err := common.ProcessPod(rt, pod)
		if err != nil {
			return nil, fmt.Errorf("failed to process pod: %w", err)
		}

		if processedPod == nil {
			return nil, fmt.Errorf("pod processing returned nil")
		}

		podStatuses = append(podStatuses, &PodStatus{
			State:   processedPod.State,
			Health:  processedPod.Health,
			PodName: processedPod.Name,
		})
	}

	return podStatuses, nil
}

// Ensure podmanSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*podmanSync)(nil)
