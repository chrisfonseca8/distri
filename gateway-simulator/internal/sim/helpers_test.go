package sim

import (
	"context"
	"strings"
	"testing"
)

func contextWithCancel(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithCancel(context.Background())
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
