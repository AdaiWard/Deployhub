package api

import (
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/deployhub-io/deployhub/internal/model"
	"github.com/google/uuid"
)

var slugPattern = regexp.MustCompile(`[^a-z0-9]+`)
var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

type projectInput struct {
	Name            string  `json:"name"`
	RepoURL         string  `json:"repo_url"`
	Branch          string  `json:"branch"`
	RootDirectory   string  `json:"root_directory"`
	Runtime         string  `json:"runtime"`
	DockerfilePath  string  `json:"dockerfile_path"`
	Port            int     `json:"port"`
	HealthcheckPath string  `json:"healthcheck_path"`
	Subdomain       string  `json:"subdomain"`
	AutoDeploy      *bool   `json:"auto_deploy"`
	MemoryMB        int     `json:"memory_mb"`
	CPU             float64 `json:"cpu"`
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	rows, err := s.db.Query(r.Context(), projectSelect+` WHERE p.user_id = $1 ORDER BY p.updated_at DESC`, user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load projects")
		return
	}
	defer rows.Close()
	projects := make([]model.Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not read projects")
			return
		}
		projects = append(projects, project)
	}
	writeJSON(w, http.StatusOK, projects)
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var input projectInput
	if !decodeJSON(w, r, &input) {
		return
	}
	input.applyDefaults()
	repoFullName, repoURL, err := parseGitHubRepository(input.RepoURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := input.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = path.Base(repoFullName)
	}
	slug := slugify(name)
	if input.Subdomain == "" {
		input.Subdomain = slug
	}
	user := currentUser(r)
	project := model.Project{ID: uuid.NewString()}
	autoDeploy := true
	if input.AutoDeploy != nil {
		autoDeploy = *input.AutoDeploy
	}
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO projects (
			id, user_id, name, slug, repo_full_name, repo_url, branch, root_directory,
			runtime, dockerfile_path, port, healthcheck_path, subdomain, auto_deploy, memory_mb, cpu
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING `+projectColumns,
		project.ID, user.ID, name, slug, repoFullName, repoURL, input.Branch, cleanRelativePath(input.RootDirectory),
		input.Runtime, cleanRelativePath(input.DockerfilePath), input.Port, input.HealthcheckPath,
		slugify(input.Subdomain), autoDeploy, input.MemoryMB, input.CPU).Scan(projectPointers(&project)...)
	if err != nil {
		if strings.Contains(err.Error(), "unique") {
			writeError(w, http.StatusConflict, "project name or subdomain is already used")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create project")
		return
	}
	if err := s.createRepositoryWebhook(r, project); err != nil {
		s.logger.Warn("could not create GitHub webhook", "project_id", project.ID, "error", err)
	}
	writeJSON(w, http.StatusCreated, project)
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	var input projectInput
	if !decodeJSON(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Name) != "" {
		project.Name = strings.TrimSpace(input.Name)
		project.Slug = slugify(project.Name)
	}
	if strings.TrimSpace(input.Branch) != "" {
		project.Branch = strings.TrimSpace(input.Branch)
	}
	if input.Port > 0 {
		project.Port = input.Port
	}
	if input.MemoryMB > 0 {
		project.MemoryMB = input.MemoryMB
	}
	if input.CPU > 0 {
		project.CPU = input.CPU
	}
	if input.Runtime != "" {
		project.Runtime = input.Runtime
	}
	if input.HealthcheckPath != "" {
		project.HealthcheckPath = input.HealthcheckPath
	}
	if input.Subdomain != "" {
		project.Subdomain = slugify(input.Subdomain)
	}
	project.RootDirectory = cleanRelativePath(input.RootDirectory)
	project.DockerfilePath = cleanRelativePath(input.DockerfilePath)
	if input.AutoDeploy != nil {
		project.AutoDeploy = *input.AutoDeploy
	}
	if project.Port < 1 || project.Port > 65535 || project.MemoryMB < 64 || project.MemoryMB > 8192 || project.CPU <= 0 || project.CPU > 8 {
		writeError(w, http.StatusBadRequest, "invalid resource or port limits")
		return
	}
	err = s.db.QueryRow(r.Context(), `
		UPDATE projects SET name=$1, slug=$2, branch=$3, root_directory=$4, runtime=$5,
			dockerfile_path=$6, port=$7, healthcheck_path=$8, subdomain=$9, auto_deploy=$10,
			memory_mb=$11, cpu=$12, updated_at=NOW()
		WHERE id=$13 AND user_id=$14 RETURNING `+projectColumns,
		project.Name, project.Slug, project.Branch, project.RootDirectory, project.Runtime,
		project.DockerfilePath, project.Port, project.HealthcheckPath, project.Subdomain,
		project.AutoDeploy, project.MemoryMB, project.CPU, project.ID, project.UserID).Scan(projectPointers(&project)...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update project")
		return
	}
	writeJSON(w, http.StatusOK, project)
}

func (s *Server) getEnvironment(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT key, value_encrypted FROM environment_variables WHERE project_id=$1 ORDER BY key`, project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load environment")
		return
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var key, encrypted string
		if err := rows.Scan(&key, &encrypted); err != nil {
			writeError(w, http.StatusInternalServerError, "could not read environment")
			return
		}
		value, err := s.box.Decrypt(encrypted)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not decrypt environment")
			return
		}
		values[key] = value
	}
	writeJSON(w, http.StatusOK, values)
}

func (s *Server) putEnvironment(w http.ResponseWriter, r *http.Request) {
	project, err := s.projectForUser(r, r.PathValue("projectID"))
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	var values map[string]string
	if !decodeJSON(w, r, &values) {
		return
	}
	if len(values) > 100 {
		writeError(w, http.StatusBadRequest, "too many environment variables")
		return
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if !envKeyPattern.MatchString(key) {
			writeError(w, http.StatusBadRequest, "invalid environment variable name: "+key)
			return
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save environment")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM environment_variables WHERE project_id=$1`, project.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save environment")
		return
	}
	for _, key := range keys {
		encrypted, err := s.box.Encrypt(values[key])
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not encrypt environment")
			return
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO environment_variables (project_id, key, value_encrypted) VALUES ($1,$2,$3)`, project.ID, key, encrypted); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save environment")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save environment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"count": len(values)})
}

func (s *Server) projectForUser(r *http.Request, projectID string) (model.Project, error) {
	user := currentUser(r)
	return scanProject(s.db.QueryRow(r.Context(), projectSelect+` WHERE p.id=$1 AND p.user_id=$2`, projectID, user.ID))
}

const projectColumns = `id,user_id,name,slug,repo_full_name,repo_url,branch,root_directory,runtime,dockerfile_path,port,healthcheck_path,subdomain,auto_deploy,memory_mb,cpu,created_at,updated_at`
const projectSelect = `SELECT ` + projectColumns + ` FROM projects p`

type rowScanner interface{ Scan(...any) error }

func scanProject(row rowScanner) (model.Project, error) {
	var project model.Project
	err := row.Scan(projectPointers(&project)...)
	return project, err
}

func projectPointers(project *model.Project) []any {
	return []any{&project.ID, &project.UserID, &project.Name, &project.Slug, &project.RepoFullName,
		&project.RepoURL, &project.Branch, &project.RootDirectory, &project.Runtime, &project.DockerfilePath,
		&project.Port, &project.HealthcheckPath, &project.Subdomain, &project.AutoDeploy, &project.MemoryMB,
		&project.CPU, &project.CreatedAt, &project.UpdatedAt}
}

func (input *projectInput) applyDefaults() {
	if strings.TrimSpace(input.Branch) == "" {
		input.Branch = "main"
	}
	if strings.TrimSpace(input.Runtime) == "" {
		input.Runtime = "auto"
	}
	if input.Port == 0 {
		input.Port = 3000
	}
	if strings.TrimSpace(input.HealthcheckPath) == "" {
		input.HealthcheckPath = "/"
	}
	if input.MemoryMB == 0 {
		input.MemoryMB = 512
	}
	if input.CPU == 0 {
		input.CPU = 0.5
	}
}

func (input projectInput) validate() error {
	if input.Port < 1 || input.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if input.MemoryMB < 64 || input.MemoryMB > 8192 {
		return fmt.Errorf("memory limit must be between 64 and 8192 MB")
	}
	if input.CPU <= 0 || input.CPU > 8 {
		return fmt.Errorf("CPU limit must be between 0 and 8")
	}
	if !strings.HasPrefix(input.HealthcheckPath, "/") {
		return fmt.Errorf("health check path must start with /")
	}
	allowed := map[string]bool{"auto": true, "node": true, "python": true, "java": true, "dockerfile": true}
	if !allowed[input.Runtime] {
		return fmt.Errorf("unsupported runtime")
	}
	if slugify(input.Subdomain) == "" && input.Subdomain != "" {
		return fmt.Errorf("invalid subdomain")
	}
	return nil
}

func parseGitHubRepository(value string) (string, string, error) {
	value = strings.TrimSpace(strings.TrimSuffix(value, ".git"))
	if value == "" {
		return "", "", fmt.Errorf("GitHub repository is required")
	}
	if !strings.Contains(value, "://") {
		parts := strings.Split(strings.Trim(value, "/"), "/")
		if len(parts) != 2 {
			return "", "", fmt.Errorf("use owner/repository or a GitHub URL")
		}
		fullName := parts[0] + "/" + parts[1]
		return fullName, "https://github.com/" + fullName + ".git", nil
	}
	parsed, err := url.Parse(value)
	if err != nil || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return "", "", fmt.Errorf("only GitHub repositories are supported")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid GitHub repository URL")
	}
	fullName := parts[0] + "/" + parts[1]
	return fullName, "https://github.com/" + fullName + ".git", nil
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = slugPattern.ReplaceAllString(value, "-")
	return strings.Trim(value, "-")
}

func cleanRelativePath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(path.Clean("/"+value), "/")
	if value == "." {
		return ""
	}
	return value
}
