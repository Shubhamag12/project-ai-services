package applicationservice

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	apimodels "github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/apiserver/services/deployment"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/types"
	catalogutils "github.com/project-ai-services/ai-services/internal/pkg/catalog/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

var errOpenShiftNotSupported = errors.New("OpenShift runtime is not yet supported")

// OpenShiftApplicationService implements ApplicationServiceInterface for the OpenShift runtime.
// It embeds ApplicationServiceBase for all shared DB operations.
type OpenShiftApplicationService struct {
	ApplicationServiceBase
}

func (s *OpenShiftApplicationService) ListApplications(_ context.Context, _ ListApplicationsRequest) (*types.ApplicationListResponse, error) {
	return nil, errOpenShiftNotSupported
}

func (s *OpenShiftApplicationService) DeleteApplication(_ context.Context, _ uuid.UUID, _ string, _ bool) (*DeleteApplicationResponse, error) {
	return nil, errOpenShiftNotSupported
}

// CreateApplication validates, plans, persists, and asynchronously deploys a new application
// using the OpenShift runtime executor.
func (s *OpenShiftApplicationService) CreateApplication(ctx context.Context, req apimodels.CreateApplicationRequest) (*apimodels.CreateApplicationResponse, error) {
	// Phase 1: check for duplicate name
	existingApp, err := s.AppRepo.GetByName(ctx, req.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to check for existing application: %w", err)
	}
	if existingApp != nil {
		return nil, &ValidationError{
			Code:    http.StatusConflict,
			Message: fmt.Sprintf(ErrMsgApplicationNameExists, req.Name),
		}
	}

	// Phase 2: validate payload
	if err := s.Validator.ValidateDeploymentRequest(ctx, req); err != nil {
		return nil, err
	}

	// Phase 3: create deployment plan
	plan, err := s.DeploymentPlanner.PlanDeployment(ctx, req, runtimeTypes.RuntimeTypeOpenShift.String())
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment plan: %w", err)
	}

	// Phase 4: persist DB records
	if err := s.InsertDeploymentRecords(ctx, plan, req.CreatedBy); err != nil {
		return nil, fmt.Errorf("failed to insert deployment records: %w", err)
	}

	// Phase 5: async deployment
	go s.executeDeploymentAsync(ctx, plan, req)

	return &apimodels.CreateApplicationResponse{ID: plan.ApplicationID.String()}, nil
}

// executeDeploymentAsync runs the OpenShift deployment in a background goroutine.
func (s *OpenShiftApplicationService) executeDeploymentAsync(parentCtx context.Context, plan *deployment.DeploymentPlan, req apimodels.CreateApplicationRequest) {
	var requestID string
	if id, ok := parentCtx.Value(logger.RequestIDKey).(string); ok {
		requestID = id
	}

	ctx := context.Background()
	if requestID != "" {
		ctx = context.WithValue(ctx, logger.RequestIDKey, requestID)
	}

	defer func() {
		if r := recover(); r != nil {
			logger.ErrorfCtx(ctx, "Panic recovered in deployment goroutine for application %s: %v", plan.ApplicationName, r)

			errMsg := fmt.Sprintf("Deployment panic: %v", r)
			if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, errMsg); updateErr != nil {
				logger.ErrorfCtx(ctx, "Failed to update application status after panic: %v", updateErr)
			}
		}
	}()

	err := s.DeploymentExecutor.ExecuteWithPlan(ctx, plan, req, runtimeTypes.RuntimeTypeOpenShift)
	if err != nil {
		logger.ErrorfCtx(ctx, "Deployment failed for application %s: %v", plan.ApplicationName, err)

		if updateErr := catalogutils.UpdateApplicationStatus(ctx, s.AppRepo, plan.ApplicationID.String(), models.ApplicationStatusError, err.Error()); updateErr != nil {
			logger.ErrorfCtx(ctx, "Failed to update application status to Error: %v", updateErr)
		}

		return
	}

	logger.InfolnCtx(ctx, fmt.Sprintf("Deployment completed successfully for application %s", plan.ApplicationName))
}

func (s *OpenShiftApplicationService) GetApplicationResources(_ context.Context, _ uuid.UUID) (*types.ApplicationResourcesResponse, error) {
	return nil, errOpenShiftNotSupported
}

func (s *OpenShiftApplicationService) ApplicationsPs(_ context.Context, _ uuid.UUID) (*types.ApplicationPSResponse, error) {
	return nil, errOpenShiftNotSupported
}

// Made with Bob
