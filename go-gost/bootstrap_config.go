package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func buildSecureControlBaseURL(addr string) (string, error) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return "", fmt.Errorf("server address is empty")
	}

	if strings.Contains(trimmed, "://") {
		u, err := url.Parse(trimmed)
		if err != nil {
			return "", fmt.Errorf("parse server address failed: %v", err)
		}

		switch strings.ToLower(u.Scheme) {
		case "https":
			u.Scheme = "https"
			return strings.TrimRight(u.String(), "/"), nil
		case "wss":
			u.Scheme = "https"
			return strings.TrimRight(u.String(), "/"), nil
		case "http":
			u.Scheme = "http"
			return strings.TrimRight(u.String(), "/"), nil
		case "ws":
			u.Scheme = "http"
			return strings.TrimRight(u.String(), "/"), nil
		default:
			return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
		}
	}

	return "", fmt.Errorf("server address must include scheme: http://, https://, ws:// or wss://")
}

func syncFullConfigFromDashboard(addr string, secret string) error {
	baseURL, err := buildSecureControlBaseURL(addr)
	if err != nil {
		return err
	}

	endpoint := baseURL + "/flow/config/all"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %v", err)
	}

	req.Header.Set("User-Agent", "Flux-Agent-Bootstrap/1.0")
	if strings.TrimSpace(secret) != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request full config failed: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read full config response failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("full config endpoint status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		trimmed = "{}"
	}

	var configDoc map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &configDoc); err != nil {
		return fmt.Errorf("invalid full config payload: %v", err)
	}

	serialized, err := json.MarshalIndent(configDoc, "", "  ")
	if err != nil {
		return fmt.Errorf("serialize full config payload failed: %v", err)
	}

	if err := os.WriteFile("gost.json", serialized, 0600); err != nil {
		return fmt.Errorf("write gost.json failed: %v", err)
	}

	return nil
}
