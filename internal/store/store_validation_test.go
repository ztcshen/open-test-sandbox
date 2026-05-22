package store

import (
	"strings"
	"testing"
)

func TestValidateEnvironmentComponentGraphAllowsInlineDDLUnderOneMB(t *testing.T) {
	ddl := "USE app;\n" + strings.Repeat("CREATE TABLE IF NOT EXISTS t_demo (id BIGINT PRIMARY KEY);\n", 900)
	graph := EnvironmentComponentGraph{
		Components: []EnvironmentComponent{
			{ComponentID: "app", Required: true},
			{ComponentID: "mysql", Required: true},
		},
		Dependencies: []ComponentDependency{
			{ConsumerComponentID: "app", ProviderComponentID: "mysql", Phase: "startup", Capability: "sql", Required: true, ProfileJSON: `{"assetIds":["app.mysql"]}`},
		},
		Assets: []ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.mysql", AssetKind: "mysql-ddl", TargetComponentID: "mysql", TargetPath: "compose/mysql/init/app.sql", ContentInline: ddl},
		},
	}

	if err := ValidateEnvironmentComponentGraph("env.ddl", graph); err != nil {
		t.Fatalf("inline DDL under the 1MB Store boundary should be accepted without per-kind limits: %v", err)
	}
}

func TestValidateEnvironmentComponentGraphBlocksSingleInlineAssetOverOneMBWithSpecificReason(t *testing.T) {
	graph := EnvironmentComponentGraph{
		Components: []EnvironmentComponent{
			{ComponentID: "app", Required: true},
			{ComponentID: "mysql", Required: true},
		},
		Assets: []ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.mysql", AssetKind: "mysql-ddl", TargetComponentID: "mysql", TargetPath: "compose/mysql/init/app.sql", ContentInline: strings.Repeat("x", ComponentAssetInlineMaxBytes+1)},
		},
	}

	err := ValidateEnvironmentComponentGraph("env.too-large-asset", graph)
	if err == nil {
		t.Fatal("expected oversized inline asset to be rejected")
	}
	if got := err.Error(); !strings.Contains(got, "write blocked") || !strings.Contains(got, `component config asset "app.mysql" inline content`) || !strings.Contains(got, "Reason:") || !strings.Contains(got, `kind="mysql-ddl"`) || !strings.Contains(got, `path="compose/mysql/init/app.sql"`) {
		t.Fatalf("oversized asset error should name asset, path, kind, and reason, got %q", got)
	}
}

func TestValidateEnvironmentComponentGraphBlocksInlineGraphOverOneMB(t *testing.T) {
	graph := EnvironmentComponentGraph{
		Components: []EnvironmentComponent{
			{ComponentID: "app", Required: true},
			{ComponentID: "mysql", Required: true},
		},
		Assets: []ComponentConfigAsset{
			{OwnerComponentID: "app", AssetID: "app.part1", AssetKind: "mysql-ddl", TargetComponentID: "mysql", TargetPath: "compose/mysql/init/app-1.sql", ContentInline: strings.Repeat("a", ComponentAssetInlineMaxBytes/2+1)},
			{OwnerComponentID: "app", AssetID: "app.part2", AssetKind: "mysql-ddl", TargetComponentID: "mysql", TargetPath: "compose/mysql/init/app-2.sql", ContentInline: strings.Repeat("b", ComponentAssetInlineMaxBytes/2+1)},
		},
	}

	err := ValidateEnvironmentComponentGraph("env.too-large-graph", graph)
	if err == nil {
		t.Fatal("expected oversized inline graph to be rejected")
	}
	if got := err.Error(); !strings.Contains(got, "write blocked") || !strings.Contains(got, "environment component graph metadata") || !strings.Contains(got, "Reason:") || !strings.Contains(got, `asset "app.part`) || !strings.Contains(got, "largest contributor") {
		t.Fatalf("oversized graph error should explain reason, got %q", got)
	}
}
