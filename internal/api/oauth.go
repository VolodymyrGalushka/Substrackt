package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuthConfig describes one loopback OAuth authorization-code flow. The
// redirect URI is always http://127.0.0.1:<random port>/<RedirectPath>.
type OAuthConfig struct {
	AuthorizeURL string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Scopes       []string
	// RedirectPath defaults to /callback.
	RedirectPath string
	// UsePKCE enables S256 PKCE.
	UsePKCE bool
}

// Token is an OAuth token response.
type Token struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	Expiry       time.Time
}

// Authorize runs the loopback flow, opening the authorization URL with open.
// The callback server only listens on 127.0.0.1.
func Authorize(ctx context.Context, cfg OAuthConfig, open func(string) error) (Token, error) {
	if open == nil {
		return Token{}, errors.New("an URL opener is required")
	}
	if cfg.RedirectPath == "" {
		cfg.RedirectPath = "/callback"
	}
	if !strings.HasPrefix(cfg.RedirectPath, "/") {
		cfg.RedirectPath = "/" + cfg.RedirectPath
	}
	if err := validateOAuthURL(cfg.AuthorizeURL, "authorization"); err != nil {
		return Token{}, err
	}
	if err := validateOAuthURL(cfg.TokenURL, "token"); err != nil {
		return Token{}, err
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return Token{}, errors.New("OAuth client ID is required")
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Token{}, fmt.Errorf("start OAuth callback listener: %w", err)
	}
	redirectURI := "http://" + listener.Addr().String() + cfg.RedirectPath

	state, err := randomToken(32)
	if err != nil {
		_ = listener.Close()
		return Token{}, err
	}
	verifier, challenge := "", ""
	if cfg.UsePKCE {
		verifier, err = randomToken(48)
		if err != nil {
			_ = listener.Close()
			return Token{}, err
		}
		sum := sha256.Sum256([]byte(verifier))
		challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	}

	type callback struct {
		code string
		err  error
	}
	result := make(chan callback, 1)
	mux := http.NewServeMux()
	mux.HandleFunc(cfg.RedirectPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Query().Get("state") != state {
			result <- callback{err: errors.New("OAuth state did not match")}
			http.Error(w, "state mismatch", http.StatusBadRequest)
			return
		}
		if oauthErr := r.URL.Query().Get("error"); oauthErr != "" {
			description := r.URL.Query().Get("error_description")
			if description != "" {
				oauthErr += ": " + description
			}
			result <- callback{err: fmt.Errorf("OAuth authorization failed: %s", oauthErr)}
			http.Error(w, "authorization failed", http.StatusBadRequest)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			result <- callback{err: errors.New("OAuth callback did not include a code")}
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		result <- callback{code: code}
		_, _ = io.WriteString(w, "<html><body><h3>Sign-in complete</h3><p>You can close this tab and return to Substrackt.</p></body></html>")
	})

	server := &http.Server{Handler: mux}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	authorizeURL, err := url.Parse(cfg.AuthorizeURL)
	if err != nil {
		return Token{}, fmt.Errorf("parse OAuth authorization URL: %w", err)
	}
	query := authorizeURL.Query()
	query.Set("response_type", "code")
	query.Set("client_id", cfg.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("state", state)
	if len(cfg.Scopes) > 0 {
		query.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	if cfg.UsePKCE {
		query.Set("code_challenge", challenge)
		query.Set("code_challenge_method", "S256")
	}
	authorizeURL.RawQuery = query.Encode()
	if err := open(authorizeURL.String()); err != nil {
		return Token{}, fmt.Errorf("open OAuth authorization page: %w", err)
	}

	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return Token{}, ctx.Err()
	case <-timer.C:
		return Token{}, errors.New("OAuth sign-in timed out")
	case callback := <-result:
		if callback.err != nil {
			return Token{}, callback.err
		}
		return exchangeCode(ctx, cfg, callback.code, redirectURI, verifier)
	}
}

// Refresh exchanges a refresh token for a new access token.
func Refresh(ctx context.Context, cfg OAuthConfig, refreshToken string) (Token, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return Token{}, errors.New("refresh token is required")
	}
	if err := validateOAuthURL(cfg.TokenURL, "token"); err != nil {
		return Token{}, err
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {cfg.ClientID},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	return requestToken(ctx, cfg.TokenURL, form)
}

func exchangeCode(ctx context.Context, cfg OAuthConfig, code, redirectURI, verifier string) (Token, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {redirectURI},
		"client_id":    {cfg.ClientID},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return requestToken(ctx, cfg.TokenURL, form)
}

func requestToken(ctx context.Context, endpoint string, form url.Values) (Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return Token{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Substrackt/0.1")
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return Token{}, fmt.Errorf("OAuth token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Token{}, fmt.Errorf("read OAuth token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Token{}, fmt.Errorf("OAuth token endpoint returned HTTP %d: %s", resp.StatusCode, apiError(body))
	}
	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return Token{}, fmt.Errorf("decode OAuth token response: %w", err)
	}
	if payload.AccessToken == "" {
		return Token{}, errors.New("OAuth token response did not include an access token")
	}
	token := Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
	}
	if payload.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return token, nil
}

func validateOAuthURL(raw, label string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("OAuth %s URL is not valid", label)
	}
	if parsed.Scheme != "https" && !isLoopbackHost(parsed.Hostname()) {
		return fmt.Errorf("OAuth %s URL must use https", label)
	}
	return nil
}

func randomToken(bytes int) (string, error) {
	buffer := make([]byte, bytes)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
