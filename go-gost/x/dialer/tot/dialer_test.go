package tot

import (
	"bytes"
	"context"
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
	client, err := dialer.Dial(context.Background(), listener.Addr().String(), coredialer.NetDialerDialOption(testNetDialer{}))
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

type testNetDialer struct{}

func (testNetDialer) Dial(ctx context.Context, network, address string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, network, address)
}
