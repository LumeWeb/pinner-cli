package mcp

import "context"

// ManagedService controls the lifecycle of a configured MCP service.
type ManagedService interface {
	Install(context.Context) error
	Uninstall(context.Context) error
	Start(context.Context) error
	Stop(context.Context) error
	Restart(context.Context) error
	Status(context.Context) (ServiceStatus, error)
	Logs(context.Context, bool) error
}

// ServiceSpec describes the process and environment managed by a service backend.
type ServiceSpec struct {
	Name        string
	Description string
	ExecPath    string
	Arguments   []string
	EnvFile     string
	UnitPath    string
	UserMode    bool
}

// ServiceStatus is the backend-independent service state exposed by Pinner.
type ServiceStatus struct {
	Installed bool
	Active    bool
	Ready     bool
	Summary   string
}

type serviceCommandRunner func(context.Context, string, ...string) error

type serviceCommandOutputRunner func(context.Context, string, ...string) (string, error)

// systemdUserCommand returns the systemd user-manager command prefix.
func systemdUserCommand(args ...string) []string {
	return append([]string{"--user"}, args...)
}
