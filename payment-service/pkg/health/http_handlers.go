package health

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func ServerHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, Status{
		Healthy: true,
		Checks:  map[string]bool{"server": true},
	})
}

func TestServer(ctx context.Context, healthURL string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return fmt.Errorf("create server health request: %w", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("request server health: %w", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("server health returned %s", response.Status)
	}

	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
