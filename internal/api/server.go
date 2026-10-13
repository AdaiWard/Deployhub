package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/deployhub-io/deployhub/internal/config"
	"github.com/deployhub-io/deployhub/internal/model"
	"github.com/deployhub-io/deployhub/internal/queue"
	"github.com/deployhub-io/deployhub/internal/secretbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

type contextKey string

const userContextKey contextKey = "user"

type Server struct {
	cfg        config.Config
	db         *pgxpool.Pool
	queue      *queue.Queue
	box        *secretbox.Box
	logger     *slog.Logger
	httpClient *http.Client
}

func New(cfg config.Config, db *pgxpool.Pool, jobs *queue.Queue, box *secretbox.Box, logger *slog.Logger) (*Server, error) {
	s := &Server{
		cfg: cfg, db: db, queue: jobs, box: box, logger: logger,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
	if cfg.DevMode {
		_, err := db.Exec(context.Background(), `
			INSERT INTO users (id, login, name)
			VALUES ('dev-user', 'local-developer', 'Local Developer')
			ON CONFLICT (id) DO NOTHING`)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *Server) Handler() http.Handler {
	public := http.NewServeMux()
	public.HandleFunc("GET /api/health", s.health)
	public.HandleFunc("GET /auth/github", s.githubLogin)
	public.HandleFunc("GET /auth/github/callback", s.githubCallback)
	public.HandleFunc("POST /auth/logout", s.logout)
	public.HandleFunc("POST /webhooks/github", s.githubWebhook)

	private := http.NewServeMux()
	private.HandleFunc("GET /api/me", s.me)
	private.HandleFunc("GET /api/github/repositories", s.githubRepositories)
	private.HandleFunc("GET /api/projects", s.listProjects)
	private.HandleFunc("POST /api/projects", s.createProject)
	private.HandleFunc("GET /api/projects/{projectID}", s.getProject)
	private.HandleFunc("PATCH /api/projects/{projectID}", s.updateProject)
	private.HandleFunc("GET /api/projects/{projectID}/environment", s.getEnvironment)
	private.HandleFunc("PUT /api/projects/{projectID}/environment", s.putEnvironment)
	private.HandleFunc("GET /api/projects/{projectID}/deployments", s.listDeployments)
	private.HandleFunc("POST /api/projects/{projectID}/deployments", s.createDeployment)
	private.HandleFunc("POST /api/projects/{projectID}/restart", s.restartProject)
	private.HandleFunc("GET /api/projects/{projectID}/metrics", s.projectMetrics)
	private.HandleFunc("GET /api/deployments/{deploymentID}", s.getDeployment)
	private.HandleFunc("GET /api/deployments/{deploymentID}/logs", s.getDeploymentLogs)
	private.HandleFunc("GET /api/deployments/{deploymentID}/events", s.deploymentEvents)
	private.HandleFunc("POST /api/deployments/{deploymentID}/rollback", s.rollbackDeployment)

	root := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/health" {
			s.auth(private).ServeHTTP(w, r)
			return
		}
		public.ServeHTTP(w, r)
	})
	return s.recoverPanic(s.cors(s.accessLog(root)))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	status := map[string]any{"status": "ok", "postgres": "ok", "redis": "ok", "time": time.Now().UTC()}
	code := http.StatusOK
	if err := s.db.Ping(ctx); err != nil {
		status["postgres"] = "unavailable"
		status["status"] = "degraded"
		code = http.StatusServiceUnavailable
	}
	if err := s.queue.Client().Ping(ctx).Err(); err != nil {
		status["redis"] = "unavailable"
		status["status"] = "degraded"
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, currentUser(r))
}

func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.DevMode {
			user := model.User{ID: "dev-user", Login: "local-developer", Name: "Local Developer"}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
			return
		}
		cookie, err := r.Cookie("deployhub_session")
		if err != nil || cookie.Value == "" {
			writeError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		var user model.User
		err = s.db.QueryRow(r.Context(), `
			SELECT u.id, COALESCE(u.github_id, 0), u.login, u.name, u.avatar_url, u.created_at
			FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.id = $1 AND s.expires_at > NOW()`, cookie.Value).
			Scan(&user.ID, &user.GitHubID, &user.Login, &user.Name, &user.AvatarURL, &user.CreatedAt)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)))
	})
}

func currentUser(r *http.Request) model.User {
	user, _ := r.Context().Value(userContextKey).(model.User)
	return user
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && s.originAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(origin string) bool {
	if origin == s.cfg.ExternalURL || (s.cfg.DevMode && (origin == "http://localhost:4173" || origin == "http://127.0.0.1:4173")) {
		return true
	}
	for _, allowed := range s.cfg.AllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "error", recovered, "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration_ms", time.Since(started).Milliseconds())
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
