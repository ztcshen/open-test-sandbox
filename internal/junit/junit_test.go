package junit

import (
	"strings"
	"testing"
)

func TestRenderEscapesAndCountsJUnitSuite(t *testing.T) {
	report, err := Render(Suite{
		Name: "Case Suite <smoke>",
		Cases: []Case{
			{Name: "case.pass", ClassName: "node.alpha", Status: "passed", TimeSeconds: 0.12},
			{Name: "case.fail", ClassName: "node.beta", Status: "failed", TimeSeconds: 1.5, FailureMessage: "expected <ok>", Output: "body mismatch"},
			{Name: "case.skip", ClassName: "node.gamma", Status: "skipped"},
		},
	})
	if err != nil {
		t.Fatalf("render junit: %v", err)
	}
	xml := string(report)
	for _, want := range []string{
		`<testsuite name="Case Suite &lt;smoke&gt;" tests="3" failures="1" skipped="1" time="1.620">`,
		`<testcase name="case.pass" classname="node.alpha" time="0.120"></testcase>`,
		`<failure message="expected &lt;ok&gt;">body mismatch</failure>`,
		`<skipped></skipped>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("junit xml missing %q:\n%s", want, xml)
		}
	}
}
