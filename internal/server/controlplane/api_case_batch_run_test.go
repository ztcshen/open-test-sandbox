package controlplane

import "testing"

func TestNormalizeAPICaseBatchOverrideKey(t *testing.T) {
	tests := map[string]string{
		"itemId":       "item_id",
		"item_id":      "item_id",
		"ItemID":       "item_id",
		"HTTPStatus":   "http_status",
		"item-id":      "item_id",
		" item id ":    "item_id",
		"order.total1": "",
	}
	for input, want := range tests {
		if got := normalizeAPICaseBatchOverrideKey(input); got != want {
			t.Fatalf("normalizeAPICaseBatchOverrideKey(%q) = %q, want %q", input, got, want)
		}
	}
}
