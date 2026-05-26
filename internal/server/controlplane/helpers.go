package controlplane

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
