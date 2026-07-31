package tot

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestAcceptorAuthenticatesAndAggregatesPaths(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	acceptor, err := NewAcceptor(listener, AcceptorOptions{
		Session:   Options{MaxPayload: 4, RetransmitInterval: 20 * time.Millisecond},
		Handshake: HandshakeOptions{Secret: secret},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer acceptor.Close()

	client := NewSession(1234, Options{MaxPayload: 4, RetransmitInterval: 20 * time.Millisecond})
	defer client.Close()
	addClientPath := func() {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		if err := ClientHandshake(conn, client.ID(), HandshakeOptions{Secret: secret}); err != nil {
			_ = conn.Close()
			t.Fatal(err)
		}
		if err := client.AddPath(conn); err != nil {
			t.Fatal(err)
		}
	}
	addClientPath()
	serverConn, err := acceptor.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer serverConn.Close()
	addClientPath()

	deadline := time.Now().Add(time.Second)
	server := serverConn.(*Session)
	for server.Stats().ActivePaths != 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if client.Stats().ActivePaths != 2 || server.Stats().ActivePaths != 2 {
		t.Fatalf("paths were not aggregated: client=%#v server=%#v", client.Stats(), server.Stats())
	}
	payload := []byte("authenticated multipath payload")
	go client.Write(payload)
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(server, received); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, payload) {
		t.Fatalf("received %q", received)
	}

	acceptResult := make(chan net.Conn, 1)
	go func() {
		conn, _ := acceptor.Accept()
		acceptResult <- conn
	}()
	select {
	case conn := <-acceptResult:
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatal("additional path created a second logical connection")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestHandshakeRejectsWrongSecret(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	serverResult := make(chan error, 1)
	go func() {
		_, err := ServerHandshake(server, HandshakeOptions{Secret: []byte("0123456789abcdef")}, nil)
		serverResult <- err
	}()
	err := ClientHandshake(client, 77, HandshakeOptions{Secret: []byte("fedcba9876543210"), Timeout: 50 * time.Millisecond})
	if err == nil {
		t.Fatal("wrong secret was accepted")
	}
	if serverErr := <-serverResult; !errors.Is(serverErr, ErrAuthentication) {
		t.Fatalf("server error = %v", serverErr)
	}
}

func TestServerHandshakeRejectsReplay(t *testing.T) {
	secret := []byte("0123456789abcdef")
	timestamp := time.Now().UnixMilli()
	nonce := bytes.Repeat([]byte{7}, nonceSize)
	payload := make([]byte, helloPayloadSize)
	binary.BigEndian.PutUint64(payload[0:8], uint64(timestamp))
	copy(payload[8:8+nonceSize], nonce)
	copy(payload[8+nonceSize:], handshakeDigest(secret, "client", 88, timestamp, nonce, nil))
	frame := Frame{Type: FrameHello, SessionID: 88, Payload: payload}

	seen := make(map[string]struct{})
	replay := func(value []byte) bool {
		key := string(value)
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
		return true
	}
	run := func(readAck bool) error {
		client, server := net.Pipe()
		defer client.Close()
		defer server.Close()
		result := make(chan error, 1)
		go func() {
			_, err := ServerHandshake(server, HandshakeOptions{Secret: secret}, replay)
			result <- err
		}()
		if err := WriteFrame(client, frame); err != nil {
			return err
		}
		if readAck {
			if _, err := ReadFrame(client, helloAckPayloadSize); err != nil {
				return err
			}
		}
		return <-result
	}
	if err := run(true); err != nil {
		t.Fatal(err)
	}
	if err := run(false); !errors.Is(err, ErrReplay) {
		t.Fatalf("replay error = %v", err)
	}
}
