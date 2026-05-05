package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// TestServiceRepository_Insert tests the Insert method.
func TestServiceRepository_Insert(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	app := setupTestApp(t, ctx, appRepo)

	t.Run("successful insert", func(t *testing.T) {
		service := createTestService(app.ID, "chat", "1.0.0", true)
		assertServiceInsertSuccess(t, ctx, serviceRepo, service)
	})

	t.Run("insert with pre-set UUID", func(t *testing.T) {
		id := uuid.New()
		service := createTestService(app.ID, "embedding", "", false)
		service.ID = id
		assertServiceInsertWithID(t, ctx, serviceRepo, service, id)
	})

	t.Run("insert without endpoints", func(t *testing.T) {
		service := createTestService(app.ID, "instruct", "", false)
		service.Status = models.ApplicationStatusDeploying
		assertServiceInsertSuccess(t, ctx, serviceRepo, service)
	})
}

// setupTestApp creates a test application.
func setupTestApp(t *testing.T, ctx context.Context, appRepo ApplicationRepository) *models.Application {
	t.Helper()
	app := &models.Application{
		Name:      "test-app",
		Template:  "rag",
		Status:    models.ApplicationStatusRunning,
		CreatedBy: "test-user",
	}
	if err := appRepo.Insert(ctx, app); err != nil {
		t.Fatalf("Failed to insert application: %v", err)
	}

	return app
}

// createTestService creates a test service with the given parameters.
func createTestService(appID uuid.UUID, serviceType, version string, withEndpoints bool) *models.Service {
	service := &models.Service{
		AppID:   appID,
		Type:    serviceType,
		Status:  models.ApplicationStatusRunning,
		Version: version,
	}
	if withEndpoints {
		service.Endpoints = map[string]any{
			"url":  "http://localhost:8080",
			"port": 8080,
		}
	}

	return service
}

// assertServiceInsertSuccess verifies successful service insertion.
func assertServiceInsertSuccess(t *testing.T, ctx context.Context, repo ServiceRepository, service *models.Service) {
	t.Helper()
	if err := repo.Insert(ctx, service); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if service.ID == uuid.Nil {
		t.Error("Expected non-nil UUID")
	}
	if service.CreatedAt.IsZero() {
		t.Error("Expected non-zero CreatedAt")
	}
	if service.UpdatedAt.IsZero() {
		t.Error("Expected non-zero UpdatedAt")
	}
}

// assertServiceInsertWithID verifies insertion with pre-set ID.
func assertServiceInsertWithID(t *testing.T, ctx context.Context, repo ServiceRepository, service *models.Service, expectedID uuid.UUID) {
	t.Helper()
	if err := repo.Insert(ctx, service); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if service.ID != expectedID {
		t.Errorf("Expected ID %v, got %v", expectedID, service.ID)
	}
}

// TestServiceRepository_GetByAppID tests the GetByAppID method.
func TestServiceRepository_GetByAppID(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	app := setupTestApp(t, ctx, appRepo)
	insertTestServices(t, ctx, serviceRepo, app.ID)

	t.Run("get services by app ID", func(t *testing.T) {
		assertGetByAppIDSuccess(t, ctx, serviceRepo, app.ID)
	})

	t.Run("get services for non-existent app", func(t *testing.T) {
		assertGetByAppIDEmpty(t, ctx, serviceRepo)
	})
}

// insertTestServices inserts multiple test services.
func insertTestServices(t *testing.T, ctx context.Context, repo ServiceRepository, appID uuid.UUID) {
	t.Helper()
	services := []struct {
		typ     string
		url     string
		version string
		status  models.ApplicationStatus
	}{
		{"chat", "http://localhost:8080", "1.0.0", models.ApplicationStatusRunning},
		{"embedding", "http://localhost:8081", "2.0.0", models.ApplicationStatusRunning},
		{"reranker", "", "", models.ApplicationStatusDeleting},
	}

	for _, svc := range services {
		service := &models.Service{
			AppID:   appID,
			Type:    svc.typ,
			Status:  svc.status,
			Version: svc.version,
		}
		if svc.url != "" {
			service.Endpoints = map[string]any{"url": svc.url}
		}
		if err := repo.Insert(ctx, service); err != nil {
			t.Fatalf("Service insert failed: %v", err)
		}
	}
}

// assertGetByAppIDSuccess verifies successful retrieval of services.
func assertGetByAppIDSuccess(t *testing.T, ctx context.Context, repo ServiceRepository, appID uuid.UUID) {
	t.Helper()
	result, err := repo.GetByAppID(ctx, appID)
	if err != nil {
		t.Fatalf("GetByAppID failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("Expected 3 services, got %d", len(result))
	}

	verifyServiceOrder(t, result)
	verifyEndpointsUnmarshaled(t, result)
}

// verifyServiceOrder checks services are ordered by created_at ASC.
func verifyServiceOrder(t *testing.T, services []models.Service) {
	t.Helper()
	if len(services) >= 2 && services[0].CreatedAt.After(services[1].CreatedAt) {
		t.Error("Expected results ordered by created_at ASC")
	}
}

// verifyEndpointsUnmarshaled checks endpoints are properly unmarshaled.
func verifyEndpointsUnmarshaled(t *testing.T, services []models.Service) {
	t.Helper()
	for _, svc := range services {
		if svc.Type == "chat" && svc.Endpoints != nil {
			if url, ok := svc.Endpoints["url"].(string); ok && url == "http://localhost:8080" {
				return
			}
		}
	}
	t.Error("Expected to find service with properly unmarshaled endpoints")
}

// assertGetByAppIDEmpty verifies empty result for non-existent app.
func assertGetByAppIDEmpty(t *testing.T, ctx context.Context, repo ServiceRepository) {
	t.Helper()
	result, err := repo.GetByAppID(ctx, uuid.New())
	if err != nil {
		t.Fatalf("GetByAppID failed: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("Expected 0 services, got %d", len(result))
	}
}

// TestServiceRepository_Update tests the Update method.
func TestServiceRepository_Update(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	app := setupTestApp(t, ctx, appRepo)
	service := setupTestService(t, ctx, serviceRepo, app.ID)

	t.Run("successful update", func(t *testing.T) {
		updateServiceFields(service)
		assertUpdateSuccess(t, ctx, serviceRepo, service, app.ID)
	})

	t.Run("update non-existent service", func(t *testing.T) {
		assertUpdateNotFound(t, ctx, serviceRepo, app.ID)
	})
}

// setupTestService creates and inserts a test service.
func setupTestService(t *testing.T, ctx context.Context, repo ServiceRepository, appID uuid.UUID) *models.Service {
	t.Helper()
	service := &models.Service{
		AppID:  appID,
		Type:   "chat",
		Status: models.ApplicationStatusDeploying,
		Endpoints: map[string]any{
			"url": "http://localhost:8080",
		},
		Version: "1.0.0",
	}
	if err := repo.Insert(ctx, service); err != nil {
		t.Fatalf("Service insert failed: %v", err)
	}

	return service
}

// updateServiceFields updates service fields for testing.
func updateServiceFields(service *models.Service) {
	service.Status = models.ApplicationStatusRunning
	service.Endpoints = map[string]any{
		"url":  "http://localhost:9090",
		"port": 9090,
	}
	service.Version = "2.0.0"
}

// assertUpdateSuccess verifies successful service update.
func assertUpdateSuccess(t *testing.T, ctx context.Context, repo ServiceRepository, service *models.Service, appID uuid.UUID) {
	t.Helper()
	if err := repo.Update(ctx, service); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	services, err := repo.GetByAppID(ctx, appID)
	if err != nil {
		t.Fatalf("GetByAppID failed: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("Expected 1 service, got %d", len(services))
	}

	verifyUpdatedService(t, services[0])
}

// verifyUpdatedService checks updated service fields.
func verifyUpdatedService(t *testing.T, updated models.Service) {
	t.Helper()
	if updated.Status != models.ApplicationStatusRunning {
		t.Errorf("Expected status running, got %s", updated.Status)
	}
	if updated.Version != "2.0.0" {
		t.Errorf("Expected version 2.0.0, got %s", updated.Version)
	}
	if url, ok := updated.Endpoints["url"].(string); !ok || url != "http://localhost:9090" {
		t.Error("Expected updated endpoint URL")
	}
}

// assertUpdateNotFound verifies update fails for non-existent service.
func assertUpdateNotFound(t *testing.T, ctx context.Context, repo ServiceRepository, appID uuid.UUID) {
	t.Helper()
	nonExistent := &models.Service{
		ID:     uuid.New(),
		AppID:  appID,
		Type:   "fake",
		Status: models.ApplicationStatusRunning,
	}
	if err := repo.Update(ctx, nonExistent); err != pgx.ErrNoRows {
		t.Errorf("Expected ErrNoRows, got %v", err)
	}
}

// TestServiceRepository_Delete tests the Delete method.
func TestServiceRepository_Delete(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	// Create test application
	app := &models.Application{
		Name:      "test-app",
		Template:  "rag",
		Status:    models.ApplicationStatusRunning,
		CreatedBy: "test-user",
	}
	if err := appRepo.Insert(ctx, app); err != nil {
		t.Fatalf("Failed to insert application: %v", err)
	}

	// Insert test services
	service1 := &models.Service{
		AppID:  app.ID,
		Type:   "chat",
		Status: models.ApplicationStatusRunning,
	}
	service2 := &models.Service{
		AppID:  app.ID,
		Type:   "embedding",
		Status: models.ApplicationStatusRunning,
	}

	if err := serviceRepo.Insert(ctx, service1); err != nil {
		t.Fatalf("Service insert failed: %v", err)
	}
	if err := serviceRepo.Insert(ctx, service2); err != nil {
		t.Fatalf("Service insert failed: %v", err)
	}

	t.Run("successful delete", func(t *testing.T) {
		err := serviceRepo.Delete(ctx, service1.ID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify service is deleted
		services, err := serviceRepo.GetByAppID(ctx, app.ID)
		if err != nil {
			t.Fatalf("GetByAppID failed: %v", err)
		}
		if len(services) != 1 {
			t.Errorf("Expected 1 service remaining, got %d", len(services))
		}
		if services[0].ID == service1.ID {
			t.Error("Deleted service still exists")
		}
	})

	t.Run("delete non-existent service", func(t *testing.T) {
		err := serviceRepo.Delete(ctx, uuid.New())
		if err != pgx.ErrNoRows {
			t.Errorf("Expected ErrNoRows, got %v", err)
		}
	})
}

// TestServiceRepository_CascadeDelete tests that services are deleted when application is deleted.
func TestServiceRepository_CascadeDelete(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	// Create test application
	app := &models.Application{
		Name:      "test-app",
		Template:  "rag",
		Status:    models.ApplicationStatusRunning,
		CreatedBy: "test-user",
	}
	if err := appRepo.Insert(ctx, app); err != nil {
		t.Fatalf("Failed to insert application: %v", err)
	}

	// Insert test services
	for i := range 3 {
		service := &models.Service{
			AppID:  app.ID,
			Type:   "service-" + string(rune(i)),
			Status: models.ApplicationStatusRunning,
		}
		if err := serviceRepo.Insert(ctx, service); err != nil {
			t.Fatalf("Service insert failed: %v", err)
		}
	}

	t.Run("cascade delete on application delete", func(t *testing.T) {
		// Delete application
		err := appRepo.Delete(ctx, app.ID)
		if err != nil {
			t.Fatalf("Application delete failed: %v", err)
		}

		// Verify all services are deleted
		services, err := serviceRepo.GetByAppID(ctx, app.ID)
		if err != nil {
			t.Fatalf("GetByAppID failed: %v", err)
		}
		if len(services) != 0 {
			t.Errorf("Expected 0 services after cascade delete, got %d", len(services))
		}
	})
}

// Made with Bob
