// OAuth sign-in for Google and GitHub (authorization-code flow, stdlib only).
// Providers are configured from env in main.go; when none are set the UI shows no
// provider buttons. A signed, short-lived state parameter protects against CSRF —
// no server-side session store is needed. On success a user is found or created
// (and linked via the user_identities layer), issued a JWT, and the browser is
// redirected back to the SPA with the token in the URL fragment.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"conduit/internal/model"
)

// OAuthProvider describes one external identity provider.
type OAuthProvider struct {
	Name         string
	ClientID     string
	ClientSecret string
	AuthURL      string
	TokenURL     string
	Scopes       string
	// Profile fetches the provider's stable subject id, email and display name
	// using a freshly-issued access token.
	Profile func(accessToken string) (subject, email, name string, err error)
}

var oauthHTTP = &http.Client{Timeout: 15 * time.Second}

// NewOAuthProviders builds the provider set from credentials. A provider with an
// empty client id is omitted (so it never appears in the UI).
func NewOAuthProviders(googleID, googleSecret, githubID, githubSecret string) map[string]*OAuthProvider {
	m := map[string]*OAuthProvider{}
	if googleID != "" && googleSecret != "" {
		m["google"] = &OAuthProvider{
			Name: "google", ClientID: googleID, ClientSecret: googleSecret,
			AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
			TokenURL: "https://oauth2.googleapis.com/token",
			Scopes:   "openid email profile",
			Profile:  googleProfile,
		}
	}
	if githubID != "" && githubSecret != "" {
		m["github"] = &OAuthProvider{
			Name: "github", ClientID: githubID, ClientSecret: githubSecret,
			AuthURL:  "https://github.com/login/oauth/authorize",
			TokenURL: "https://github.com/login/oauth/access_token",
			Scopes:   "read:user user:email",
			Profile:  githubProfile,
		}
	}
	return m
}

// baseURL returns the public origin for building redirect URIs.
func (s *Server) baseURL(r *http.Request) string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) redirectURI(r *http.Request, provider string) string {
	return s.baseURL(r) + "/api/auth/oauth/" + provider + "/callback"
}

// oauthStart redirects the browser to the provider's authorization page.
func (s *Server) oauthStart(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	p, ok := s.OAuth[name]
	if !ok || p == nil || p.ClientID == "" {
		writeErr(w, http.StatusNotFound, "unknown or unconfigured provider")
		return
	}
	state := s.Auth.SignState(name, 10*time.Minute)
	q := url.Values{}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", s.redirectURI(r, name))
	q.Set("response_type", "code")
	q.Set("scope", p.Scopes)
	q.Set("state", state)
	if name == "google" {
		q.Set("access_type", "online")
		q.Set("prompt", "select_account")
	}
	http.Redirect(w, r, p.AuthURL+"?"+q.Encode(), http.StatusFound)
}

// oauthCallback handles the provider redirect: exchange code → fetch profile →
// find/create the user → issue a JWT → return to the SPA with the token.
func (s *Server) oauthCallback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("provider")
	p, ok := s.OAuth[name]
	if !ok || p == nil {
		writeErr(w, http.StatusNotFound, "unknown provider")
		return
	}
	home := s.baseURL(r) + "/"
	fail := func(msg string) { http.Redirect(w, r, home+"#auth_error="+url.QueryEscape(msg), http.StatusFound) }

	if e := r.URL.Query().Get("error"); e != "" {
		fail(e)
		return
	}
	stateProvider, valid := s.Auth.VerifyState(r.URL.Query().Get("state"))
	if !valid || stateProvider != name {
		fail("invalid or expired sign-in state")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		fail("missing authorization code")
		return
	}

	accessToken, err := exchangeCode(p, code, s.redirectURI(r, name))
	if err != nil {
		fail("token exchange failed")
		return
	}
	subject, email, displayName, err := p.Profile(accessToken)
	if err != nil || subject == "" {
		fail("could not read your " + name + " profile")
		return
	}
	email = strings.ToLower(strings.TrimSpace(email))

	// 1) Known identity → that user.
	u, found, _ := s.Store.GetUserByIdentity(name, subject)
	if !found {
		// 2) Existing account with the same email → link this provider to it.
		if email != "" {
			if eu, ok, _ := s.Store.GetUserByEmail(email); ok {
				u, found = eu, true
			}
		}
		// 3) Brand-new person → create an account (if signup is allowed).
		if !found {
			if !s.AllowSignup {
				fail("sign-up is disabled on this server")
				return
			}
			if email == "" {
				email = subject + "@" + name + ".oauth.local"
			}
			nu, cerr := s.Auth.CreateOAuthUser(email, displayName, model.RoleMember)
			if cerr != nil {
				fail(cerr.Error())
				return
			}
			s.createPersonalProject(nu)
			u = nu
		}
		_ = s.Store.AddUserIdentity(model.UserIdentity{
			Provider: name, Subject: subject, UserID: u.ID, Email: email, CreatedAt: time.Now(),
		})
	}

	token, err := s.Auth.IssueJWT(u)
	if err != nil {
		fail("could not issue a session")
		return
	}
	// Token in the fragment: never sent to a server or written to access logs.
	http.Redirect(w, r, home+"#access_token="+url.QueryEscape(token), http.StatusFound)
}

// exchangeCode swaps an authorization code for an access token.
func exchangeCode(p *OAuthProvider, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", p.ClientID)
	form.Set("client_secret", p.ClientSecret)
	form.Set("code", code)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest(http.MethodPost, p.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // GitHub returns form-encoded otherwise
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint %d", resp.StatusCode)
	}
	var tok struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("no access token (%s)", tok.Error)
	}
	return tok.AccessToken, nil
}

func googleProfile(accessToken string) (subject, email, name string, err error) {
	var info struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err = getJSONAuthed("https://openidconnect.googleapis.com/v1/userinfo", accessToken, &info); err != nil {
		return "", "", "", err
	}
	return info.Sub, info.Email, info.Name, nil
}

func githubProfile(accessToken string) (subject, email, name string, err error) {
	var u struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err = getJSONAuthed("https://api.github.com/user", accessToken, &u); err != nil {
		return "", "", "", err
	}
	subject = strconv.FormatInt(u.ID, 10)
	name = u.Name
	if name == "" {
		name = u.Login
	}
	email = u.Email
	if email == "" {
		// Primary email is a separate, scoped endpoint.
		var emails []struct {
			Email    string `json:"email"`
			Primary  bool   `json:"primary"`
			Verified bool   `json:"verified"`
		}
		if getJSONAuthed("https://api.github.com/user/emails", accessToken, &emails) == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					email = e.Email
					break
				}
			}
			if email == "" {
				for _, e := range emails {
					if e.Verified {
						email = e.Email
						break
					}
				}
			}
		}
	}
	return subject, email, name, nil
}

func getJSONAuthed(endpoint, accessToken string, out any) error {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "conduit-agent")
	resp, err := oauthHTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("profile endpoint %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}
