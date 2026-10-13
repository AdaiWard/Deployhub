package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

type githubUserResponse struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

func (s *Server) githubLogin(w http.ResponseWriter, r *http.Request) {
	if s.cfg.GitHubClientID == "" || s.cfg.GitHubClientSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "GitHub OAuth is not configured")
		return
	}
	state, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start authentication")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "github_oauth_state", Value: state, Path: "/auth/github/callback",
		HttpOnly: true, Secure: strings.HasPrefix(s.cfg.ExternalURL, "https://"), SameSite: http.SameSiteLaxMode,
		MaxAge: 600,
	})
	query := url.Values{
		"client_id":    {s.cfg.GitHubClientID},
		"redirect_uri": {s.cfg.ExternalURL + "/auth/github/callback"},
		"scope":        {"read:user repo admin:repo_hook"},
		"state":        {state},
	}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+query.Encode(), http.StatusFound)
}

func (s *Server) githubCallback(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("github_oauth_state")
	if err != nil || stateCookie.Value == "" || stateCookie.Value != r.URL.Query().Get("state") {
		writeError(w, http.StatusBadRequest, "invalid OAuth state")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "missing OAuth code")
		return
	}

	form := url.Values{
		"client_id":     {s.cfg.GitHubClientID},
		"client_secret": {s.cfg.GitHubClientSecret},
		"code":          {code},
		"redirect_uri":  {s.cfg.ExternalURL + "/auth/github/callback"},
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(form.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := s.httpClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "GitHub token exchange failed")
		return
	}
	defer response.Body.Close()
	var token githubTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&token); err != nil || token.AccessToken == "" {
		writeError(w, http.StatusBadGateway, "GitHub rejected the OAuth request")
		return
	}

	userRequest, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user", nil)
	userRequest.Header.Set("Accept", "application/vnd.github+json")
	userRequest.Header.Set("Authorization", "Bearer "+token.AccessToken)
	userRequest.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	userResponse, err := s.httpClient.Do(userRequest)
	if err != nil {
		writeError(w, http.StatusBadGateway, "GitHub user lookup failed")
		return
	}
	defer userResponse.Body.Close()
	var githubUser githubUserResponse
	if err := json.NewDecoder(userResponse.Body).Decode(&githubUser); err != nil || githubUser.ID == 0 {
		writeError(w, http.StatusBadGateway, "GitHub user lookup failed")
		return
	}

	encryptedToken, err := s.box.Encrypt(token.AccessToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not store GitHub token")
		return
	}
	userID := "gh-" + strconv.FormatInt(githubUser.ID, 10)
	_, err = s.db.Exec(r.Context(), `
		INSERT INTO users (id, github_id, login, name, avatar_url, access_token_encrypted)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (github_id) DO UPDATE SET
			login = EXCLUDED.login, name = EXCLUDED.name, avatar_url = EXCLUDED.avatar_url,
			access_token_encrypted = EXCLUDED.access_token_encrypted, updated_at = NOW()`,
		userID, githubUser.ID, githubUser.Login, githubUser.Name, githubUser.AvatarURL, encryptedToken)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not save GitHub user")
		return
	}

	sessionID, err := randomToken(40)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	expiresAt := time.Now().Add(30 * 24 * time.Hour)
	if _, err := s.db.Exec(r.Context(), `INSERT INTO sessions (id, user_id, expires_at) VALUES ($1, $2, $3)`, sessionID, userID, expiresAt); err != nil {
		writeError(w, http.StatusInternalServerError, "could not create session")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "deployhub_session", Value: sessionID, Path: "/", Expires: expiresAt,
		HttpOnly: true, Secure: strings.HasPrefix(s.cfg.ExternalURL, "https://"), SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{Name: "github_oauth_state", Value: "", Path: "/auth/github/callback", MaxAge: -1, HttpOnly: true})
	http.Redirect(w, r, s.cfg.ExternalURL, http.StatusFound)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("deployhub_session"); err == nil {
		_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE id = $1`, cookie.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: "deployhub_session", Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) githubRepositories(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	var encrypted string
	if err := s.db.QueryRow(r.Context(), `SELECT access_token_encrypted FROM users WHERE id = $1`, user.ID).Scan(&encrypted); err != nil || encrypted == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	token, err := s.box.Decrypt(encrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read GitHub credentials")
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "https://api.github.com/user/repos?per_page=100&sort=updated&affiliation=owner,collaborator,organization_member", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	response, err := s.httpClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "GitHub repositories are unavailable")
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "GitHub repositories are unavailable")
		return
	}
	var repositories []struct {
		ID            int64  `json:"id"`
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		CloneURL      string `json:"clone_url"`
		DefaultBranch string `json:"default_branch"`
		Language      string `json:"language"`
	}
	if err := json.NewDecoder(response.Body).Decode(&repositories); err != nil {
		writeError(w, http.StatusBadGateway, "invalid GitHub response")
		return
	}
	writeJSON(w, http.StatusOK, repositories)
}

func randomToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("random token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
