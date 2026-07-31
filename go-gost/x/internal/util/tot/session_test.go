package tot

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func TestFrameRoundTripAndIntegrity(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	original := Frame{Type: FrameData, Flags: 3, SessionID: 7, Sequence: 11, Ack: 9, Payload: []byte("payload")}
	var buffer bytes.Buffer
	if err := WriteFrame(&buffer, original, secret); err != nil {
		t.Fatal(err)
	}
	encoded := append([]byte(nil), buffer.Bytes()...)
	decoded, err := ReadFrame(&buffer, 1024, secret)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Type != original.Type || decoded.Flags != original.Flags || decoded.SessionID != original.SessionID || decoded.Sequence != original.Sequence || decoded.Ack != original.Ack || !bytes.Equal(decoded.Payload, original.Payload) {
		t.Fatalf("frame mismatch: %#v", decoded)
	}

	for _, mutate := range []func([]byte){
		func(data []byte) { data[8] ^= 0xff },
		func(data []byte) { data[40] ^= 0xff },
		func(data []byte) { data[len(data)-1] ^= 0xff },
	} {
		corrupt := append([]byte(nil), encoded...)
		mutate(corrupt)
		if _, err := ReadFrame(bytes.NewReader(corrupt), 1024, secret); !errors.Is(err, ErrInvalidFrame) {
			t.Fatalf("corrupt frame error = %v", err)
		}
	}
	if _, err := ReadFrame(bytes.NewReader(encoded), 1024, []byte("fedcba9876543210fedcba9876543210")); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("wrong secret error = %v", err)
	}
}

func TestSessionCloseExchangesCloseFrame(t *testing.T) {
	options := Options{Key: []byte("0123456789abcdef0123456789abcdef")}
	left := NewSession(123, options)
	right := NewSession(123, options)
	leftConn, rightConn := net.Pipe()
	if err := left.AddPath(leftConn); err != nil {
		t.Fatal(err)
	}
	if err := right.AddPath(rightConn); err != nil {
		t.Fatal(err)
	}

	if err := left.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-right.Done():
	case <-time.After(time.Second):
		t.Fatal("peer session did not close after receiving FrameClose")
	}
	if !left.IsClosed() {
		t.Fatal("local session remained open")
	}
}

func TestSessionCloseDoesNotBlockWithoutPeerReader(t *testing.T) {
	left := NewSession(124, Options{Key: []byte("0123456789abcdef0123456789abcdef")})
	peer, local := net.Pipe()
	defer peer.Close()
	if err := left.AddPath(local); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		_ = left.Close()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("session close blocked on an unresponsive path")
	}
}
func TestSessionAggregatesPathsAndReordersFrames(t *testing.T) {
	left := NewSession(42, Options{MaxPayload: 4, RetransmitInterval: 50 * time.Millisecond})
	right := NewSession(42, Options{MaxPayload: 4, RetransmitInterval: 50 * time.Millisecond})
	defer left.Close()
	defer right.Close()
	for i := 0; i < 2; i++ {
		a, b := net.Pipe()
		if err := left.AddPath(a); err != nil {
			t.Fatal(err)
		}
		if err := right.AddPath(b); err != nil {
			t.Fatal(err)
		}
	}
	payload := []byte("abcdefghijklmnop")
	writeDone := make(chan error, 1)
	go func() {
		_, err := left.Write(payload)
		writeDone <- err
	}()
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(right, received); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, payload) {
		t.Fatalf("received %q", received)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for left.Stats().PendingFrames != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if stats := left.Stats(); stats.ActivePaths != 2 || stats.PendingFrames != 0 || stats.SentFrames < 4 {
		t.Fatalf("unexpected sender stats: %#v", stats)
	}
}

func TestSessionRetransmitsAfterPathFailureAndRecovery(t *testing.T) {
	options := Options{MaxPayload: 64, RetransmitInterval: 20 * time.Millisecond, MaxRetries: 20}
	left := NewSession(99, options)
	right := NewSession(99, options)
	defer left.Close()
	defer right.Close()
	client, server := net.Pipe()
	if err := left.AddPath(client); err != nil {
		t.Fatal(err)
	}
	if err := right.AddPath(&dropFirstDataConn{Conn: server}); err != nil {
		t.Fatal(err)
	}
	payload := []byte("survives path replacement")
	writeDone := make(chan error, 1)
	go func() {
		_, err := left.Write(payload)
		writeDone <- err
	}()
	time.Sleep(40 * time.Millisecond)
	client2, server2 := net.Pipe()
	if err := left.AddPath(client2); err != nil {
		t.Fatal(err)
	}
	if err := right.AddPath(server2); err != nil {
		t.Fatal(err)
	}
	received := make([]byte, len(payload))
	if _, err := io.ReadFull(right, received); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(received, payload) {
		t.Fatalf("received %q", received)
	}
	if err := <-writeDone; err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for left.Stats().PendingFrames != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stats := left.Stats()
	if stats.PathFailures == 0 || stats.Retransmits == 0 || stats.PendingFrames != 0 {
		t.Fatalf("recovery not observed: %#v", stats)
	}
}

type dropFirstDataConn struct {
	net.Conn
	dropped bool
}

func (c *dropFirstDataConn) Read(buffer []byte) (int, error) {
	if c.dropped {
		return c.Conn.Read(buffer)
	}
	frame, err := ReadFrame(c.Conn, 64)
	if err != nil {
		return 0, err
	}
	if frame.Type == FrameData {
		c.dropped = true
		return 0, errors.New("injected path failure")
	}
	var encoded bytes.Buffer
	if err := WriteFrame(&encoded, frame); err != nil {
		return 0, err
	}
	return encoded.Read(buffer)
}
