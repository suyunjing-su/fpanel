package tot

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"time"
)

const (
	nonceSize           = 32
	digestSize          = 32
	helloPayloadSize    = 8 + nonceSize + digestSize
	helloAckPayloadSize = 8 + nonceSize + digestSize
)

var (
	ErrAuthentication = errors.New("TOT authentication failed")
	ErrReplay         = errors.New("TOT handshake replay detected")
)

type HandshakeOptions struct {
	Secret       []byte
	Timeout      time.Duration
	MaxClockSkew time.Duration
}

func (o *HandshakeOptions) defaults() error {
	if len(o.Secret) < 16 {
		return errors.New("TOT secret must contain at least 16 bytes")
	}
	if o.Timeout <= 0 {
		o.Timeout = 5 * time.Second
	}
	if o.MaxClockSkew <= 0 {
		o.MaxClockSkew = 30 * time.Second
	}
	return nil
}

func ClientHandshake(conn net.Conn, sessionID uint64, options HandshakeOptions) error {
	if conn == nil || sessionID == 0 {
		return ErrInvalidFrame
	}
	if err := options.defaults(); err != nil {
		return err
	}
	deadline := time.Now().Add(options.Timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	defer conn.SetDeadline(time.Time{})

	timestamp := time.Now().UnixMilli()
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	payload := make([]byte, helloPayloadSize)
	binary.BigEndian.PutUint64(payload[0:8], uint64(timestamp))
	copy(payload[8:8+nonceSize], nonce)
	copy(payload[8+nonceSize:], handshakeDigest(options.Secret, "client", sessionID, timestamp, nonce, nil))
	if err := WriteFrame(conn, Frame{Type: FrameHello, SessionID: sessionID, Payload: payload}); err != nil {
		return err
	}
	response, err := ReadFrame(conn, helloAckPayloadSize)
	if err != nil {
		return err
	}
	if response.Type != FrameHelloAck || response.SessionID != sessionID || len(response.Payload) != helloAckPayloadSize {
		return ErrAuthentication
	}
	serverTimestamp := int64(binary.BigEndian.Uint64(response.Payload[0:8]))
	if !withinClockSkew(serverTimestamp, options.MaxClockSkew) {
		return ErrAuthentication
	}
	serverNonce := response.Payload[8 : 8+nonceSize]
	expected := handshakeDigest(options.Secret, "server", sessionID, serverTimestamp, nonce, serverNonce)
	if subtle.ConstantTimeCompare(response.Payload[8+nonceSize:], expected) != 1 {
		return ErrAuthentication
	}
	return nil
}

func ServerHandshake(conn net.Conn, options HandshakeOptions, replay func([]byte) bool) (uint64, error) {
	if conn == nil {
		return 0, ErrInvalidFrame
	}
	if err := options.defaults(); err != nil {
		return 0, err
	}
	deadline := time.Now().Add(options.Timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, err
	}
	defer conn.SetDeadline(time.Time{})

	hello, err := ReadFrame(conn, helloPayloadSize)
	if err != nil {
		return 0, err
	}
	if hello.Type != FrameHello || hello.SessionID == 0 || len(hello.Payload) != helloPayloadSize {
		return 0, ErrAuthentication
	}
	timestamp := int64(binary.BigEndian.Uint64(hello.Payload[0:8]))
	if !withinClockSkew(timestamp, options.MaxClockSkew) {
		return 0, ErrAuthentication
	}
	nonce := append([]byte(nil), hello.Payload[8:8+nonceSize]...)
	expected := handshakeDigest(options.Secret, "client", hello.SessionID, timestamp, nonce, nil)
	if subtle.ConstantTimeCompare(hello.Payload[8+nonceSize:], expected) != 1 {
		return 0, ErrAuthentication
	}
	if replay != nil && !replay(nonce) {
		return 0, ErrReplay
	}

	serverTimestamp := time.Now().UnixMilli()
	serverNonce := make([]byte, nonceSize)
	if _, err := rand.Read(serverNonce); err != nil {
		return 0, err
	}
	payload := make([]byte, helloAckPayloadSize)
	binary.BigEndian.PutUint64(payload[0:8], uint64(serverTimestamp))
	copy(payload[8:8+nonceSize], serverNonce)
	copy(payload[8+nonceSize:], handshakeDigest(options.Secret, "server", hello.SessionID, serverTimestamp, nonce, serverNonce))
	if err := WriteFrame(conn, Frame{Type: FrameHelloAck, SessionID: hello.SessionID, Payload: payload}); err != nil {
		return 0, err
	}
	return hello.SessionID, nil
}

func NewSessionID() (uint64, error) {
	var value [8]byte
	if _, err := rand.Read(value[:]); err != nil {
		return 0, err
	}
	id := binary.BigEndian.Uint64(value[:])
	if id == 0 {
		return NewSessionID()
	}
	return id, nil
}

func handshakeDigest(secret []byte, role string, sessionID uint64, timestamp int64, firstNonce, secondNonce []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("TOT/1/" + role))
	var number [8]byte
	binary.BigEndian.PutUint64(number[:], sessionID)
	_, _ = mac.Write(number[:])
	binary.BigEndian.PutUint64(number[:], uint64(timestamp))
	_, _ = mac.Write(number[:])
	_, _ = mac.Write(firstNonce)
	_, _ = mac.Write(secondNonce)
	return mac.Sum(nil)
}

func withinClockSkew(timestamp int64, skew time.Duration) bool {
	delta := time.Since(time.UnixMilli(timestamp))
	if delta < 0 {
		delta = -delta
	}
	return delta <= skew
}

func sessionKey(id uint64) string {
	return fmt.Sprintf("%016x", id)
}
