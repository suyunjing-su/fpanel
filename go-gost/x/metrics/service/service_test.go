package service

import (
	"testing"
)

func TestNewServiceRequiresAuthentication(t *testing.T) {
	if _, err := NewService("tcp", "127.0.0.1:0"); err == nil {
		t.Fatal("unauthenticated metrics service was created")
	}
}
