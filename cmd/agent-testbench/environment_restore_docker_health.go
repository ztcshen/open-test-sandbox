package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

func waitEnvironmentRestoreHealthChecks(ctx context.Context, checks []any, timeout time.Duration, workspace string, composeBaseArgs []string) []environmentRestoreHealthCheckReport {
	out := make([]environmentRestoreHealthCheckReport, 0, len(checks))
	for _, raw := range checks {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		kind := strings.TrimSpace(valueString(item["kind"]))
		if kind == "" && strings.TrimSpace(valueString(item["url"])) != "" {
			kind = "url"
		}
		check := environmentRestoreHealthCheckReport{
			ID:        strings.TrimSpace(valueString(item["id"])),
			Kind:      kind,
			URL:       strings.TrimSpace(valueString(item["url"])),
			Address:   strings.TrimSpace(valueString(item["address"])),
			Command:   strings.TrimSpace(valueString(item["command"])),
			Service:   strings.TrimSpace(valueString(item["service"])),
			Container: strings.TrimSpace(valueString(item["container"])),
		}
		switch check.Kind {
		case "url", "":
			if check.URL == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreURLHealthCheck(ctx, check, timeout))
		case "tcp":
			if check.Address == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreTCPHealthCheck(ctx, check, timeout))
		case "command":
			if check.Command == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreCommandHealthCheck(ctx, check, timeout, workspace))
		case "compose-service":
			if check.Service == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreComposeServiceHealthCheck(ctx, check, timeout, workspace, composeBaseArgs))
		case "container":
			if check.Container == "" {
				continue
			}
			out = append(out, waitEnvironmentRestoreContainerHealthCheck(ctx, check, timeout))
		default:
			check.Error = "unsupported health check kind: " + check.Kind
			out = append(out, check)
		}
	}
	return out
}

func waitEnvironmentRestoreURLHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthCheckReport {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(timeout)
	progress := newEnvironmentRestoreHealthProgress(ctx, check, timeout)
	progress.start()
	var lastErr string
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, check.URL, nil)
		if err != nil {
			check.Error = err.Error()
			progress.done(false, check.Error)
			return check
		}
		resp, err := client.Do(req)
		if err == nil {
			check.StatusCode = resp.StatusCode
			if closeErr := resp.Body.Close(); closeErr != nil {
				lastErr = closeErr.Error()
				check.Error = lastErr
				progress.done(false, check.Error)
				return check
			}
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				check.OK = true
				check.Error = ""
				progress.done(true, fmt.Sprintf("HTTP %d", resp.StatusCode))
				return check
			}
			lastErr = fmt.Sprintf("health check returned HTTP %d", resp.StatusCode)
		} else {
			lastErr = err.Error()
		}
		var keepWaiting bool
		check, keepWaiting = waitEnvironmentRestoreHealthPoll(ctx, check, &progress, deadline, lastErr)
		if !keepWaiting {
			return check
		}
	}
}

func waitEnvironmentRestoreTCPHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthCheckReport {
	deadline := time.Now().Add(timeout)
	progress := newEnvironmentRestoreHealthProgress(ctx, check, timeout)
	progress.start()
	var lastErr string
	for {
		dialer := net.Dialer{Timeout: 2 * time.Second}
		conn, err := dialer.DialContext(ctx, "tcp", check.Address)
		if err == nil {
			if closeErr := conn.Close(); closeErr != nil {
				check.Error = closeErr.Error()
				progress.done(false, check.Error)
				return check
			}
			check.OK = true
			check.Error = ""
			progress.done(true, "tcp connected")
			return check
		}
		lastErr = err.Error()
		var keepWaiting bool
		check, keepWaiting = waitEnvironmentRestoreHealthPoll(ctx, check, &progress, deadline, lastErr)
		if !keepWaiting {
			return check
		}
	}
}

func waitEnvironmentRestoreHealthPoll(ctx context.Context, check environmentRestoreHealthCheckReport, progress *environmentRestoreHealthProgress, deadline time.Time, lastErr string) (environmentRestoreHealthCheckReport, bool) {
	if time.Now().After(deadline) {
		check.Error = lastErr
		progress.done(false, check.Error)
		return check, false
	}
	progress.waiting(lastErr, deadline)
	select {
	case <-ctx.Done():
		check.Error = ctx.Err().Error()
		progress.done(false, check.Error)
		return check, false
	case <-time.After(250 * time.Millisecond):
		return check, true
	}
}

func waitEnvironmentRestoreCommandHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration, workspace string) environmentRestoreHealthCheckReport {
	return waitEnvironmentRestoreCommand(ctx, check, timeout, workspace, []string{"/bin/sh", "-c", check.Command}, func(check *environmentRestoreHealthCheckReport, output string) bool {
		check.Output = truncateReportText(output, 200)
		return true
	})
}

func waitEnvironmentRestoreComposeServiceHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration, workspace string, composeBaseArgs []string) environmentRestoreHealthCheckReport {
	if len(composeBaseArgs) == 0 {
		check.Error = "compose service health check requires composeFile"
		return check
	}
	command := append(append([]string{"docker", "compose"}, composeBaseArgs...), "ps", "--format", "json", check.Service)
	return waitEnvironmentRestoreCommand(ctx, check, timeout, workspace, command, func(check *environmentRestoreHealthCheckReport, output string) bool {
		check.Output = truncateReportText(output, 200)
		state, health := parseComposeServiceHealth(output)
		check.State = state
		check.Health = health
		return state == "running" && (health == "" || health == "healthy")
	})
}

func waitEnvironmentRestoreContainerHealthCheck(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthCheckReport {
	command := []string{"docker", "inspect", "--format", "{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}", check.Container}
	return waitEnvironmentRestoreCommand(ctx, check, timeout, "", command, func(check *environmentRestoreHealthCheckReport, output string) bool {
		check.Output = truncateReportText(output, 200)
		fields := strings.Fields(output)
		if len(fields) > 0 {
			check.State = strings.TrimSpace(fields[0])
		}
		if len(fields) > 1 {
			check.Health = strings.TrimSpace(fields[1])
		}
		return check.State == "running" && (check.Health == "" || check.Health == "healthy")
	})
}

func waitEnvironmentRestoreCommand(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration, workspace string, command []string, ok func(*environmentRestoreHealthCheckReport, string) bool) environmentRestoreHealthCheckReport {
	deadline := time.Now().Add(timeout)
	progress := newEnvironmentRestoreHealthProgress(ctx, check, timeout)
	progress.start()
	var lastErr string
	for {
		output, errText := runRestoreCommand(ctx, workspace, command)
		if errText == "" && ok(&check, output) {
			check.OK = true
			check.Error = ""
			if check.Output == "" {
				check.Output = truncateReportText(output, 200)
			}
			progress.done(true, "ready")
			return check
		}
		if errText != "" {
			lastErr = errText
		} else {
			lastErr = "health command did not report ready"
		}
		if time.Now().After(deadline) {
			check.Error = lastErr
			if check.Output == "" {
				check.Output = truncateReportText(output, 200)
			}
			progress.done(false, check.Error)
			return check
		}
		progress.waiting(lastErr, deadline)
		select {
		case <-ctx.Done():
			check.Error = ctx.Err().Error()
			progress.done(false, check.Error)
			return check
		case <-time.After(250 * time.Millisecond):
		}
	}
}

type environmentRestoreHealthProgress struct {
	ctx       context.Context
	target    string
	timeout   time.Duration
	lastPrint time.Time
}

func newEnvironmentRestoreHealthProgress(ctx context.Context, check environmentRestoreHealthCheckReport, timeout time.Duration) environmentRestoreHealthProgress {
	return environmentRestoreHealthProgress{
		ctx:     ctx,
		target:  environmentRestoreHealthProgressTarget(check),
		timeout: timeout,
	}
}

func (p *environmentRestoreHealthProgress) start() {
	environmentRestoreProgressf(p.ctx, "restore health checking: %s timeout=%s\n", p.target, p.timeout)
	p.lastPrint = time.Now()
}

func (p *environmentRestoreHealthProgress) waiting(lastErr string, deadline time.Time) {
	if time.Since(p.lastPrint) < 2*time.Second {
		return
	}
	remaining := time.Until(deadline).Round(time.Second)
	if remaining < 0 {
		remaining = 0
	}
	environmentRestoreProgressf(p.ctx, "restore health waiting: %s last=%s remaining=%s\n", p.target, lastErr, remaining)
	p.lastPrint = time.Now()
}

func (p *environmentRestoreHealthProgress) done(ok bool, detail string) {
	state := "failed"
	if ok {
		state = "ok"
	}
	environmentRestoreProgressf(p.ctx, "restore health %s: %s last=%s\n", state, p.target, detail)
}

func environmentRestoreHealthProgressTarget(check environmentRestoreHealthCheckReport) string {
	switch strings.TrimSpace(check.Kind) {
	case "url", "":
		return "url " + check.URL
	case "tcp":
		return "tcp " + check.Address
	case "compose-service":
		return "compose-service " + check.Service
	case "container":
		return "container " + check.Container
	case "command":
		if check.ID != "" {
			return "command " + check.ID
		}
		return "command health check"
	default:
		if check.ID != "" {
			return check.Kind + " " + check.ID
		}
		return check.Kind
	}
}

func parseComposeServiceHealth(output string) (string, string) {
	output = strings.TrimSpace(output)
	if output == "" {
		return "", ""
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(output), &object); err == nil && object != nil {
		return strings.ToLower(valueString(firstNonNil(object["State"], object["state"]))), strings.ToLower(valueString(firstNonNil(object["Health"], object["health"])))
	}
	var array []map[string]any
	if err := json.Unmarshal([]byte(output), &array); err == nil && len(array) > 0 {
		return strings.ToLower(valueString(firstNonNil(array[0]["State"], array[0]["state"]))), strings.ToLower(valueString(firstNonNil(array[0]["Health"], array[0]["health"])))
	}
	lower := strings.ToLower(output)
	state := ""
	health := ""
	if strings.Contains(lower, "running") {
		state = "running"
	}
	if strings.Contains(lower, "unhealthy") {
		health = "unhealthy"
	} else if strings.Contains(lower, "healthy") {
		health = "healthy"
	}
	return state, health
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
