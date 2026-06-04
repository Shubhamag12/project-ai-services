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
    type unconfined_t;
    type container_file_t;
    type postgresql_port_t;
    type node_t;
    type http_cache_port_t;

    # Marks ai_services_root_t as a process domain
    attribute domain;

    # Marks ai_services_root_t as a container domain so that container
    # runtime rules (networking, mounts, etc.) apply to it
    attribute container_domain;

    # Required for MCS label validation — without this the kernel rejects
    # the context as invalid when keycreate or any MCS-tagged label is set
    attribute mcs_constrained_type;

    # Required for this type to be a valid target for process transitions
    # and context assignments (e.g. runcon, /proc/self/attr/keycreate)
    attribute process_user_target;

    class sock_file { getattr open read write };
    class unix_stream_socket connectto;
    class tcp_socket { create connect listen name_bind name_connect node_bind };

    # Needed for the process transition rule below
    class process { transition setcurrent setexec setkeycreate setsockcreate };

    # Needed for kernel keyring creation under this label
    class key { create link read search setattr view write };

    # Needed to execute binaries from the container overlay filesystem
    class file { entrypoint execute execute_no_trans getattr ioctl map open read };

    # Needed to read symlinks (e.g. /lib64) inside the container overlay filesystem
    class lnk_file read;

    # Needed to open /dev/tty inside the container
    class chr_file { getattr ioctl open read write };

    role system_r;
}

type ai_services_root_t;

# Associate ai_services_root_t with the domain attribute
typeattribute ai_services_root_t domain;

# Associate ai_services_root_t with container_domain attribute
typeattribute ai_services_root_t container_domain;

# Required for the kernel to accept this type as a valid MCS-tagged context
typeattribute ai_services_root_t mcs_constrained_type;

# Required for this type to be a valid target for process transitions
# and context assignments (e.g. runcon, /proc/self/attr/keycreate)
typeattribute ai_services_root_t process_user_target;

# Allow system_r role to use this type — without this the kernel rejects
# system_u:system_r:ai_services_root_t as an invalid context
role system_r types ai_services_root_t;

# Allow the container runtime (running as container_t) to transition
# the process into ai_services_root_t when the security label is set
allow container_t ai_services_root_t:process { transition setcurrent setexec setkeycreate setsockcreate };

# Allow unconfined_t (conmon/podman) to transition into ai_services_root_t
allow unconfined_t ai_services_root_t:process { transition setcurrent setexec setkeycreate setsockcreate };

# Allow the process, once running as ai_services_root_t, to set its own
# keyring and socket creation labels (crun does a second round of context
# setup from within the container process itself)
allow ai_services_root_t ai_services_root_t:process { setkeycreate setsockcreate };

# Allow the custom type to create and use kernel keyrings
allow ai_services_root_t ai_services_root_t:key { create link read search setattr view write };

# Allow the custom type to listen on its own tcp sockets
allow ai_services_root_t ai_services_root_t:tcp_socket { create connect listen };

# Allow the custom type to execute binaries from the container overlay filesystem
allow ai_services_root_t container_file_t:file { entrypoint execute execute_no_trans getattr ioctl map open read };

# Allow the custom type to read symlinks (e.g. /lib64) inside the container overlay filesystem
allow ai_services_root_t container_file_t:lnk_file read;

# Allow the custom type to open /dev/tty inside the container
allow ai_services_root_t container_file_t:chr_file { getattr ioctl open read write };

# Allow the custom type to connect to PostgreSQL on port 5432
allow ai_services_root_t postgresql_port_t:tcp_socket { create connect name_connect };

# Allow the custom type to bind to loopback/localhost nodes
allow ai_services_root_t node_t:tcp_socket node_bind;

# Allow the custom type to bind to port 8080 (labeled http_cache_port_t)
allow ai_services_root_t http_cache_port_t:tcp_socket name_bind;

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
