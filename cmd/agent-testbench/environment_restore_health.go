package main

import (
	"encoding/json"
	"strings"

	"agent-testbench/internal/store"
)

func environmentRestoreEffectiveHealthChecks(checks []any, compose map[string]any, graph store.EnvironmentComponentGraph) []any {
	set := environmentRestoreHealthCheckSet{
		covered: map[string]bool{},
		seen:    map[string]bool{},
	}
	startedServices := environmentRestoreStartedServices(compose)
	hasServiceAllowList := len(startedServices) > 0
	for _, raw := range checks {
		set.add(raw)
	}
	for _, component := range graph.Components {
		if !environmentRestoreShouldAddComponentHealth(component, startedServices, hasServiceAllowList) {
			continue
		}
		item, errText := environmentRestoreNormalizeComponentHealthCheck(component)
		if errText == "" {
			set.add(item)
		}
	}
	for _, service := range stringSliceFromAny(compose["services"]) {
		if set.covered[service] {
			continue
		}
		set.out = append(set.out, map[string]any{
			"id":      "compose-service-" + safeReportID(service),
			"kind":    "compose-service",
			"service": service,
		})
		set.covered[service] = true
	}
	return set.out
}

type environmentRestoreHealthCheckSet struct {
	out     []any
	covered map[string]bool
	seen    map[string]bool
}

func (s *environmentRestoreHealthCheckSet) add(raw any) {
	item, ok := raw.(map[string]any)
	if !ok {
		s.out = append(s.out, raw)
		return
	}
	if signature := environmentRestoreHealthCheckSignature(item); signature != "" {
		if s.seen[signature] {
			return
		}
		s.seen[signature] = true
	}
	if environmentRestoreHealthCheckCoversService(item) {
		if service := strings.TrimSpace(valueString(item["service"])); service != "" {
			s.covered[service] = true
		}
	}
	s.out = append(s.out, raw)
}

func environmentRestoreStartedServices(compose map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, service := range stringSliceFromAny(compose["services"]) {
		if service = strings.TrimSpace(service); service != "" {
			out[service] = true
		}
	}
	return out
}

func environmentRestoreShouldAddComponentHealth(component store.EnvironmentComponent, startedServices map[string]bool, hasServiceAllowList bool) bool {
	service := strings.TrimSpace(component.ComposeService)
	return !hasServiceAllowList || service == "" || startedServices[service]
}

func environmentRestoreHealthCheckCoversService(item map[string]any) bool {
	kind := strings.TrimSpace(valueString(item["kind"]))
	if kind == "" {
		kind = strings.TrimSpace(valueString(item["type"]))
	}
	return kind == "compose-service" || kind == "url"
}

func environmentRestoreNormalizeComponentHealthCheck(component store.EnvironmentComponent) (map[string]any, string) {
	raw := strings.TrimSpace(component.HealthCheckJSON)
	normalized, errText := environmentRestoreDecodeHealthCheck(raw)
	if errText != "" {
		return nil, errText
	}
	environmentRestoreApplyComponentHealthDefaults(normalized, component)
	kind := environmentRestoreHealthCheckKind(normalized)
	normalized["kind"] = kind
	if environmentRestoreComponentRequiresURLHealth(component) && kind != "url" {
		return nil, strings.TrimSpace(component.Role) + " health check requires url"
	}
	if errText := environmentRestoreValidateHealthCheckKind(normalized, kind, component); errText != "" {
		return nil, errText
	}
	return normalized, ""
}

func environmentRestoreDecodeHealthCheck(raw string) (map[string]any, string) {
	if raw == "" || raw == "{}" {
		return nil, "missing health check"
	}
	var item map[string]any
	if err := json.Unmarshal([]byte(raw), &item); err != nil {
		return nil, "invalid health check JSON: " + err.Error()
	}
	if len(item) == 0 {
		return nil, "missing health check"
	}
	normalized := map[string]any{}
	for key, value := range item {
		normalized[key] = value
	}
	return normalized, ""
}

func environmentRestoreApplyComponentHealthDefaults(normalized map[string]any, component store.EnvironmentComponent) {
	componentID := strings.TrimSpace(component.ComponentID)
	if strings.TrimSpace(valueString(normalized["id"])) == "" && componentID != "" {
		normalized["id"] = "component-" + safeReportID(componentID)
	}
	if componentID != "" {
		normalized["componentId"] = componentID
	}
	if strings.TrimSpace(valueString(normalized["service"])) == "" && strings.TrimSpace(component.ComposeService) != "" {
		normalized["service"] = strings.TrimSpace(component.ComposeService)
	}
}

func environmentRestoreHealthCheckKind(normalized map[string]any) string {
	kind := strings.TrimSpace(valueString(normalized["kind"]))
	if kind == "" {
		kind = strings.TrimSpace(valueString(normalized["type"]))
	}
	if kind == "" && strings.TrimSpace(valueString(normalized["url"])) != "" {
		return "url"
	}
	return kind
}

func environmentRestoreValidateHealthCheckKind(normalized map[string]any, kind string, component store.EnvironmentComponent) string {
	switch kind {
	case "url":
		if strings.TrimSpace(valueString(normalized["url"])) == "" {
			return "url health check requires url"
		}
	case "tcp":
		if strings.TrimSpace(valueString(normalized["address"])) == "" {
			return "tcp health check requires address"
		}
	case "command":
		if strings.TrimSpace(valueString(normalized["command"])) == "" {
			return "command health check requires command"
		}
	case "compose-service":
		if strings.TrimSpace(valueString(normalized["service"])) == "" {
			normalized["service"] = strings.TrimSpace(component.ComposeService)
		}
		if strings.TrimSpace(valueString(normalized["service"])) == "" {
			return "compose-service health check requires service"
		}
	case "container":
		if strings.TrimSpace(valueString(normalized["container"])) == "" {
			return "container health check requires container"
		}
	default:
		if kind == "" {
			return "health check requires kind"
		}
		return "unsupported health check kind: " + kind
	}
	return ""
}

func environmentRestoreComponentRequiresURLHealth(component store.EnvironmentComponent) bool {
	role := strings.TrimSpace(strings.ToLower(component.Role))
	kind := strings.TrimSpace(strings.ToLower(component.Kind))
	return role == "business-service" || kind == "app"
}

func environmentRestoreHealthCheckSignature(item map[string]any) string {
	kind := strings.TrimSpace(valueString(item["kind"]))
	if kind == "" {
		kind = strings.TrimSpace(valueString(item["type"]))
	}
	switch kind {
	case "url":
		return "url:" + strings.TrimSpace(valueString(item["url"]))
	case "tcp":
		return "tcp:" + strings.TrimSpace(valueString(item["address"]))
	case "command":
		return "command:" + strings.TrimSpace(valueString(item["command"]))
	case "compose-service":
		return "compose-service:" + strings.TrimSpace(valueString(item["service"]))
	case "container":
		return "container:" + strings.TrimSpace(valueString(item["container"]))
	default:
		return ""
	}
}
