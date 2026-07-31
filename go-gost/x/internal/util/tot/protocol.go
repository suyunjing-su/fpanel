package tot

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
)

const (
	magic      uint32 = 0x544f5431
	version    uint8  = 1
	headerSize        = 56
	tagSize           = 16
)

type FrameType uint8

const (
	FrameHello FrameType = iota + 1
	FrameHelloAck
	FrameData
	FrameAck
	FramePing
	FramePong
	FrameClose
)

var (
	ErrInvalidFrame  = errors.New("invalid TOT frame")
	ErrFrameTooLarge = errors.New("TOT frame payload exceeds limit")
)

type Frame struct {
	Type      FrameType
	Flags     uint16
	SessionID uint64
	Sequence  uint64
	Ack       uint64
	Payload   []byte
}

func WriteFrame(w io.Writer, frame Frame, secret ...[]byte) error {
	if len(frame.Payload) > int(^uint32(0)) {
		return ErrFrameTooLarge
	}
	header := make([]byte, headerSize)
	binary.BigEndian.PutUint32(header[0:4], magic)
	header[4] = version
	header[5] = byte(frame.Type)
	binary.BigEndian.PutUint16(header[6:8], frame.Flags)
	binary.BigEndian.PutUint64(header[8:16], frame.SessionID)
	binary.BigEndian.PutUint64(header[16:24], frame.Sequence)
	binary.BigEndian.PutUint64(header[24:32], frame.Ack)
	binary.BigEndian.PutUint32(header[32:36], uint32(len(frame.Payload)))
	binary.BigEndian.PutUint32(header[36:40], crc32.ChecksumIEEE(frame.Payload))
	tag := frameTag(secretBytes(secret), header[:40], frame.Payload)
	copy(header[40:56], tag)
	if err := writeFull(w, header); err != nil {
		return err
	}
	return writeFull(w, frame.Payload)
}

func ReadFrame(r io.Reader, maxPayload int, secret ...[]byte) (Frame, error) {
	var frame Frame
	header := make([]byte, headerSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return frame, err
	}
	if binary.BigEndian.Uint32(header[0:4]) != magic || header[4] != version {
		return frame, ErrInvalidFrame
	}
	frame.Type = FrameType(header[5])
	if frame.Type < FrameHello || frame.Type > FrameClose {
		return frame, ErrInvalidFrame
	}
	frame.Flags = binary.BigEndian.Uint16(header[6:8])
	frame.SessionID = binary.BigEndian.Uint64(header[8:16])
	frame.Sequence = binary.BigEndian.Uint64(header[16:24])
	frame.Ack = binary.BigEndian.Uint64(header[24:32])
	length := int(binary.BigEndian.Uint32(header[32:36]))
	if maxPayload >= 0 && length > maxPayload {
		return frame, ErrFrameTooLarge
	}
	if length > 0 {
		frame.Payload = make([]byte, length)
		if _, err := io.ReadFull(r, frame.Payload); err != nil {
			return Frame{}, err
		}
	}
	if crc32.ChecksumIEEE(frame.Payload) != binary.BigEndian.Uint32(header[36:40]) {
		return Frame{}, ErrInvalidFrame
	}
	if subtle.ConstantTimeCompare(header[40:56], frameTag(secretBytes(secret), header[:40], frame.Payload)) != 1 {
		return Frame{}, ErrInvalidFrame
	}
	return frame, nil
}

func secretBytes(secret [][]byte) []byte {
	if len(secret) == 0 {
		return nil
	}
	return secret[0]
}

func frameTag(secret, header, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(header)
	_, _ = mac.Write(payload)
	return mac.Sum(nil)[:tagSize]
}

func writeFull(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
