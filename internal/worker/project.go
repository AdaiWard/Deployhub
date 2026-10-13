package worker

import (
	"context"
	"fmt"

	"github.com/deployhub-io/deployhub/internal/model"
)

func (w *Worker) loadProject(ctx context.Context, projectID string) (model.Project, string, error) {
	var project model.Project
	var encryptedToken string
	err := w.db.QueryRow(ctx, `
		SELECT p.id,p.user_id,p.name,p.slug,p.repo_full_name,p.repo_url,p.branch,p.root_directory,
			p.runtime,p.dockerfile_path,p.port,p.healthcheck_path,p.subdomain,p.auto_deploy,
			p.memory_mb,p.cpu,p.created_at,p.updated_at,u.access_token_encrypted
		FROM projects p JOIN users u ON u.id=p.user_id WHERE p.id=$1`, projectID).
		Scan(&project.ID, &project.UserID, &project.Name, &project.Slug, &project.RepoFullName, &project.RepoURL,
			&project.Branch, &project.RootDirectory, &project.Runtime, &project.DockerfilePath, &project.Port,
			&project.HealthcheckPath, &project.Subdomain, &project.AutoDeploy, &project.MemoryMB, &project.CPU,
			&project.CreatedAt, &project.UpdatedAt, &encryptedToken)
	if err != nil {
		return model.Project{}, "", fmt.Errorf("load project: %w", err)
	}
	token := ""
	if encryptedToken != "" {
		token, err = w.box.Decrypt(encryptedToken)
		if err != nil {
			return model.Project{}, "", fmt.Errorf("decrypt GitHub token: %w", err)
		}
	}
	return project, token, nil
}

func (w *Worker) loadDeployment(ctx context.Context, deploymentID string) (model.Deployment, error) {
	var deployment model.Deployment
	err := w.db.QueryRow(ctx, `
		SELECT id,project_id,commit_sha,commit_message,status,image_tag,container_name,url,trigger,error,created_at,started_at,finished_at
		FROM deployments WHERE id=$1`, deploymentID).
		Scan(&deployment.ID, &deployment.ProjectID, &deployment.CommitSHA, &deployment.CommitMessage, &deployment.Status,
			&deployment.ImageTag, &deployment.ContainerName, &deployment.URL, &deployment.Trigger, &deployment.Error,
			&deployment.CreatedAt, &deployment.StartedAt, &deployment.FinishedAt)
	return deployment, err
}

func (w *Worker) environment(ctx context.Context, projectID string) (map[string]string, error) {
	rows, err := w.db.Query(ctx, `SELECT key,value_encrypted FROM environment_variables WHERE project_id=$1`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make(map[string]string)
	for rows.Next() {
		var key, encrypted string
		if err := rows.Scan(&key, &encrypted); err != nil {
			return nil, err
		}
		value, err := w.box.Decrypt(encrypted)
		if err != nil {
			return nil, fmt.Errorf("decrypt environment %s: %w", key, err)
		}
		values[key] = value
	}
	return values, rows.Err()
}
