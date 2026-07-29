package sync

import (
	"context"
	"fmt"

	"github.com/project-ai-services/ai-services/internal/pkg/constants"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/common"
	runtimetypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// openShiftSync implements RuntimeSync for the OpenShift runtime.
type openShiftSync struct{}

func newOpenShiftSync() *openShiftSync {
	return &openShiftSync{}
}

// FetchPodStatuses fetches all pods labelled with ref.CatalogID using the OpenShift runtime.
func (s *openShiftSync) FetchPodStatuses(rt runtime.Runtime, ref ResourceRef) ([]*PodStatus, error) {
	filteredPods, err := common.FetchFilteredPods(rt, ref.CatalogID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pods: %w", err)
	}

	if len(filteredPods) == 0 {
		return nil, fmt.Errorf("no pod found with catalog ID: %s", ref.CatalogID)
	}

	podStatuses := make([]*PodStatus, 0, len(filteredPods))
	for _, pod := range filteredPods {
		inspected, err := rt.InspectPod(pod.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect pod %s: %w", pod.Name, err)
		}

		if inspected == nil {
			return nil, fmt.Errorf("pod inspect returned nil for %s", pod.Name)
		}

		podStatuses = append(podStatuses, &PodStatus{
			State:   inspected.State,
			Health:  derivePodHealth(inspected),
			PodName: inspected.Name,
		})
	}

	return podStatuses, nil
}

// derivePodHealth aggregates container-level health into a single pod health string
// using the same constants.Ready / constants.NotReady conventions that determinePodStatus expects.
func derivePodHealth(pod *runtimetypes.Pod) string {
	if pod.State != "Running" {
		return string(constants.NotReady)
	}

	for _, c := range pod.Containers {
		if c.Health != string(constants.Ready) {
			return string(constants.NotReady)
		}
	}

	return string(constants.Ready)
}

// ValidateResources checks that the expected Kubernetes Secrets and PVCs (volumes) exist.
func (s *openShiftSync) ValidateResources(ctx context.Context, expectedSecretNames, expectedVolumeNames []string, rt runtime.Runtime) string {
	return validateResources(ctx, expectedSecretNames, expectedVolumeNames, rt)
}

// Ensure openShiftSync implements RuntimeSync at compile time.
var _ RuntimeSync = (*openShiftSync)(nil)
