package seal

import (
	"strings"
	"testing"
)

// Produced by the dashboard's lib/mcp-grant.ts seal() with key = 32 bytes of 7
// and nonce = 12 bytes of 9. Go must open what TypeScript seals.
const (
	tsKey   = "BwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwcHBwc="
	tsGrant = "CQkJCQkJCQkJCQkJXKfytoTB7UPSB7pNg5nXgLzREJsW1NWTIeg35OcBaCC1lLb1jARuQCvYzUY9V62zNif68XDC1yRWHUr0P0Y-21oYqiKrU5_Gqr-gEJ3TpoAw2Ok6iWa01CLUM_99vS8PcC6A1poJwEx747sqCZclSMBrdCfz9KONdej9pw5WYDQ3uFwHFt8wm2ime-1K1XSUMOZQRsJg9mB7TMzrnoY-vTkMkS5Gu0igl32wLDirV36K29x4odtJ2UemSzqPgUzIt3iE"
)

func TestOpensDashboardSealedGrant(t *testing.T) {
	b, err := FromString(tsKey)
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		RequestHash string `json:"request_hash"`
		APIKey      string `json:"api_key"`
		APIKeyName  string `json:"api_key_name"`
	}
	if err := b.Open("meetstream-mcp-grant-v1", tsGrant, &g); err != nil {
		t.Fatalf("open: %v", err)
	}
	if g.APIKey != "ms_TESTKEY" || g.APIKeyName != "MCP · Claude · 2026-10-03" || g.RequestHash != Hash("msr_test") {
		t.Fatalf("decoded %+v", g)
	}
	// Bound to its purpose.
	if b.Open("meetstream-mcp-consent-v1", tsGrant, &g) == nil {
		t.Fatal("opened under the wrong purpose")
	}
}

func TestRotationAndTamper(t *testing.T) {
	oldB, _ := FromString(tsKey)
	s, _ := oldB.Seal("p", map[string]int{"a": 1})
	both, _ := FromString("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=," + tsKey)
	var v map[string]int
	if err := both.Open("p", s, &v); err != nil || v["a"] != 1 {
		t.Fatal("rotated key list must still open values sealed with the old key")
	}
	bad := s[:len(s)-2] + strings.Repeat("A", 2)
	if both.Open("p", bad, &v) == nil {
		t.Fatal("tampered value opened")
	}
	if !both.VerifyMAC("x", "d", oldB.MAC("x", "d")) || both.VerifyMAC("x", "d2", oldB.MAC("x", "d")) {
		t.Fatal("MAC verify")
	}
}
