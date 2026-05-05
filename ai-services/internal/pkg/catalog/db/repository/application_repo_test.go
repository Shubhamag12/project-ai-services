package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/project-ai-services/ai-services/internal/pkg/catalog/db/models"
)

// TestApplicationRepository_Insert tests the Insert method.
func TestApplicationRepository_Insert(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	repo := NewApplicationRepository(pool)
	ctx := context.Background()

	t.Run("successful insert", func(t *testing.T) {
		app := createTestApp("test-app", "rag", "Test message")
		assertInsertSuccess(t, ctx, repo, app)
	})

	t.Run("insert with pre-set UUID", func(t *testing.T) {
		id := uuid.New()
		app := &models.Application{
			ID:        id,
			Name:      "test-app-2",
			Template:  "rag-cpu",
			Status:    models.ApplicationStatusRunning,
			CreatedBy: "test-user",
		}
		assertInsertSuccessWithID(t, ctx, repo, app, id)
	})

	t.Run("insert without message", func(t *testing.T) {
		app := createTestApp("test-app-3", "rag-dev", "")
		assertInsertSuccess(t, ctx, repo, app)
	})
}

// createTestApp creates a test application with the given parameters.
func createTestApp(name, template, message string) *models.Application {
	return &models.Application{
		Name:      name,
		Template:  template,
		Status:    models.ApplicationStatusDeploying,
		Message:   message,
		CreatedBy: "test-user",
	}
}

// assertInsertSuccess verifies successful application insertion.
func assertInsertSuccess(t *testing.T, ctx context.Context, repo ApplicationRepository, app *models.Application) {
	t.Helper()
	if err := repo.Insert(ctx, app); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if app.ID == uuid.Nil {
		t.Error("Expected non-nil UUID")
	}
	if app.CreatedAt.IsZero() {
		t.Error("Expected non-zero CreatedAt")
	}
	if app.UpdatedAt.IsZero() {
		t.Error("Expected non-zero UpdatedAt")
	}
}

// assertInsertSuccessWithID verifies insertion with pre-set ID.
func assertInsertSuccessWithID(t *testing.T, ctx context.Context, repo ApplicationRepository, app *models.Application, expectedID uuid.UUID) {
	t.Helper()
	if err := repo.Insert(ctx, app); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if app.ID != expectedID {
		t.Errorf("Expected ID %v, got %v", expectedID, app.ID)
	}
}

// TestApplicationRepository_GetAll tests the GetAll method.
func TestApplicationRepository_GetAll(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	repo := NewApplicationRepository(pool)
	ctx := context.Background()

	// Insert test data
	apps := []*models.Application{
		{
			Name:      "app-1",
			Template:  "rag",
			Status:    models.ApplicationStatusRunning,
			CreatedBy: "user-1",
		},
		{
			Name:      "app-2",
			Template:  "rag-cpu",
			Status:    models.ApplicationStatusDeleting,
			Message:   "Stopped for maintenance",
			CreatedBy: "user-2",
		},
	}

	for _, app := range apps {
		if err := repo.Insert(ctx, app); err != nil {
			t.Fatalf("Insert failed: %v", err)
		}
	}

	t.Run("get all applications", func(t *testing.T) {
		result, err := repo.GetAll(ctx)
		if err != nil {
			t.Fatalf("GetAll failed: %v", err)
		}
		if len(result) != 2 {
			t.Errorf("Expected 2 applications, got %d", len(result))
		}

		// Verify order (should be DESC by created_at)
		if len(result) >= 2 {
			if result[0].CreatedAt.Before(result[1].CreatedAt) {
				t.Error("Expected results ordered by created_at DESC")
			}
		}
	})
}

// TestApplicationRepository_GetByID tests the GetByID method.
func TestApplicationRepository_GetByID(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	app := setupAppWithServices(t, ctx, appRepo, serviceRepo)

	t.Run("get by ID with services", func(t *testing.T) {
		assertGetByIDSuccess(t, ctx, appRepo, app)
	})

	t.Run("get by non-existent ID", func(t *testing.T) {
		assertGetByIDNotFound(t, ctx, appRepo)
	})
}

// setupAppWithServices creates an app with test services.
func setupAppWithServices(t *testing.T, ctx context.Context, appRepo ApplicationRepository, serviceRepo ServiceRepository) *models.Application {
	t.Helper()
	app := createTestApp("test-app", "rag", "Test message")
	app.Status = models.ApplicationStatusRunning
	if err := appRepo.Insert(ctx, app); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	services := []struct {
		typ     string
		url     string
		version string
	}{
		{"chat", "http://localhost:8080", "1.0.0"},
		{"embedding", "http://localhost:8081", ""},
	}

	for _, svc := range services {
		service := &models.Service{
			AppID:     app.ID,
			Type:      svc.typ,
			Status:    models.ApplicationStatusRunning,
			Endpoints: map[string]any{"url": svc.url},
			Version:   svc.version,
		}
		if err := serviceRepo.Insert(ctx, service); err != nil {
			t.Fatalf("Service insert failed: %v", err)
		}
	}

	return app
}

// assertGetByIDSuccess verifies successful retrieval by ID.
func assertGetByIDSuccess(t *testing.T, ctx context.Context, repo ApplicationRepository, expected *models.Application) {
	t.Helper()
	result, err := repo.GetByID(ctx, expected.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if result.ID != expected.ID {
		t.Errorf("Expected ID %v, got %v", expected.ID, result.ID)
	}
	if result.Name != expected.Name {
		t.Errorf("Expected name %s, got %s", expected.Name, result.Name)
	}
	if len(result.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(result.Services))
	}
}

// assertGetByIDNotFound verifies not found error.
func assertGetByIDNotFound(t *testing.T, ctx context.Context, repo ApplicationRepository) {
	t.Helper()
	result, err := repo.GetByID(ctx, uuid.New())
	if err != pgx.ErrNoRows {
		t.Errorf("Expected ErrNoRows, got %v", err)
	}
	if result != nil {
		t.Error("Expected nil result")
	}
}

// TestApplicationRepository_GetByName tests the GetByName method.
func TestApplicationRepository_GetByName(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	repo := NewApplicationRepository(pool)
	ctx := context.Background()

	// Insert test application
	app := &models.Application{
		Name:      "unique-app-name",
		Template:  "rag",
		Status:    models.ApplicationStatusRunning,
		CreatedBy: "test-user",
	}
	if err := repo.Insert(ctx, app); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	t.Run("get by name", func(t *testing.T) {
		result, err := repo.GetByName(ctx, "unique-app-name")
		if err != nil {
			t.Fatalf("GetByName failed: %v", err)
		}
		if result.ID != app.ID {
			t.Errorf("Expected ID %v, got %v", app.ID, result.ID)
		}
		if result.Name != app.Name {
			t.Errorf("Expected name %s, got %s", app.Name, result.Name)
		}
	})

	t.Run("get by non-existent name", func(t *testing.T) {
		result, err := repo.GetByName(ctx, "non-existent-app")
		if err != pgx.ErrNoRows {
			t.Errorf("Expected ErrNoRows, got %v", err)
		}
		if result != nil {
			t.Error("Expected nil result")
		}
	})
}

// TestApplicationRepository_UpdateDeploymentName tests the UpdateDeploymentName method.
func TestApplicationRepository_UpdateDeploymentName(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	repo := NewApplicationRepository(pool)
	ctx := context.Background()

	// Insert test application
	app := &models.Application{
		Name:      "old-name",
		Template:  "rag",
		Status:    models.ApplicationStatusRunning,
		CreatedBy: "test-user",
	}
	if err := repo.Insert(ctx, app); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	t.Run("successful update", func(t *testing.T) {
		err := repo.UpdateDeploymentName(ctx, app.ID, "new-name")
		if err != nil {
			t.Fatalf("UpdateDeploymentName failed: %v", err)
		}

		// Verify update
		result, err := repo.GetByID(ctx, app.ID)
		if err != nil {
			t.Fatalf("GetByID failed: %v", err)
		}
		if result.Name != "new-name" {
			t.Errorf("Expected name 'new-name', got %s", result.Name)
		}
		if !result.UpdatedAt.After(app.UpdatedAt) {
			t.Error("Expected UpdatedAt to be updated")
		}
	})

	t.Run("update non-existent application", func(t *testing.T) {
		err := repo.UpdateDeploymentName(ctx, uuid.New(), "some-name")
		if err != pgx.ErrNoRows {
			t.Errorf("Expected ErrNoRows, got %v", err)
		}
	})
}

// TestApplicationRepository_Delete tests the Delete method.
func TestApplicationRepository_Delete(t *testing.T) {
	pool := setupTestDB(t)
	defer cleanupTestDB(t, pool)

	appRepo := NewApplicationRepository(pool)
	serviceRepo := NewServiceRepository(pool)
	ctx := context.Background()

	// Insert test application
	app := &models.Application{
		Name:      "test-app",
		Template:  "rag",
		Status:    models.ApplicationStatusRunning,
		CreatedBy: "test-user",
	}
	if err := appRepo.Insert(ctx, app); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Insert test service
	service := &models.Service{
		AppID:  app.ID,
		Type:   "chat",
		Status: models.ApplicationStatusRunning,
	}
	if err := serviceRepo.Insert(ctx, service); err != nil {
		t.Fatalf("Service insert failed: %v", err)
	}

	t.Run("successful delete with cascade", func(t *testing.T) {
		err := appRepo.Delete(ctx, app.ID)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify application is deleted
		result, err := appRepo.GetByID(ctx, app.ID)
		if err != pgx.ErrNoRows {
			t.Errorf("Expected ErrNoRows, got %v", err)
		}
		if result != nil {
			t.Error("Expected nil result")
		}

		// Verify services are also deleted (CASCADE)
		services, err := serviceRepo.GetByAppID(ctx, app.ID)
		if err != nil {
			t.Fatalf("GetByAppID failed: %v", err)
		}
		if len(services) != 0 {
			t.Errorf("Expected 0 services after cascade delete, got %d", len(services))
		}
	})

	t.Run("delete non-existent application", func(t *testing.T) {
		err := appRepo.Delete(ctx, uuid.New())
		if err != pgx.ErrNoRows {
			t.Errorf("Expected ErrNoRows, got %v", err)
		}
	})
}

// setupTestDB creates a test database connection pool
// Note: This requires a running PostgreSQL instance for integration tests.
func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()

	// Skip if not running integration tests
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	ctx := context.Background()

	// Use test database connection string from environment or default
	connString := "postgres://postgres:postgres@localhost:5432/ai_services_test?sslmode=disable"

	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Run migrations or create tables
	setupTestSchema(t, pool)

	return pool
}

// cleanupTestDB cleans up test data and closes the connection.
func cleanupTestDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Clean up test data
	_, err := pool.Exec(ctx, "TRUNCATE TABLE services, applications CASCADE")
	if err != nil {
		t.Logf("Warning: failed to truncate tables: %v", err)
	}

	pool.Close()
}

// setupTestSchema creates the necessary tables for testing.
func setupTestSchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	createStatusEnum(t, ctx, pool)
	createApplicationsTable(t, ctx, pool)
	createServicesTable(t, ctx, pool)
	createTriggerFunction(t, ctx, pool)
	createTriggers(t, ctx, pool)
	truncateTables(t, ctx, pool)
}

// createStatusEnum creates the status enum type.
func createStatusEnum(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		DO $$ BEGIN
			CREATE TYPE status AS ENUM ('Downloading', 'Deploying', 'Running', 'Deleting', 'Error');
		EXCEPTION
			WHEN duplicate_object THEN null;
		END $$;
	`)
	if err != nil {
		t.Fatalf("Failed to create status enum: %v", err)
	}
}

// createApplicationsTable creates the applications table.
func createApplicationsTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS applications (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(100),
			template VARCHAR(100),
			status status,
			message TEXT,
			created_by VARCHAR(100),
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create applications table: %v", err)
	}
}

// createServicesTable creates the services table.
func createServicesTable(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS services (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			app_id UUID NOT NULL,
			type VARCHAR(100),
			status status,
			endpoints JSONB,
			version TEXT,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW(),
			CONSTRAINT fk_app_id FOREIGN KEY (app_id) REFERENCES applications(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create services table: %v", err)
	}
}

// createTriggerFunction creates the update timestamp trigger function.
func createTriggerFunction(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		CREATE OR REPLACE FUNCTION update_updated_at_column()
		RETURNS TRIGGER AS $$
		BEGIN
			NEW.updated_at = NOW();
			RETURN NEW;
		END;
		$$ LANGUAGE plpgsql;
	`)
	if err != nil {
		t.Fatalf("Failed to create trigger function: %v", err)
	}
}

// createTriggers creates update triggers for tables.
func createTriggers(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	triggers := []struct {
		name  string
		table string
	}{
		{"update_applications_updated_at", "applications"},
		{"update_services_updated_at", "services"},
	}

	for _, tr := range triggers {
		query := fmt.Sprintf(`
			DROP TRIGGER IF EXISTS %s ON %s;
			CREATE TRIGGER %s
				BEFORE UPDATE ON %s
				FOR EACH ROW
				EXECUTE FUNCTION update_updated_at_column();
		`, tr.name, tr.table, tr.name, tr.table)
		if _, err := pool.Exec(ctx, query); err != nil {
			t.Fatalf("Failed to create %s trigger: %v", tr.name, err)
		}
	}
}

// truncateTables cleans any existing test data.
func truncateTables(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, "TRUNCATE TABLE services, applications CASCADE"); err != nil {
		t.Fatalf("Failed to truncate tables: %v", err)
	}
}

// Made with Bob
