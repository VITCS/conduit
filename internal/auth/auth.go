// Package auth provides authentication (password login via bcrypt + HS256 JWTs)
// and API tokens, backed by the store. JWTs are signed with a server secret and
// are stateless; API tokens are random strings stored only as SHA-256 hashes.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"conduit/internal/model"
	"conduit/internal/store"

	"golang.org/x/crypto/bcrypt"
)

// ErrInvalidCredentials is returned when a login does not match.
var ErrInvalidCredentials = errors.New("invalid email or password")

// Service performs auth operations against the store.
type Service struct {
	store     store.Store
	jwtSecret []byte
	TokenTTL  time.Duration
}

// New builds an auth service. The JWT secret signs session tokens.
func New(st store.Store, jwtSecret string) *Service {
	if jwtSecret == "" {
		// Ephemeral secret: logins won't survive a restart. Caller should warn.
		b := make([]byte, 32)
		_, _ = rand.Read(b)
		jwtSecret = hex.EncodeToString(b)
	}
	return &Service{store: st, jwtSecret: []byte(jwtSecret), TokenTTL: 24 * time.Hour}
}

func newID(prefix string) string {
	b := make([]byte, 9)
	_, _ = rand.Read(b)
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(b)
}

// CreateUser registers a new user with a bcrypt-hashed password.
func (s *Service) CreateUser(email, name, password string, role model.Role) (model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return model.User{}, errors.New("email and password are required")
	}
	if _, ok, _ := s.store.GetUserByEmail(email); ok {
		return model.User{}, errors.New("a user with that email already exists")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return model.User{}, err
	}
	u := model.User{
		ID: newID("usr"), Email: email, Name: name, Role: role,
		PasswordHash: string(hash), CreatedAt: time.Now(),
	}
	if err := s.store.CreateUser(u); err != nil {
		return model.User{}, err
	}
	return u, nil
}

// Authenticate verifies a password and returns the user.
func (s *Service) Authenticate(email, password string) (model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	u, ok, err := s.store.GetUserByEmail(email)
	if err != nil {
		return model.User{}, err
	}
	if !ok {
		// Still run a hash compare to reduce timing signal.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$10$invalidinvalidinvalidinvalidinvalidinvalidinv"), []byte(password))
		return model.User{}, ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return model.User{}, ErrInvalidCredentials
	}
	return u, nil
}

// EnsureBootstrapAdmin creates an owner account if there are no users yet.
// Returns whether it created one.
func (s *Service) EnsureBootstrapAdmin(email, password string) (bool, error) {
	n, err := s.store.CountUsers()
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	if _, err := s.CreateUser(email, "Administrator", password, model.RoleOwner); err != nil {
		return false, err
	}
	return true, nil
}

// --- JWT (HS256), minimal stdlib implementation ---

type Claims struct {
	Sub   string     `json:"sub"`
	Email string     `json:"email"`
	Role  model.Role `json:"role"`
	Exp   int64      `json:"exp"`
	Iat   int64      `json:"iat"`
}

func b64(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func (s *Service) sign(signingInput string) string {
	m := hmac.New(sha256.New, s.jwtSecret)
	m.Write([]byte(signingInput))
	return b64(m.Sum(nil))
}

// IssueJWT returns a signed token for a user.
func (s *Service) IssueJWT(u model.User) (string, error) {
	header := b64([]byte(`{"alg":"HS256","typ":"JWT"}`))
	now := time.Now()
	claims := Claims{Sub: u.ID, Email: u.Email, Role: u.Role, Iat: now.Unix(), Exp: now.Add(s.TokenTTL).Unix()}
	cb, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := b64(cb)
	return header + "." + payload + "." + s.sign(header+"."+payload), nil
}

// ParseJWT validates a token's signature and expiry and returns its claims.
func (s *Service) ParseJWT(token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, errors.New("malformed token")
	}
	if !hmac.Equal([]byte(parts[2]), []byte(s.sign(parts[0]+"."+parts[1]))) {
		return Claims{}, errors.New("bad signature")
	}
	cb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, err
	}
	var c Claims
	if err := json.Unmarshal(cb, &c); err != nil {
		return Claims{}, err
	}
	if time.Now().Unix() >= c.Exp {
		return Claims{}, errors.New("token expired")
	}
	return c, nil
}

// --- API tokens ---

func hashToken(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken returns the plaintext token (shown once) and its stored metadata.
func (s *Service) CreateAPIToken(name string, role model.Role) (string, model.APIToken, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", model.APIToken{}, err
	}
	plain := "cdt_" + hex.EncodeToString(raw)
	t := model.APIToken{
		ID: newID("tok"), Name: name, Hash: hashToken(plain), Role: role, CreatedAt: time.Now(),
	}
	if err := s.store.CreateAPIToken(t); err != nil {
		return "", model.APIToken{}, err
	}
	return plain, t, nil
}

// LookupAPIToken resolves a plaintext token to its role.
func (s *Service) LookupAPIToken(plain string) (model.APIToken, bool) {
	t, ok, err := s.store.GetAPITokenByHash(hashToken(plain))
	if err != nil || !ok {
		return model.APIToken{}, false
	}
	return t, true
}

func (s *Service) ListAPITokens() ([]model.APIToken, error) { return s.store.ListAPITokens() }
func (s *Service) DeleteAPIToken(id string) error           { return s.store.DeleteAPIToken(id) }
func (s *Service) GetUser(id string) (model.User, bool, error) {
	return s.store.GetUserByID(id)
}

// IsValidRole reports whether r is one of the known roles.
func IsValidRole(r model.Role) bool {
	switch r {
	case model.RoleOwner, model.RoleAdmin, model.RoleMember, model.RoleViewer:
		return true
	}
	return false
}
