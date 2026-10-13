package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/deployhub-io/deployhub/internal/model"
	"github.com/google/uuid"
)

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	limit := 30
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 100 {
		limit = value
	}
	rows, err := s.db.Query(r.Context(), deploymentSelect+` WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2`, project.ID, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load deployments")
		return
	}
	defer rows.Close()
	deployments := make([]model.Deployment, 0)
	for rows.Next() {
		deployment, err := scanDeployment(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read deployments")
			return
		}
		deployments = append(deployments, deployment)
	}
	writeJSON(w, http.StatusOK, deployments)
}

func (s *Server) createDeployment(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	var input struct {
		CommitSHA string `json:"commit_sha"`
	}
	if r.ContentLength > 0 && !decodeJSON(w, r, &input) {
		return
	}
	deployment, err := s.enqueueDeployment(r.Context(), project, strings.TrimSpace(input.CommitSHA), "", "manual")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not queue deployment")
		return
	}
	writeJSON(w, http.StatusAccepted, deployment)
}

func (s *Server) restartProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	if err := s.queue.Enqueue(r.Context(), model.DeployJob{Action: "restart", ProjectID: project.ID}); err != nil {
		writeError(w, http.StatusInternalServerError, "could not queue restart")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "queued"})
}

func (s *Server) getDeployment(w http.ResponseWriter, r *http.Request) {
	deployment, err := s.deploymentForUser(r, r.PathValue("deploymentID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	writeJSON(w, http.StatusOK, deployment)
}

func (s *Server) getDeploymentLogs(w http.ResponseWriter, r *http.Request) {
	deployment, err := s.deploymentForUser(r, r.PathValue("deploymentID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	logs, err := s.logsAfter(r.Context(), deployment.ID, after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load deployment logs")
		return
	}
	writeJSON(w, http.StatusOK, logs)
}

func (s *Server) deploymentEvents(w http.ResponseWriter, r *http.Request) {
	deployment, err := s.deploymentForUser(r, r.PathValue("deploymentID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusNotImplemented, "streaming is unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	lastLogID := int64(0)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	_, _ = fmt.Fprint(w, "event: connected\ndata: {}\n\n")
	flusher.Flush()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			logs, err := s.logsAfter(r.Context(), deployment.ID, lastLogID)
			if err != nil {
				return
			}
			for _, entry := range logs {
				payload, _ := json.Marshal(entry)
				_, _ = fmt.Fprintf(w, "event: log\ndata: %s\n\n", payload)
				lastLogID = entry.ID
			}
			current, err := scanDeployment(s.db.QueryRow(r.Context(), deploymentSelect+` WHERE id=$1`, deployment.ID))
			if err != nil {
				return
			}
			payload, _ := json.Marshal(current)
			_, _ = fmt.Fprintf(w, "event: status\ndata: %s\n\n", payload)
			flusher.Flush()
			if current.Status == "succeeded" || current.Status == "failed" || current.Status == "cancelled" {
				return
			}
		}
	}
}

func (s *Server) rollbackDeployment(w http.ResponseWriter, r *http.Request) {
	previous, err := s.deploymentForUser(r, r.PathValue("deploymentID"))
	if err != nil || previous.CommitSHA == "" {
		writeError(w, http.StatusNotFound, "deployment cannot be rolled back")
		return
	}
	project, err := s.projectForUser(r, previous.ProjectID)
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	deployment, err := s.enqueueDeployment(r.Context(), project, previous.CommitSHA, previous.CommitMessage, "rollback")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not queue rollback")
		return
	}
	writeJSON(w, http.StatusAccepted, deployment)
}

func (s *Server) projectMetrics(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	payload, err := s.queue.Client().Get(r.Context(), "deployhub:metrics:"+project.ID).Bytes()
	if err != nil {
		writeJSON(w, http.StatusOK, model.Metrics{CollectedAt: time.Now().UTC()})
		return
	}
	var metrics model.Metrics
	if err := json.Unmarshal(payload, &metrics); err != nil {
		writeError(w, http.StatusInternalServerError, "could not read metrics")
		return
	}
	writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) enqueueDeployment(ctx context.Context, project model.Project, commitSHA, commitMessage, trigger string) (model.Deployment, error) {
	deployment := model.Deployment{
		ID: uuid.NewString(), ProjectID: project.ID, CommitSHA: commitSHA,
		CommitMessage: commitMessage, Status: "queued", Trigger: trigger,
		URL: s.cfg.URLScheme + "://" + project.Subdomain + "." + s.cfg.BaseDomain,
	}
	err := s.db.QueryRow(ctx, `
		INSERT INTO deployments (id,project_id,commit_sha,commit_message,status,url,trigger)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+deploymentColumns,
		deployment.ID, deployment.ProjectID, deployment.CommitSHA, deployment.CommitMessage,
		deployment.Status, deployment.URL, deployment.Trigger).Scan(deploymentPointers(&deployment)...)
	if err != nil {
		return model.Deployment{}, err
	}
	if err := s.queue.Enqueue(ctx, model.DeployJob{Action: "deploy", DeploymentID: deployment.ID, ProjectID: project.ID}); err != nil {
		_, _ = s.db.Exec(ctx, `UPDATE deployments SET status='failed', error=$1, finished_at=NOW() WHERE id=$2`, "queue unavailable", deployment.ID)
		return model.Deployment{}, err
	}
	return deployment, nil
}

func (s *Server) deploymentForUser(r *http.Request, deploymentID string) (model.Deployment, error) {
	user := currentUser(r)
	return scanDeployment(s.db.QueryRow(r.Context(), deploymentSelect+`
		JOIN projects p ON p.id=d.project_id WHERE d.id=$1 AND p.user_id=$2`, deploymentID, user.ID))
}

func (s *Server) logsAfter(ctx context.Context, deploymentID string, after int64) ([]model.DeploymentLog, error) {
	rows, err := s.db.Query(ctx, `SELECT id,deployment_id,created_at,stream,message FROM deployment_logs WHERE deployment_id=$1 AND id>$2 ORDER BY id LIMIT 1000`, deploymentID, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	logs := make([]model.DeploymentLog, 0)
	for rows.Next() {
		var entry model.DeploymentLog
		if err := rows.Scan(&entry.ID, &entry.DeploymentID, &entry.CreatedAt, &entry.Stream, &entry.Message); err != nil {
			return nil, err
		}
		logs = append(logs, entry)
	}
	return logs, rows.Err()
}

const deploymentColumns = `id,project_id,commit_sha,commit_message,status,image_tag,container_name,url,trigger,error,created_at,started_at,finished_at`
const deploymentSelect = `SELECT d.id,d.project_id,d.commit_sha,d.commit_message,d.status,d.image_tag,d.container_name,d.url,d.trigger,d.error,d.created_at,d.started_at,d.finished_at FROM deployments d`

func scanDeployment(row rowScanner) (model.Deployment, error) {
	var deployment model.Deployment
	err := row.Scan(deploymentPointers(&deployment)...)
	return deployment, err
}

func deploymentPointers(deployment *model.Deployment) []any {
	return []any{&deployment.ID, &deployment.ProjectID, &deployment.CommitSHA, &deployment.CommitMessage, &deployment.Status,
		&deployment.ImageTag, &deployment.ContainerName, &deployment.URL, &deployment.Trigger, &deployment.Error,
		&deployment.CreatedAt, &deployment.StartedAt, &deployment.FinishedAt}
}
