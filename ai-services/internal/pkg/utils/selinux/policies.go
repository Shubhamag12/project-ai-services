package selinux

// VFIOPolicyContent defines the SELinux policy for VFIO device access.
// This allows containers with container_t type to access VFIO devices.
const VFIOPolicyContent = `
module vllm_vfio_policy 1.0;

require {
    type container_t;
    type vfio_device_t;
    class chr_file { ioctl open read write getattr };
}

# Allow container_t (vLLM) to access vfio_device_t
allow container_t vfio_device_t:chr_file { ioctl open read write getattr };
`

// AiServicesRootPolicy defines the SELinux policy for root Podman socket access.
const AiServicesRootPolicy = `
module ai_services_root_policy 1.0;

require {
    type var_run_t;
    type container_runtime_t;
    type container_t;

    # Marks ai_services_root_t as a process domain
    attribute domain;

    # Marks ai_services_root_t as a container domain so that container
    # runtime rules (networking, mounts, etc.) apply to it
    attribute container_domain;

    class sock_file { getattr open read write };
    class unix_stream_socket connectto;

    # Needed for the process transition rule below
    class process transition;
}

type ai_services_root_t;

# Associate ai_services_root_t with the domain attribute
typeattribute ai_services_root_t domain;

# Associate ai_services_root_t with container_domain attribute
typeattribute ai_services_root_t container_domain;

# Allow the container runtime (running as container_t) to transition
# the process into ai_services_root_t when the security label is set
allow container_t ai_services_root_t:process transition;

# Allow the custom type to access the root podman socket
allow ai_services_root_t var_run_t:sock_file { getattr open read write };
allow ai_services_root_t var_run_t:unix_stream_socket connectto;
allow ai_services_root_t container_runtime_t:unix_stream_socket connectto;
`

// AiServicesNonRootPolicy defines the SELinux policy for rootless Podman socket access.
const AiServicesNonRootPolicy = `
module ai_services_nonroot_policy 1.0;

require {
    type user_tmp_t;
    class sock_file { getattr open read write };
    class unix_stream_socket connectto;
    class dir search;
}

type ai_services_nonroot_t;

allow ai_services_nonroot_t user_tmp_t:sock_file { getattr open read write };
allow ai_services_nonroot_t user_tmp_t:unix_stream_socket connectto;
allow ai_services_nonroot_t user_tmp_t:dir search;
`

// Made with Bob
