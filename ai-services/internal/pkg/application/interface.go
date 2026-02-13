package application

import (
	runtimeTypes "github.com/project-ai-services/ai-services/internal/pkg/runtime/types"
)

// Application defines the interface for application lifecycle management operations
type Application interface {
	// Type returns the runtime type
	Type() runtimeTypes.RuntimeType
}
