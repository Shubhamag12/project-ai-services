package application

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/project-ai-services/ai-services/internal/pkg/application"
	appTypes "github.com/project-ai-services/ai-services/internal/pkg/application/types"
	catalogClient "github.com/project-ai-services/ai-services/internal/pkg/catalog/client"
	appFlags "github.com/project-ai-services/ai-services/internal/pkg/cli/constants/application"
	"github.com/project-ai-services/ai-services/internal/pkg/cli/flagvalidator"
	cliUtils "github.com/project-ai-services/ai-services/internal/pkg/cli/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/logger"
	"github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
	"github.com/project-ai-services/ai-services/internal/pkg/utils"
	"github.com/project-ai-services/ai-services/internal/pkg/vars"
)

var (
	skipCleanup        bool
	deleteTimeout      time.Duration
	experimentalDelete bool
)

var deleteCmd = &cobra.Command{
	Use:   "delete [name]",
	Short: "Delete an application",
	Long: `Deletes an application and all associated resources.

Arguments
  [name]: Application name (required)`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// Build and run flag validator
		flagValidator := buildDeleteFlagValidator()
		if err := flagValidator.Validate(cmd); err != nil {
			return err
		}

		appName := args[0]
		if !experimentalDelete {
			return utils.VerifyAppName(appName)
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		applicationName := args[0]

		// Once precheck passes, silence usage for any *later* internal errors.
		cmd.SilenceUsage = true

		rt := vars.RuntimeFactory.GetRuntimeType()

		// When experimentalDelete is true and runtime is podman, validate application name using catalog API
		// For openshift runtime, always use the older/stable code path regardless of experimental flag
		if experimentalDelete && rt == types.RuntimeTypePodman {
			return deleteApplication(applicationName)
		}

		// Create application instance using factory
		factory := application.NewFactory(rt)
		app, err := factory.Create(applicationName)
		if err != nil {
			return fmt.Errorf("failed to create application instance: %w", err)
		}

		opts := appTypes.DeleteOptions{
			Name:        applicationName,
			AutoYes:     autoYes,
			SkipCleanup: skipCleanup,
			Timeout:     deleteTimeout,
		}

		return app.Delete(cmd.Context(), opts)

	},
}

func init() {
	initDeleteCommonFlags()
	initDeleteOpenShiftFlags()
}

func initDeleteCommonFlags() {
	deleteCmd.Flags().BoolVar(&skipCleanup, appFlags.Delete.SkipCleanup, false, "Skip deleting application data (default=false)")
	deleteCmd.Flags().BoolVarP(&autoYes, appFlags.Delete.AutoYes, "y", false, "Automatically accept all confirmation prompts (default=false)")
	deleteCmd.Flags().BoolVar(&experimentalDelete, "experimental", false, "Include experimental application delete")
}

func initDeleteOpenShiftFlags() {
	deleteCmd.Flags().DurationVar(
		&deleteTimeout,
		appFlags.Delete.Timeout,
		0, // default
		"Timeout for the operation (e.g. 10s, 2m, 1h).\n"+
			"Note: Supported for openshift runtime only.\n",
	)
}

// buildDeleteFlagValidator creates and configures the flag validator for the delete command.
func buildDeleteFlagValidator() *flagvalidator.FlagValidator {
	runtimeType := vars.RuntimeFactory.GetRuntimeType()

	builder := flagvalidator.NewFlagValidatorBuilder(runtimeType)

	// Register common flags
	builder.
		AddCommonFlag(appFlags.Delete.SkipCleanup, nil).
		AddCommonFlag(appFlags.Delete.AutoYes, nil)

	// Register OpenShift-specific flags
	builder.
		AddOpenShiftFlag(appFlags.Delete.Timeout, nil)

	return builder.Build()
}

func deleteApplication(appName string) error {
	appDir := filepath.Join(utils.GetApplicationsPath(), filepath.Base(appName))
	appExists := utils.FileExists(appDir)
	appClient, err := catalogClient.NewApplicationClient()
	if err != nil {
		return fmt.Errorf("failed to create application client: %w", err)
	}
	app, err := cliUtils.GetAppByName(appClient, appName)
	if err != nil {
		return err
	}

	pods, err := cliUtils.GetPodsFromApplicationsPS(appName)
	if err != nil {
		return err
	}

	logPodsToBeDeleted(appName, pods)
	podsExists := len(pods) != 0

	if !podsExists {
		logger.Infof("No pods found for application: %s\n", appName)

		return nil
	}

	if !autoYes {
		confirmDelete, err := deleteConfirmation(appName, podsExists, appExists, skipCleanup)
		if err != nil {
			return err
		}
		if !confirmDelete {
			logger.Infoln("Deletion cancelled")

			return nil
		}
	}

	deleteParams := catalogClient.DeleteApplicationParams{
		KeepData: skipCleanup,
	}

	if err := appClient.DeleteApplication(app.ID, &deleteParams); err != nil {
		return fmt.Errorf("failed to delete application: %w", err)
	}

	// Poll to verify deletion is complete
	logger.Infof("Waiting for application %s to be deleted...\n", appName)
	if err := waitForApplicationDeletion(appClient, appName); err != nil {
		return fmt.Errorf("failed to verify application deletion: %w", err)
	}

	logger.Infof("Application %s deleted successfully.", appName)

	return nil
}

// waitForApplicationDeletion polls the application status until it's fully deleted.
func waitForApplicationDeletion(appClient *catalogClient.ApplicationClient, appName string) error {
	const (
		pollInterval = 3 * time.Second
		maxAttempts  = 10
	)

	for range maxAttempts {
		// Check if application still exists
		_, err := cliUtils.GetAppByName(appClient, appName)
		if err != nil {
			// If application is not found, it's been deleted
			// Check if pods are also gone
			pods, podErr := cliUtils.GetPodsFromApplicationsPS(appName)
			if podErr != nil || len(pods) == 0 {
				// Application and pods are deleted
				return nil
			}
		}

		// Wait before next poll
		time.Sleep(pollInterval)
	}

	return fmt.Errorf("timeout waiting for application deletion after %v", maxAttempts*pollInterval)
}

func logPodsToBeDeleted(appName string, pods []types.Pod) {
	logger.Infof("Found %d pods for given applicationName: %s.\n", len(pods), appName)
	logger.Infoln("Below are the list of pods to be deleted")
	for _, pod := range pods {
		logger.Infof("\t-> %s\n", pod.Name)
	}
}

func deleteConfirmation(appName string, podsExists, appExists, skipCleanup bool) (bool, error) {
	var confirmActionPrompt string
	if podsExists && appExists && !skipCleanup {
		confirmActionPrompt = "Are you sure you want to delete the above pods and application data? "
	} else if podsExists {
		confirmActionPrompt = "Are you sure you want to delete the above pods? "
	} else if appExists && !skipCleanup {
		confirmActionPrompt = "Are you sure you want to delete the application data? "
	} else {
		logger.Infof("Application %s does not exist", appName)

		return false, nil
	}

	confirmDelete, err := utils.ConfirmAction(confirmActionPrompt)
	if err != nil {
		return confirmDelete, fmt.Errorf("failed to take user input: %w", err)
	}

	return confirmDelete, nil
}
