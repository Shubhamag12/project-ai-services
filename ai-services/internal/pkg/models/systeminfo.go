package models

// SystemInfo represents system resource information.
type SystemInfo struct {
	VCPU         *VCPUInfo                   `json:"vcpu,omitempty"`
	Memory       *MemoryInfo                 `json:"memory,omitempty"`
	Accelerators map[string]*AcceleratorInfo `json:"accelerators,omitempty"`
}

// VCPUInfo represents vCPU utilization information.
type VCPUInfo struct {
	Total     int     `json:"total"`
	Available float64 `json:"available"`
}

// MemoryInfo represents memory usage information.
type MemoryInfo struct {
	TotalBytes     int64 `json:"total_bytes"`
	AvailableBytes int64 `json:"available_bytes"`
}

// AcceleratorInfo represents accelerator availability information.
type AcceleratorInfo struct {
	Total     int `json:"total"`
	Available int `json:"available"`
}

// Made with Bob
