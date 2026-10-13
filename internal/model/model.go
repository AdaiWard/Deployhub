package model

import "time"

type User struct {
	ID        string    `json:"id"`
	GitHubID  int64     `json:"github_id,omitempty"`
	Login     string    `json:"login"`
	Name      string    `json:"name"`
	AvatarURL string    `json:"avatar_url"`
	CreatedAt time.Time `json:"created_at"`
}

type Project struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	Slug            string    `json:"slug"`
	RepoFullName    string    `json:"repo_full_name"`
	RepoURL         string    `json:"repo_url"`
	Branch          string    `json:"branch"`
	RootDirectory   string    `json:"root_directory"`
	Runtime         string    `json:"runtime"`
	DockerfilePath  string    `json:"dockerfile_path"`
	Port            int       `json:"port"`
	HealthcheckPath string    `json:"healthcheck_path"`
	Subdomain       string    `json:"subdomain"`
	AutoDeploy      bool      `json:"auto_deploy"`
	MemoryMB        int       `json:"memory_mb"`
	CPU             float64   `json:"cpu"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Deployment struct {
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	CommitSHA     string     `json:"commit_sha"`
	CommitMessage string     `json:"commit_message"`
	Status        string     `json:"status"`
	ImageTag      string     `json:"image_tag,omitempty"`
	ContainerName string     `json:"container_name,omitempty"`
	URL           string     `json:"url,omitempty"`
	Trigger       string     `json:"trigger"`
	Error         string     `json:"error,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	FinishedAt    *time.Time `json:"finished_at,omitempty"`
}

type DeploymentLog struct {
	ID           int64     `json:"id"`
	DeploymentID string    `json:"deployment_id"`
	CreatedAt    time.Time `json:"created_at"`
	Stream       string    `json:"stream"`
	Message      string    `json:"message"`
}

type DeployJob struct {
	Action       string `json:"action"`
	DeploymentID string `json:"deployment_id,omitempty"`
	ProjectID    string `json:"project_id"`
}

type Metrics struct {
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryUsageMB float64   `json:"memory_usage_mb"`
	MemoryLimitMB float64   `json:"memory_limit_mb"`
	RestartCount  int       `json:"restart_count"`
	UptimeSeconds int64     `json:"uptime_seconds"`
	CollectedAt   time.Time `json:"collected_at"`
}
