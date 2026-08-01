package main

import (
	"testing"

	"github.com/go-gost/x/config"
)

func TestBuildControlServicesRequireResolvedAuthentication(t *testing.T) {
	const missingAuther = "control-plane-missing-auther"
	if _, err := buildApiService(&config.APIConfig{Addr: "127.0.0.1:0", Auther: missingAuther}); err == nil {
		t.Fatal("API accepted a missing named authenticator")
	}
	if _, err := buildMetricsService(&config.MetricsConfig{Addr: "127.0.0.1:0", Auther: missingAuther}); err == nil {
		t.Fatal("metrics accepted a missing named authenticator")
	}
}

func TestBuildSecureControlBaseURL(t *testing.T) {
	for _, address := range []string{"http://controller.example", "ws://controller.example", "controller.example"} {
		if _, err := buildSecureControlBaseURL(address); err == nil {
			t.Fatalf("insecure controller %q was accepted", address)
		}
	}
	for _, address := range []string{"https://controller.example/", "wss://controller.example/"} {
		if _, err := buildSecureControlBaseURL(address); err != nil {
			t.Fatalf("secure controller %q was rejected: %v", address, err)
		}
	}
}

func TestRequireLoopbackAddress(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8080", "[::1]:8080", "localhost:8080", ":8080"} {
		if err := requireLoopbackAddress(address, "test"); err != nil {
			t.Fatalf("loopback address %q was rejected: %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:8080", "192.0.2.10:8080", "example.com:8080"} {
		if err := requireLoopbackAddress(address, "test"); err == nil {
			t.Fatalf("non-loopback address %q was accepted", address)
		}
	}
}
