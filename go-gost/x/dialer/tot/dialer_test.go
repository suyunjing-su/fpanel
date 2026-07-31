package tot

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	coredialer "github.com/go-gost/core/dialer"
	corelistener "github.com/go-gost/core/listener"
	"github.com/go-gost/x/internal/util/tot"
	listenerTot "github.com/go-gost/x/listener/tot"
	xmetadata "github.com/go-gost/x/metadata"
)

func TestDialerCreatesIndependentSessions(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	listener := listenerTot.NewListener(corelistener.AddrOption("127.0.0.1:0"))
	if err := listener.Init(xmetadata.NewMetadata(map[string]any{"secret": secret, "pathCount": 1})); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	dialer := NewDialer()
	if err := dialer.Init(xmetadata.NewMetadata(map[string]any{"secret": secret, "pathCount": 1})); err != nil {
		t.Fatal(err)
	}
	options := coredialer.NetDialerDialOption(&testNetDialer{})
	first, err := dialer.Dial(context.Background(), listener.Addr().String(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := dialer.Dial(context.Background(), listener.Addr().String(), options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.(*tot.Session).ID() == second.(*tot.Session).ID() {
		t.Fatal("independent dials reused the same session")
	}
	for i := 0; i < 2; i++ {
		conn, err := listener.Accept()
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
}
func TestDialerAndListenerExchangeData(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	listener := listenerTot.NewListener(corelistener.AddrOption("127.0.0.1:0"))
	if err := listener.Init(xmetadata.NewMetadata(map[string]any{
		"secret":             secret,
		"maxPayload":         256,
		"retransmitInterval": "20ms",
	})); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	dialer := NewDialer()
	if err := dialer.Init(xmetadata.NewMetadata(map[string]any{
		"secret":             secret,
		"pathCount":          2,
		"maxPayload":         256,
		"retransmitInterval": "20ms",
		"recoveryPeriod":     "20ms",
	})); err != nil {
		t.Fatal(err)
	}
	client, err := dialer.Dial(context.Background(), listener.Addr().String(), coredialer.NetDialerDialOption(&testNetDialer{}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	clientSession := client.(*tot.Session)
	deadline := time.Now().Add(time.Second)
	for clientSession.Stats().ActivePaths < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if clientSession.Stats().ActivePaths < 2 {
		t.Fatalf("expected two active paths, got %#v", clientSession.Stats())
	}

	request := bytes.Repeat([]byte("request-"), 1024)
	go func() {
		_, _ = client.Write(request)
	}()
	received := make([]byte, len(request))
	if _, err := io.ReadFull(server, received); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, request) {
		t.Fatal("request payload mismatch")
	}
	response := bytes.Repeat([]byte("response-"), 1024)
	go func() {
		_, _ = server.Write(response)
	}()
	received = make([]byte, len(response))
	if _, err := io.ReadFull(client, received); err != nil {
		t.Fatal(err)
	}
}

func TestDialerRequiresMultipathTCPCapability(t *testing.T) {
	dialer := NewDialer()
	if err := dialer.Init(xmetadata.NewMetadata(map[string]any{
		"secret": "0123456789abcdef0123456789abcdef",
		"mptcp":  true,
	})); err != nil {
		t.Fatal(err)
	}
	_, err := dialer.Dial(context.Background(), "127.0.0.1:1", coredialer.NetDialerDialOption(plainTestNetDialer{}))
	if err == nil || err.Error() != "TOT multipath TCP requires a capable network dialer" {
		t.Fatalf("multipath capability error = %v", err)
	}
}

func TestDialerUsesMultipathTCPCapability(t *testing.T) {
	secret := "0123456789abcdef0123456789abcdef"
	listener := listenerTot.NewListener(corelistener.AddrOption("127.0.0.1:0"))
	if err := listener.Init(xmetadata.NewMetadata(map[string]any{"secret": secret})); err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	netDialer := &testNetDialer{}
	dialer := NewDialer()
	if err := dialer.Init(xmetadata.NewMetadata(map[string]any{"secret": secret, "pathCount": 1, "mptcp": true})); err != nil {
		t.Fatal(err)
	}
	client, err := dialer.Dial(context.Background(), listener.Addr().String(), coredialer.NetDialerDialOption(netDialer))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if netDialer.multipathCalls != 1 || netDialer.plainCalls != 0 {
		t.Fatalf("dial calls: multipath=%d plain=%d", netDialer.multipathCalls, netDialer.plainCalls)
	}
}

type plainTestNetDialer struct{}

func (plainTestNetDialer) Dial(context.Context, string, string) (net.Conn, error) {
	return nil, errors.New("plain dial should not run")
}

type testNetDialer struct {
	plainCalls     int
	multipathCalls int
}

func (d *testNetDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	d.plainCalls++
	return (&net.Dialer{}).DialContext(ctx, network, address)
}

func (d *testNetDialer) DialMultipathTCP(ctx context.Context, network, address string) (net.Conn, error) {
	d.multipathCalls++
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
