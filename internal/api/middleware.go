package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"conduit/internal/metrics"
	"conduit/internal/model"
)

// statusRecorder captures the response status code for logging/metrics/audit.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}
func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wrote {
		r.status = http.StatusOK
		r.wrote = true
	}
	return r.ResponseWriter.Write(b)
}
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type reqIDKey int

const requestIDKey reqIDKey = 0

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// observe assigns a request id, records metrics, and emits one structured log
// line per request. It is the outermost middleware so it sees the final status.
func observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The SSE stream is long-lived; don't wrap/measure it like a normal request.
		if r.URL.Path == "/api/events" {
			next.ServeHTTP(w, r)
			return
		}
		rid := newRequestID()
		rec := &statusRecorder{ResponseWriter: w, status: 0}
		rec.Header().Set("X-Request-ID", rid)
		start := time.Now()
		next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), requestIDKey, rid)))
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		dur := time.Since(start)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			metrics.ObserveHTTP(r.Method, statusText(rec.status), dur.Seconds())
			slog.Info("request",
				"id", rid, "method", r.Method, "path", r.URL.Path,
				"status", rec.status, "dur_ms", dur.Milliseconds())
		}
	})
}

func statusText(code int) string {
	switch {
	case code >= 500:
		return "5xx"
	case code >= 400:
		return "4xx"
	case code >= 300:
		return "3xx"
	default:
		return "2xx"
	}
}

// --- rate limiting (token bucket per client IP) ---

type bucket struct {
	tokens float64
	last   time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	clients map[string]*bucket
}

func newRateLimiter(ratePerSec float64, burst int) *rateLimiter {
	return &rateLimiter{rate: ratePerSec, burst: float64(burst), clients: map[string]*bucket{}}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b := rl.clients[key]
	if b == nil {
		b = &bucket{tokens: rl.burst, last: now}
		rl.clients[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) rateLimit(next http.Handler) http.Handler {
	if s.Limiter == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !s.Limiter.allow(clientIP(r)) {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- audit log ---

// audit records mutating API calls (after auth, so the principal is known).
func (s *Server) audit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		isMutation := r.Method != http.MethodGet && r.Method != http.MethodHead &&
			strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/events"
		if !isMutation {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: 0}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		actor := "anonymous"
		if p, ok := r.Context().Value(principalKey).(principal); ok {
			if p.email != "" {
				actor = p.email
			} else {
				actor = "token"
			}
		}
		entry := model.AuditEntry{
			ID: newRequestID(), Time: time.Now(), Actor: actor,
			Method: r.Method, Path: r.URL.Path, Project: projectID(r), Status: rec.status,
		}
		go func() { _ = s.Store.AppendAudit(entry) }() // best-effort
	})
}
