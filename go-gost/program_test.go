package main

import (
	"testing"

	"github.com/go-gost/x/config"
)

func TestProfilingAddr(t *testing.T) {
	tests := []struct {
		name    string
		config  config.ProfilingConfig
		want    string
		wantErr bool
	}{
		{name: "default loopback", want: "127.0.0.1:6060"},
		{name: "ipv4 loopback", config: config.ProfilingConfig{Addr: "127.0.0.1:7000"}, want: "127.0.0.1:7000"},
		{name: "ipv6 loopback", config: config.ProfilingConfig{Addr: "[::1]:7000"}, want: "[::1]:7000"},
		{name: "empty host normalizes to loopback", config: config.ProfilingConfig{Addr: ":7000"}, want: "127.0.0.1:7000"},
		{name: "remote address rejected", config: config.ProfilingConfig{Addr: "0.0.0.0:6060"}, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := profilingAddr(&test.config)
			if test.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("address=%q want=%q", got, test.want)
			}
		})
	}
}
