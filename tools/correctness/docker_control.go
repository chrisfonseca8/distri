package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type dockerContainer struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	State  string            `json:"State"`
	Labels map[string]string `json:"Labels"`
}

// dockerContainerAction stops or starts the one non-oneoff Compose container
// for service through a forwarded Docker Unix socket.
func dockerContainerAction(ctx context.Context, socket, action, service string) error {
	if action != "start" && action != "stop" {
		return fmt.Errorf("unsupported Docker container action %q", action)
	}
	client := dockerUnixClient(socket)
	filters, err := json.Marshal(map[string][]string{
		"label": {
			"com.docker.compose.service=" + service,
			"com.docker.compose.oneoff=False",
		},
	})
	if err != nil {
		return err
	}
	endpoint := "http://docker/containers/json?all=1&filters=" + url.QueryEscape(string(filters))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("list %s containers through %s: %w", service, socket, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("list %s containers: Docker returned %s: %s", service, response.Status, strings.TrimSpace(string(body)))
	}
	var containers []dockerContainer
	if err := json.NewDecoder(response.Body).Decode(&containers); err != nil {
		return fmt.Errorf("decode %s container list: %w", service, err)
	}
	if len(containers) != 1 {
		return fmt.Errorf("expected exactly one %s container, found %d", service, len(containers))
	}

	actionURL := fmt.Sprintf("http://docker/containers/%s/%s", containers[0].ID, action)
	if action == "stop" {
		actionURL += "?t=40"
	}
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, actionURL, nil)
	if err != nil {
		return err
	}
	response, err = client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s through %s: %w", action, service, socket, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent && response.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("%s %s: Docker returned %s: %s", action, service, response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func dockerUnixClient(socket string) *http.Client {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}
	return &http.Client{Transport: transport, Timeout: 50 * time.Second}
}
