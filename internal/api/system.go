package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime"
	"strconv"
	"time"
)

// readinessTimeout bounds how long a readiness probe may spend checking its
// dependencies. Kubernetes' default probe timeout is 1s, so staying under it
// means the probe fails for the right reason rather than being killed.
const readinessTimeout = 900 * time.Millisecond

// healthResponse is the liveness payload.
type healthResponse struct {
	Status        string `json:"status"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	Goroutines    int    `json:"goroutines"`
	Timestamp     string `json:"timestamp"`
	Version       string `json:"version"`
}

// handleHealth serves GET /api/v1/health, the liveness probe.
//
// It answers from process state only and never touches a dependency, on
// purpose: a liveness probe that fails when the database is down makes the
// orchestrator restart every replica during an outage, turning a partial
// failure into a total one. Dependency state belongs in handleReady.
func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	now := a.clock()
	a.write(w, r, http.StatusOK, healthResponse{
		Status:        "ok",
		UptimeSeconds: int64(now.Sub(a.started).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		Timestamp:     formatTime(now),
		Version:       a.info.Version,
	})
}

// checkResult is one dependency's contribution to readiness.
type checkResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // "ok" | "error"
	DurationMS int64  `json:"durationMs"`
	Detail     string `json:"detail,omitempty"`
}

// readyResponse is the readiness payload.
type readyResponse struct {
	Status string        `json:"status"` // "ok" | "unavailable"
	Checks []checkResult `json:"checks"`
}

// handleReady serves GET /api/v1/health/ready, the readiness probe. It reports
// 503 when any dependency is unhealthy, which takes the instance out of
// rotation without restarting it.
func (a *API) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), readinessTimeout)
	defer cancel()

	checks := []checkResult{a.checkStore(ctx)}

	status, code := "ok", http.StatusOK
	for _, c := range checks {
		if c.Status != "ok" {
			status, code = "unavailable", http.StatusServiceUnavailable
			break
		}
	}
	a.write(w, r, code, readyResponse{Status: status, Checks: checks})
}

// checkStore probes storage with the cheapest operation that can fail.
func (a *API) checkStore(ctx context.Context) checkResult {
	start := time.Now()
	count, err := a.projects.Count(ctx)
	result := checkResult{
		Name:       "database",
		Status:     "ok",
		DurationMS: time.Since(start).Milliseconds(),
	}
	if err != nil {
		result.Status = "error"
		// The cause goes to the log, not to the response: a readiness endpoint
		// is reachable from outside the cluster in most setups.
		result.Detail = "count failed"
		a.log.ErrorContext(ctx, "readiness check failed",
			slog.String("check", result.Name),
			slog.Any("error", err),
		)
		return result
	}
	result.Detail = strconv.Itoa(count) + " projects"
	return result
}

// versionResponse is the build and runtime metadata an operator needs to
// confirm what is actually deployed.
type versionResponse struct {
	Service       string `json:"service"`
	Version       string `json:"version"`
	Commit        string `json:"commit"`
	Date          string `json:"date,omitempty"`
	GoVersion     string `json:"goVersion"`
	Platform      string `json:"platform"`
	Environment   string `json:"environment"`
	Repository    string `json:"repository,omitempty"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
}

// handleVersion serves GET /api/v1/version.
func (a *API) handleVersion(w http.ResponseWriter, r *http.Request) {
	now := a.clock()
	a.write(w, r, http.StatusOK, versionResponse{
		Service:       "splash",
		Version:       a.info.Version,
		Commit:        a.info.ShortCommit(),
		Date:          a.info.Date,
		GoVersion:     a.info.GoVersion,
		Platform:      a.info.Platform,
		Environment:   string(a.env),
		Repository:    a.repoURL,
		UptimeSeconds: int64(now.Sub(a.started).Seconds()),
	})
}
