package sqlstore_test

import (
	"strings"
	"testing"

	"agent-testbench/internal/store/sqlstore"
)

func sqlValuesClause(dialect sqlstore.Dialect, count int) string {
	bindVars := make([]string, count)
	for i := 1; i <= count; i++ {
		bindVars[i-1] = dialect.BindVar(i)
	}
	return "values (" + strings.Join(bindVars, ", ") + ")"
}

func assertSQLContains(t *testing.T, query, context string, fragments ...string) {
	t.Helper()

	for _, fragment := range fragments {
		if !strings.Contains(query, fragment) {
			t.Fatalf("%s did not contain %q:\n%s", context, fragment, query)
		}
	}
}

func assertSQLOmits(t *testing.T, query, context string, fragments ...string) {
	t.Helper()

	for _, fragment := range fragments {
		if fragment == "" {
			continue
		}
		if strings.Contains(query, fragment) {
			t.Fatalf("%s unexpectedly contained %q:\n%s", context, fragment, query)
		}
	}
}
