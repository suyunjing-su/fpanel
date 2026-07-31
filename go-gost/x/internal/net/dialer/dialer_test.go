package dialer

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/go-gost/core/logger"
	xlogger "github.com/go-gost/x/logger"
)

func TestNewNetDialerEnablesMultipathTCP(t *testing.T) {
	dialer := (&Dialer{Netns: "configured"}).newNetDialer(nil, "", true, logger.Default())
	if !dialer.MultipathTCP() {
		t.Fatal("multipath TCP was not enabled")
	}
	if dialer.FallbackDelay != -1 {
		t.Fatalf("netns fallback delay = %v", dialer.FallbackDelay)
	}
}

func TestDialMultipathTCPRejectsCustomRoute(t *testing.T) {
	dialer := &Dialer{Logger: xlogger.NewLogger(), DialFunc: func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("custom route dialed despite unsupported multipath TCP")
		return nil, nil
	}}
	_, err := dialer.DialMultipathTCP(context.Background(), "tcp", "127.0.0.1:1")
	if !errors.Is(err, ErrMultipathTCPUnsupported) {
		t.Fatalf("multipath custom route error = %v", err)
	}
}
