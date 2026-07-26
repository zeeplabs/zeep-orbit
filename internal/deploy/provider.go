package deploy

import "context"

type CreateServiceParams struct {
	RepoOwner    string
	RepoName     string
	ServiceType  string // "static_site" | "web_service"
	BuildCommand string
	PublishPath  string // static_site
	StartCommand string // web_service
	EnvVars      map[string]string
}

type ServiceInfo struct {
	ServiceID string
	URL       string
}

type DeployProvider interface {
	CreateService(ctx context.Context, params CreateServiceParams) (ServiceInfo, error)
	DeleteService(ctx context.Context, serviceID string) error
}
