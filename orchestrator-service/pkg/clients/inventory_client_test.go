package clients

import (
	"strings"
	"testing"
)

func TestNewInventoryClientFromEnvRequiresInventoryURL(t *testing.T) {
	t.Setenv("INVENTORY_URL", "")

	client, err := NewInventoryClientFromEnv()
	if client != nil {
		t.Fatal("client is not nil")
	}
	if err == nil || !strings.Contains(err.Error(), "INVENTORY_URL is not set") {
		t.Fatalf("error = %v, want missing INVENTORY_URL error", err)
	}
}
