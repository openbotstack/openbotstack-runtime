package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"
)

// ComponentHealth is the health status of a single dependency.
type ComponentHealth struct {
	Status     string            `json:"status"`
	DurationMs int64             `json:"duration_ms"`
	Error      string            `json:"error,omitempty"`
	Details    map[string]string `json:"details,omitempty"`
}

// HealthReport is the full system health report.
type HealthReport struct {
	Status     string                     `json:"status"`
	Version    string                     `json:"version"`
	Commit     string                     `json:"commit"`
	Branch     string                     `json:"branch"`
	BuildTime  string                     `json:"build_time"`
	GoVersion  string                     `json:"go_version"`
	Components map[string]ComponentHealth `json:"components"`
	CheckedAt  time.Time                  `json:"checked_at"`
}

// BuildInfo holds build metadata injected via -ldflags.
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Branch    string `json:"branch"`
	BuildTime string `json:"build_time"`
	GoVersion string `json:"go_version"`
}

// HealthChecker checks a single dependency.
// Implementations MUST be safe for concurrent use.
type HealthChecker interface {
	Name() string
	Check(ctx context.Context) ComponentHealth
}

// RedisHealthChecker checks Redis connectivity.
type RedisHealthChecker struct {
	pingFunc func(ctx context.Context) error
}

// NewRedisHealthChecker creates a health checker for Redis.
func NewRedisHealthChecker(pingFunc func(ctx context.Context) error) *RedisHealthChecker {
	return &RedisHealthChecker{pingFunc: pingFunc}
}

func (c *RedisHealthChecker) Name() string { return "redis" }

func (c *RedisHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()
	err := c.pingFunc(ctx)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return ComponentHealth{
			Status:     "unhealthy",
			DurationMs: durationMs,
			Error:      err.Error(),
		}
	}
	return ComponentHealth{Status: "healthy", DurationMs: durationMs}
}

// ProviderHealthChecker checks LLM provider connectivity via /models endpoint.
type ProviderHealthChecker struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewProviderHealthChecker creates a health checker for an LLM provider.
func NewProviderHealthChecker(baseURL, apiKey string) *ProviderHealthChecker {
	return &ProviderHealthChecker{
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *ProviderHealthChecker) Name() string { return "llm_provider" }

func (c *ProviderHealthChecker) Check(ctx context.Context) ComponentHealth {
	start := time.Now()

	url := c.baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return ComponentHealth{Status: "unhealthy", DurationMs: time.Since(start).Milliseconds(), Error: err.Error()}
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	durationMs := time.Since(start).Milliseconds()
	if err != nil {
		return ComponentHealth{
			Status:     "unhealthy",
			DurationMs: durationMs,
			Error:      fmt.Sprintf("connection failed: %v", err),
		}
	}
	// Drain body to allow connection reuse in the transport pool.
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ComponentHealth{Status: "healthy", DurationMs: durationMs}
	}

	return ComponentHealth{
		Status:     "unhealthy",
		DurationMs: durationMs,
		Error:      fmt.Sprintf("unexpected status: %d", resp.StatusCode),
	}
}

// runHealthChecks executes all checkers concurrently with a timeout.
func runHealthChecks(ctx context.Context, checkers []HealthChecker) map[string]ComponentHealth {
	if len(checkers) == 0 {
		return map[string]ComponentHealth{}
	}

	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var mu sync.Mutex
	results := make(map[string]ComponentHealth, len(checkers))
	var wg sync.WaitGroup

	for _, checker := range checkers {
		wg.Add(1)
		go func(c HealthChecker) {
			defer wg.Done()
			result := c.Check(checkCtx)
			mu.Lock()
			results[c.Name()] = result
			mu.Unlock()
		}(checker)
	}

	wg.Wait()
	return results
}

// aggregateHealthStatus determines overall status from component results.
func aggregateHealthStatus(components map[string]ComponentHealth) string {
	hasUnhealthy := false
	hasDegraded := false
	for _, c := range components {
		switch c.Status {
		case "unhealthy":
			hasUnhealthy = true
		case "degraded":
			hasDegraded = true
		}
	}
	if hasUnhealthy {
		return "unhealthy"
	}
	if hasDegraded {
		return "degraded"
	}
	return "healthy"
}

func (r *Router) handleHealthz(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

func (r *Router) handleReadyz(w http.ResponseWriter, req *http.Request) {
	components := runHealthChecks(req.Context(), r.healthCheckers)
	status := aggregateHealthStatus(components)

	report := HealthReport{
		Status:     status,
		Version:    r.buildInfo.Version,
		Commit:     r.buildInfo.Commit,
		Branch:     r.buildInfo.Branch,
		BuildTime:  r.buildInfo.BuildTime,
		GoVersion:  runtime.Version(),
		Components: components,
		CheckedAt:  time.Now().UTC(),
	}

	// "degraded" returns 200 per spec: system is usable but some components are slow.
	// Only "unhealthy" returns 503.
	code := http.StatusOK
	if status == "unhealthy" {
		code = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(report)
}

func (r *Router) handleVersion(w http.ResponseWriter, req *http.Request) {
	info := BuildInfo{
		Version:   r.buildInfo.Version,
		Commit:    r.buildInfo.Commit,
		Branch:    r.buildInfo.Branch,
		BuildTime: r.buildInfo.BuildTime,
	}
	info.GoVersion = runtime.Version()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}
