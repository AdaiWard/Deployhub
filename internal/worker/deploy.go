package worker

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/deployhub-io/deployhub/internal/model"
)

func (w *Worker) deploy(ctx context.Context, job model.DeployJob) error {
	project, githubToken, err := w.loadProject(ctx, job.ProjectID)
	if err != nil {
		return err
	}
	deployment, err := w.loadDeployment(ctx, job.DeploymentID)
	if err != nil {
		return fmt.Errorf("load deployment: %w", err)
	}
	if _, err := w.db.Exec(ctx, `UPDATE deployments SET status='cloning',started_at=NOW(),error='' WHERE id=$1`, deployment.ID); err != nil {
		return err
	}
	w.log(ctx, deployment.ID, "system", "Deployment worker claimed the job")

	workspace, err := os.MkdirTemp("", "deployhub-build-*")
	if err != nil {
		return fmt.Errorf("create build workspace: %w", err)
	}
	defer os.RemoveAll(workspace)
	checkout := filepath.Join(workspace, "repository")

	gitEnv := []string{"GIT_TERMINAL_PROMPT=0"}
	if githubToken != "" {
		credentials := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + githubToken))
		gitEnv = append(gitEnv,
			"GIT_CONFIG_COUNT=1",
			"GIT_CONFIG_KEY_0=http.extraheader",
			"GIT_CONFIG_VALUE_0=Authorization: Basic "+credentials,
		)
	}
	w.log(ctx, deployment.ID, "system", "Cloning "+project.RepoFullName+" from branch "+project.Branch)
	if err := w.commandLogged(ctx, deployment.ID, workspace, gitEnv, "git", "clone", "--depth", "1", "--branch", project.Branch, "--single-branch", project.RepoURL, checkout); err != nil {
		return err
	}
	if deployment.CommitSHA != "" {
		if err := w.commandLogged(ctx, deployment.ID, checkout, gitEnv, "git", "fetch", "origin", deployment.CommitSHA, "--depth", "1"); err != nil {
			return err
		}
		if err := w.commandLogged(ctx, deployment.ID, checkout, gitEnv, "git", "checkout", "--detach", deployment.CommitSHA); err != nil {
			return err
		}
	}
	commitSHA, err := w.command(ctx, checkout, gitEnv, "git", "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	commitSHA = strings.TrimSpace(commitSHA)
	commitMessage, _ := w.command(ctx, checkout, gitEnv, "git", "log", "-1", "--pretty=%s")
	commitMessage = strings.TrimSpace(commitMessage)
	if _, err := w.db.Exec(ctx, `UPDATE deployments SET commit_sha=$1,commit_message=$2,status='building' WHERE id=$3`, commitSHA, commitMessage, deployment.ID); err != nil {
		return err
	}
	w.log(ctx, deployment.ID, "system", "Checked out commit "+shortSHA(commitSHA))

	buildContext, err := safeJoin(checkout, project.RootDirectory)
	if err != nil {
		return fmt.Errorf("invalid root directory: %w", err)
	}
	if stat, err := os.Stat(buildContext); err != nil || !stat.IsDir() {
		return fmt.Errorf("root directory %q does not exist", project.RootDirectory)
	}
	dockerfile, runtimeName, err := prepareDockerfile(buildContext, project)
	if err != nil {
		return err
	}
	w.log(ctx, deployment.ID, "system", "Runtime detected: "+runtimeName)

	imageTag := "deployhub/" + project.Slug + ":" + shortSHA(commitSHA)
	if !w.imageExists(ctx, imageTag) {
		w.log(ctx, deployment.ID, "system", "Building image "+imageTag)
		if err := w.commandLogged(ctx, deployment.ID, buildContext, nil, "docker", "build", "--progress", "plain", "--pull", "-t", imageTag,
			"--label", "deployhub.managed=true", "--label", "deployhub.project_id="+project.ID,
			"--label", "deployhub.deployment_id="+deployment.ID, "-f", dockerfile, "."); err != nil {
			return err
		}
	} else {
		w.log(ctx, deployment.ID, "system", "Existing image found, skipping build")
	}

	values, err := w.environment(ctx, project.ID)
	if err != nil {
		return fmt.Errorf("load environment: %w", err)
	}
	values["PORT"] = strconv.Itoa(project.Port)
	envFile := filepath.Join(workspace, "runtime.env")
	if err := writeEnvFile(envFile, values); err != nil {
		return err
	}

	oldDeploymentID, oldContainer := w.activeContainer(ctx, project.ID, deployment.ID)
	if _, err := w.db.Exec(ctx, `UPDATE deployments SET status='deploying',image_tag=$1 WHERE id=$2`, imageTag, deployment.ID); err != nil {
		return err
	}
	candidateName := containerName(project.Slug, deployment.ID) + "-check"
	defer w.removeContainer(context.Background(), candidateName)
	w.log(ctx, deployment.ID, "system", "Starting isolated health check container")
	if err := w.runContainer(ctx, project, deployment, imageTag, candidateName, envFile, false); err != nil {
		return err
	}
	if err := w.waitHealthy(ctx, deployment.ID, candidateName, project.Port, project.HealthcheckPath); err != nil {
		return err
	}
	w.removeContainer(ctx, candidateName)

	finalName := containerName(project.Slug, deployment.ID)
	w.log(ctx, deployment.ID, "system", "Health check passed, attaching Traefik route")
	if err := w.runContainer(ctx, project, deployment, imageTag, finalName, envFile, true); err != nil {
		return err
	}
	if !w.containerRunning(ctx, finalName) {
		w.removeContainer(context.Background(), finalName)
		return fmt.Errorf("new application container stopped during startup")
	}

	if oldContainer != "" && oldContainer != finalName {
		time.Sleep(2 * time.Second)
		w.log(ctx, deployment.ID, "system", "Removing previous application container")
		w.stopLogFollower(oldContainer)
		w.removeContainer(ctx, oldContainer)
		if oldDeploymentID != "" {
			_, _ = w.db.Exec(ctx, `UPDATE deployments SET container_name='' WHERE id=$1`, oldDeploymentID)
		}
	}
	url := w.cfg.URLScheme + "://" + project.Subdomain + "." + w.cfg.BaseDomain
	_, err = w.db.Exec(ctx, `
		UPDATE deployments SET status='succeeded',image_tag=$1,container_name=$2,url=$3,error='',finished_at=NOW()
		WHERE id=$4`, imageTag, finalName, url, deployment.ID)
	if err != nil {
		return err
	}
	w.log(ctx, deployment.ID, "system", "Deployment is live at "+url)
	w.followRuntimeLogs(context.Background(), deployment.ID, finalName, "0s")
	return nil
}

func (w *Worker) runContainer(ctx context.Context, project model.Project, deployment model.Deployment, imageTag, name, envFile string, routed bool) error {
	args := []string{"run", "-d", "--name", name, "--network", w.cfg.DockerNetwork,
		"--memory", fmt.Sprintf("%dm", project.MemoryMB), "--memory-swap", fmt.Sprintf("%dm", project.MemoryMB),
		"--cpus", strconv.FormatFloat(project.CPU, 'f', 2, 64), "--pids-limit", "256",
		"--restart", "unless-stopped", "--security-opt", "no-new-privileges:true", "--cap-drop", "ALL",
		"--read-only", "--tmpfs", "/tmp:rw,noexec,nosuid,size=64m", "--env-file", envFile,
		"--label", "deployhub.managed=true", "--label", "deployhub.project_id=" + project.ID,
		"--label", "deployhub.deployment_id=" + deployment.ID,
	}
	if routed {
		router := "deployhub-" + strings.ReplaceAll(deployment.ID[:8], "-", "")
		host := project.Subdomain + "." + w.cfg.BaseDomain
		args = append(args,
			"--label", "traefik.enable=true",
			"--label", "traefik.docker.network="+w.cfg.DockerNetwork,
			"--label", "traefik.http.routers."+router+".rule=Host(`"+host+"`)",
			"--label", "traefik.http.routers."+router+".entrypoints="+w.cfg.TraefikEntryPoint,
			"--label", "traefik.http.services."+router+".loadbalancer.server.port="+strconv.Itoa(project.Port),
		)
		if w.cfg.TraefikCertResolver != "" {
			args = append(args,
				"--label", "traefik.http.routers."+router+".tls=true",
				"--label", "traefik.http.routers."+router+".tls.certresolver="+w.cfg.TraefikCertResolver,
			)
		}
	} else {
		args = append(args, "--label", "traefik.enable=false")
	}
	args = append(args, imageTag)
	output, err := w.command(ctx, "", nil, "docker", args...)
	if err != nil {
		return err
	}
	w.log(ctx, deployment.ID, "system", "Container started: "+strings.TrimSpace(output))
	return nil
}

func (w *Worker) waitHealthy(ctx context.Context, deploymentID, container string, port int, healthPath string) error {
	client := &http.Client{Timeout: 2 * time.Second}
	address := fmt.Sprintf("http://%s:%d%s", container, port, healthPath)
	for attempt := 1; attempt <= 40; attempt++ {
		if !w.containerRunning(ctx, container) {
			logs, _ := w.command(ctx, "", nil, "docker", "logs", "--tail", "100", container)
			if logs != "" {
				w.log(ctx, deploymentID, "stderr", logs)
			}
			return fmt.Errorf("health check container exited")
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		response, err := client.Do(req)
		if err == nil {
			response.Body.Close()
			if response.StatusCode < 500 {
				w.log(ctx, deploymentID, "system", fmt.Sprintf("Health check returned HTTP %d", response.StatusCode))
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(750 * time.Millisecond):
		}
	}
	logs, _ := w.command(ctx, "", nil, "docker", "logs", "--tail", "100", container)
	if logs != "" {
		w.log(ctx, deploymentID, "stderr", logs)
	}
	return fmt.Errorf("health check did not pass within 30 seconds")
}

func (w *Worker) activeContainer(ctx context.Context, projectID, currentDeploymentID string) (string, string) {
	var deploymentID, container string
	err := w.db.QueryRow(ctx, `
		SELECT id,container_name FROM deployments
		WHERE project_id=$1 AND id<>$2 AND status='succeeded' AND container_name<>''
		ORDER BY finished_at DESC LIMIT 1`, projectID, currentDeploymentID).Scan(&deploymentID, &container)
	if err != nil || !w.containerRunning(ctx, container) {
		return "", ""
	}
	return deploymentID, container
}

func (w *Worker) imageExists(ctx context.Context, image string) bool {
	_, err := w.command(ctx, "", nil, "docker", "image", "inspect", image)
	return err == nil
}

func (w *Worker) containerRunning(ctx context.Context, container string) bool {
	if container == "" {
		return false
	}
	output, err := w.command(ctx, "", nil, "docker", "inspect", "--format", "{{.State.Running}}", container)
	return err == nil && strings.TrimSpace(output) == "true"
}

func (w *Worker) removeContainer(ctx context.Context, container string) {
	if container == "" {
		return
	}
	_, _ = w.command(ctx, "", nil, "docker", "rm", "-f", container)
}

func containerName(slug, deploymentID string) string {
	cleanID := strings.ReplaceAll(deploymentID, "-", "")
	if len(cleanID) > 8 {
		cleanID = cleanID[:8]
	}
	return "deployhub-" + slug + "-" + cleanID
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func safeJoin(root, relative string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		return "", err
	}
	if candidate != root && !strings.HasPrefix(candidate, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes repository")
	}
	return candidate, nil
}

func writeEnvFile(filename string, values map[string]string) error {
	var content strings.Builder
	for key, value := range values {
		if strings.ContainsAny(value, "\r\n") {
			return fmt.Errorf("environment variable %s contains a newline", key)
		}
		content.WriteString(key + "=" + value + "\n")
	}
	if err := os.WriteFile(filename, []byte(content.String()), 0600); err != nil {
		return fmt.Errorf("write runtime environment: %w", err)
	}
	return nil
}
