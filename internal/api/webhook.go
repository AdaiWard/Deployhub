package api

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/deployhub-io/deployhub/internal/model"
)

type pushPayload struct {
	Ref        string `json:"ref"`
	After      string `json:"after"`
	Deleted    bool   `json:"deleted"`
	HeadCommit *struct {
		Message string `json:"message"`
	} `json:"head_commit"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (s *Server) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GitHubWebhookSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "GitHub webhook secret is not configured")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid webhook body")
		return
	}
	if !validGitHubSignature(body, r.Header.Get("X-Hub-Signature-256"), s.cfg.GitHubWebhookSecret) {
		writeError(w, http.StatusUnauthorized, "invalid webhook signature")
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	event := r.Header.Get("X-GitHub-Event")
	if deliveryID == "" {
		writeError(w, http.StatusBadRequest, "missing delivery identifier")
		return
	}
	result, err := s.db.Exec(r.Context(), `INSERT INTO webhook_deliveries (id,event) VALUES ($1,$2) ON CONFLICT DO NOTHING`, deliveryID, event)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not record webhook")
		return
	}
	if result.RowsAffected() == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	if event != "push" {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid push payload")
		return
	}
	if payload.Deleted || payload.Repository.FullName == "" || payload.After == "" {
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
	rows, err := s.db.Query(r.Context(), projectSelect+` WHERE LOWER(p.repo_full_name)=LOWER($1) AND p.branch=$2 AND p.auto_deploy=TRUE`, payload.Repository.FullName, branch)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not match webhook projects")
		return
	}
	defer rows.Close()
	message := ""
	if payload.HeadCommit != nil {
		message = strings.Split(payload.HeadCommit.Message, "\n")[0]
	}
	queued := 0
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			continue
		}
		if _, err := s.enqueueDeployment(r.Context(), project, payload.After, message, "webhook"); err == nil {
			queued++
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "queued": queued})
}

func validGitHubSignature(body []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

func (s *Server) createRepositoryWebhook(r *http.Request, project model.Project) error {
	if s.cfg.GitHubWebhookSecret == "" || !strings.HasPrefix(s.cfg.ExternalURL, "https://") {
		return nil
	}
	user := currentUser(r)
	var encrypted string
	if err := s.db.QueryRow(r.Context(), `SELECT access_token_encrypted FROM users WHERE id=$1`, user.ID).Scan(&encrypted); err != nil || encrypted == "" {
		return nil
	}
	token, err := s.box.Decrypt(encrypted)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]any{
		"name":   "web",
		"active": true,
		"events": []string{"push"},
		"config": map[string]string{
			"url":          s.cfg.ExternalURL + "/webhooks/github",
			"content_type": "json",
			"secret":       s.cfg.GitHubWebhookSecret,
			"insecure_ssl": "0",
		},
	})
	endpoint := "https://api.github.com/repos/" + strings.Trim(project.RepoFullName, "/") + "/hooks"
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint, bytes.NewReader(body))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("Content-Type", "application/json")
	response, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusUnprocessableEntity {
		responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("create GitHub webhook: status %d: %s", response.StatusCode, responseBody)
	}
	if response.StatusCode == http.StatusCreated {
		var result struct {
			ID int64 `json:"id"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err == nil && result.ID != 0 {
			_, _ = s.db.Exec(r.Context(), `UPDATE projects SET github_webhook_id=$1 WHERE id=$2`, result.ID, project.ID)
		}
	}
	return nil
}
