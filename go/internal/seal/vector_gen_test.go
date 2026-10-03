package seal

import (
	"fmt"
	"os"
	"testing"
)

// Run with SEAL_PRINT_VECTOR=1 to print a Go-sealed consent context for the dashboard's tests.
func TestPrintConsentVector(t *testing.T) {
	if os.Getenv("SEAL_PRINT_VECTOR") == "" {
		t.Skip()
	}
	b, _ := FromString(tsKey)
	s, _ := b.Seal("meetstream-mcp-consent-v1", map[string]any{"v": 1, "request_hash": Hash("msr_test"), "client_name": "Claude",
		"redirect_uri": "https://claude.ai/api/mcp/auth_callback", "redirect_host": "claude.ai", "exp": 4102444800})
	fmt.Println("VECTOR", s)
}
