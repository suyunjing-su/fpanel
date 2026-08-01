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

	"github.com/go-gost/x/config"
	"github.com/go-gost/x/controller"
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
		case "https", "wss":
			u.Scheme = "https"
			return strings.TrimRight(u.String(), "/"), nil
		default:
			return "", fmt.Errorf("controller address must use https:// or wss://")
		}
	}

	return "", fmt.Errorf("server address must include https:// or wss://")
}

func syncFullConfigFromDashboard(pool *controller.Pool, secret string) error {
	var failures []string
	for _, addr := range pool.Candidates() {
		if err := fetchFullConfig(addr, secret); err != nil {
			pool.Fail(addr, err)
			failures = append(failures, addr+": "+err.Error())
			continue
		}
		pool.Succeed(addr)
		return nil
	}
	return fmt.Errorf("all controllers failed: %s", strings.Join(failures, "; "))
}

func syncFullConfigOrUseCache(pool *controller.Pool, secret, path string) (bool, error) {
	syncErr := syncFullConfigFromDashboard(pool, secret)
	if syncErr == nil {
		return false, nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("%v; cached config unavailable: %w", syncErr, err)
	}
	if len(strings.TrimSpace(string(content))) == 0 {
		return false, fmt.Errorf("%v; cached config is empty", syncErr)
	}
	var cached config.Config
	if err := json.Unmarshal(content, &cached); err != nil {
		return false, fmt.Errorf("%v; cached config is invalid: %w", syncErr, err)
	}
	return true, nil
}

func fetchFullConfig(addr string, secret string) error {
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

	if err := config.WriteFileAtomic("gost.json", serialized, 0600); err != nil {
		return fmt.Errorf("write gost.json failed: %v", err)
	}

	return nil
}
