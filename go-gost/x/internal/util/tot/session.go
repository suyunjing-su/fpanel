package tot

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sort"
	"sync"
	"time"
)

var (
	ErrClosed          = errors.New("TOT session is closed")
	ErrNoPath          = errors.New("TOT session has no active path")
	ErrPathExists      = errors.New("TOT session path already exists")
	ErrRetransmitLimit = errors.New("TOT retransmission limit reached")
	ErrCloseTimeout    = errors.New("TOT session close timed out")
)

var sessionRegistry = struct {
	sync.Mutex
	items  map[*Session]struct{}
	totals Stats
}{items: make(map[*Session]struct{})}

type AggregateStats struct {
	Sessions        int    `json:"sessions"`
	ActivePaths     int    `json:"activePaths"`
	PendingFrames   int    `json:"pendingFrames"`
	SentFrames      uint64 `json:"sentFrames"`
	ReceivedFrames  uint64 `json:"receivedFrames"`
	Retransmits     uint64 `json:"retransmits"`
	DuplicateFrames uint64 `json:"duplicateFrames"`
	PathFailures    uint64 `json:"pathFailures"`
}

func Snapshot() AggregateStats {
	sessionRegistry.Lock()
	sessions := make([]*Session, 0, len(sessionRegistry.items))
	for session := range sessionRegistry.items {
		sessions = append(sessions, session)
	}
	totals := sessionRegistry.totals
	sessionRegistry.Unlock()

	var aggregate AggregateStats
	aggregate.SentFrames = totals.SentFrames
	aggregate.ReceivedFrames = totals.ReceivedFrames
	aggregate.Retransmits = totals.Retransmits
	aggregate.DuplicateFrames = totals.DuplicateFrames
	aggregate.PathFailures = totals.PathFailures
	for _, session := range sessions {
		stats := session.Stats()
		aggregate.Sessions++
		aggregate.ActivePaths += stats.ActivePaths
		aggregate.PendingFrames += stats.PendingFrames
		aggregate.SentFrames += stats.SentFrames
		aggregate.ReceivedFrames += stats.ReceivedFrames
		aggregate.Retransmits += stats.Retransmits
		aggregate.DuplicateFrames += stats.DuplicateFrames
		aggregate.PathFailures += stats.PathFailures
	}
	return aggregate
}

type Role uint8

const (
	RolePeer Role = iota
	RoleClient
	RoleServer
)

type Options struct {
	Key                []byte
	Role               Role
	MaxPayload         int
	Window             int
	RetransmitInterval time.Duration
	MaxRetries         int
	CloseTimeout       time.Duration
}

func (o *Options) defaults() {
	if len(o.Key) < 16 {
		o.Key = nil
	}
	if o.MaxPayload <= 0 {
		o.MaxPayload = 32 << 10
	}
	if o.Window <= 0 {
		o.Window = 256
	}
	if o.RetransmitInterval <= 0 {
		o.RetransmitInterval = 300 * time.Millisecond
	}
	if o.MaxRetries <= 0 {
		o.MaxRetries = 20
	}
	if o.CloseTimeout <= 0 {
		o.CloseTimeout = 2 * time.Second
	}
}

type Stats struct {
	ActivePaths     int    `json:"activePaths"`
	PendingFrames   int    `json:"pendingFrames"`
	SentFrames      uint64 `json:"sentFrames"`
	ReceivedFrames  uint64 `json:"receivedFrames"`
	Retransmits     uint64 `json:"retransmits"`
	DuplicateFrames uint64 `json:"duplicateFrames"`
	PathFailures    uint64 `json:"pathFailures"`
}

type packet struct {
	frame   Frame
	sentAt  time.Time
	retries int
	busy    bool
}

type path struct {
	id     uint64
	key    string
	conn   net.Conn
	write  sync.Mutex
	failed bool
}

type Session struct {
	id      uint64
	options Options
	sendKey []byte
	recvKey []byte

	mu              sync.Mutex
	receiveMu       sync.Mutex
	retransmitMu    sync.Mutex
	paths           map[uint64]*path
	nextPath        uint64
	roundRobin      uint64
	pending         map[uint64]*packet
	sendSeq         uint64
	recvSeq         uint64
	reorder         map[uint64][]byte
	stats           Stats
	err             error
	closing         bool
	remoteFinalSeq  uint64
	remoteCloseSeen bool
	local           net.Addr
	remote          net.Addr

	incoming      chan []byte
	readBuf       bytes.Buffer
	notify        chan struct{}
	retransmit    chan struct{}
	closed        chan struct{}
	closeOnce     sync.Once
	readDeadline  time.Time
	writeDeadline time.Time
}

func NewSession(id uint64, options Options) *Session {
	options.defaults()
	sendKey, recvKey := sessionFrameKeys(options.Key, id, options.Role)
	s := &Session{
		id:         id,
		options:    options,
		sendKey:    sendKey,
		recvKey:    recvKey,
		paths:      make(map[uint64]*path),
		pending:    make(map[uint64]*packet),
		sendSeq:    1,
		recvSeq:    1,
		reorder:    make(map[uint64][]byte),
		incoming:   make(chan []byte, options.Window),
		notify:     make(chan struct{}, 1),
		retransmit: make(chan struct{}, 1),
		closed:     make(chan struct{}),
	}
	sessionRegistry.Lock()
	sessionRegistry.items[s] = struct{}{}
	sessionRegistry.Unlock()
	go s.retransmitLoop()
	return s
}

func sessionFrameKeys(secret []byte, id uint64, role Role) ([]byte, []byte) {
	if role == RolePeer {
		return append([]byte(nil), secret...), append([]byte(nil), secret...)
	}
	derive := func(direction string) []byte {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write([]byte("TOT/1/frame/" + direction))
		var session [8]byte
		binary.BigEndian.PutUint64(session[:], id)
		_, _ = mac.Write(session[:])
		return mac.Sum(nil)
	}
	clientToServer := derive("client-to-server")
	serverToClient := derive("server-to-client")
	if role == RoleClient {
		return clientToServer, serverToClient
	}
	return serverToClient, clientToServer
}

func (s *Session) ID() uint64 { return s.id }

func (s *Session) AddPath(conn net.Conn) error {
	return s.AddNamedPath("", conn)
}

func (s *Session) AddNamedPath(key string, conn net.Conn) error {
	if conn == nil {
		return ErrNoPath
	}
	s.mu.Lock()
	select {
	case <-s.closed:
		s.mu.Unlock()
		return ErrClosed
	default:
	}
	for _, existing := range s.paths {
		if key != "" && existing.key == key {
			s.mu.Unlock()
			return ErrPathExists
		}
	}
	s.nextPath++
	p := &path{id: s.nextPath, key: key, conn: conn}
	s.paths[p.id] = p
	if s.local == nil {
		s.local, s.remote = conn.LocalAddr(), conn.RemoteAddr()
	}
	s.stats.ActivePaths = len(s.paths)
	s.mu.Unlock()
	s.signal()
	go s.readPath(p)
	s.requestRetransmit()
	return nil
}

func (s *Session) Read(buffer []byte) (int, error) {
	for {
		select {
		case data := <-s.incoming:
			s.mu.Lock()
			_, _ = s.readBuf.Write(data)
			s.mu.Unlock()
		default:
		}
		s.mu.Lock()
		if s.readBuf.Len() > 0 {
			n, _ := s.readBuf.Read(buffer)
			s.mu.Unlock()
			return n, nil
		}
		deadline := s.readDeadline
		err := s.err
		s.mu.Unlock()
		if err != nil {
			return 0, err
		}
		var timer <-chan time.Time
		if !deadline.IsZero() {
			duration := time.Until(deadline)
			if duration <= 0 {
				return 0, timeoutError{}
			}
			t := time.NewTimer(duration)
			defer t.Stop()
			timer = t.C
		}
		select {
		case data := <-s.incoming:
			s.mu.Lock()
			_, _ = s.readBuf.Write(data)
			s.mu.Unlock()
		case <-s.closed:
			select {
			case data := <-s.incoming:
				s.mu.Lock()
				_, _ = s.readBuf.Write(data)
				s.mu.Unlock()
				continue
			default:
			}
			s.mu.Lock()
			err = s.err
			s.mu.Unlock()
			if err == nil {
				err = io.EOF
			}
			return 0, err
		case <-timer:
			return 0, timeoutError{}
		}
	}
}

func (s *Session) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		size := len(data)
		if size > s.options.MaxPayload {
			size = s.options.MaxPayload
		}
		payload := append([]byte(nil), data[:size]...)
		sequence, err := s.queue(payload)
		if err != nil {
			return written, err
		}
		for {
			if err := s.transmit(sequence, false); err == nil {
				break
			}
			s.mu.Lock()
			deadline := s.writeDeadline
			s.mu.Unlock()
			if err := s.wait(deadline); err != nil {
				s.mu.Lock()
				delete(s.pending, sequence)
				s.mu.Unlock()
				s.signal()
				return written, err
			}
		}
		written += size
		data = data[size:]
	}
	return written, nil
}

func (s *Session) queue(payload []byte) (uint64, error) {
	for {
		s.mu.Lock()
		if s.err != nil || s.closing {
			err := s.err
			if err == nil {
				err = ErrClosed
			}
			s.mu.Unlock()
			return 0, err
		}
		if len(s.paths) > 0 && len(s.pending) < s.options.Window {
			sequence := s.sendSeq
			s.sendSeq++
			s.pending[sequence] = &packet{frame: Frame{Type: FrameData, SessionID: s.id, Sequence: sequence, Payload: payload}}
			s.mu.Unlock()
			return sequence, nil
		}
		deadline := s.writeDeadline
		s.mu.Unlock()
		if err := s.wait(deadline); err != nil {
			return 0, err
		}
	}
}

func (s *Session) wait(deadline time.Time) error {
	var timer <-chan time.Time
	if !deadline.IsZero() {
		duration := time.Until(deadline)
		if duration <= 0 {
			return timeoutError{}
		}
		t := time.NewTimer(duration)
		defer t.Stop()
		timer = t.C
	}
	select {
	case <-s.notify:
		return nil
	case <-s.closed:
		s.mu.Lock()
		err := s.err
		s.mu.Unlock()
		if err == nil {
			return ErrClosed
		}
		return err
	case <-timer:
		return timeoutError{}
	}
}

func (s *Session) transmit(sequence uint64, retransmit bool) error {
	s.mu.Lock()
	entry := s.pending[sequence]
	if entry == nil {
		s.mu.Unlock()
		return nil
	}
	if entry.busy {
		s.mu.Unlock()
		return nil
	}
	entry.busy = true
	frame := entry.frame
	paths := s.pathCandidatesLocked()
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if current := s.pending[sequence]; current != nil {
			current.busy = false
		}
		s.mu.Unlock()
		s.signal()
	}()
	if len(paths) == 0 {
		return ErrNoPath
	}
	var lastErr error
	for _, p := range paths {
		if err := s.send(p, frame); err != nil {
			lastErr = err
			s.dropPath(p, err)
			continue
		}
		s.mu.Lock()
		if current := s.pending[sequence]; current != nil {
			current.sentAt = time.Now()
			if retransmit {
				current.retries++
			}
		}
		if retransmit {
			s.stats.Retransmits++
		}
		s.stats.SentFrames++
		s.mu.Unlock()
		return nil
	}
	if lastErr == nil {
		lastErr = ErrNoPath
	}
	return lastErr
}

func (s *Session) pathCandidatesLocked() []*path {
	paths := make([]*path, 0, len(s.paths))
	ids := make([]uint64, 0, len(s.paths))
	for id := range s.paths {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	if len(ids) == 0 {
		return paths
	}
	start := int(s.roundRobin % uint64(len(ids)))
	s.roundRobin++
	for offset := 0; offset < len(ids); offset++ {
		paths = append(paths, s.paths[ids[(start+offset)%len(ids)]])
	}
	return paths
}

func (s *Session) send(p *path, frame Frame) error {
	p.write.Lock()
	defer p.write.Unlock()
	return WriteFrame(p.conn, frame, s.sendKey)
}

func (s *Session) readPath(p *path) {
	for {
		frame, err := ReadFrame(p.conn, s.options.MaxPayload, s.recvKey)
		if err != nil {
			s.dropPath(p, err)
			return
		}
		if frame.SessionID != s.id {
			s.dropPath(p, ErrInvalidFrame)
			return
		}
		s.handleFrame(p, frame)
	}
}

func (s *Session) handleFrame(p *path, frame Frame) {
	s.mu.Lock()
	s.stats.ReceivedFrames++
	s.mu.Unlock()
	s.acknowledge(frame.Ack)
	switch frame.Type {
	case FrameData:
		s.receiveData(p, frame)
	case FrameAck:
	case FramePing:
		_ = s.send(p, Frame{Type: FramePong, SessionID: s.id, Ack: s.receivedAck()})
	case FrameClose:
		s.receiveClose(frame.Sequence)
	}
}

func (s *Session) receiveClose(finalSequence uint64) {
	s.mu.Lock()
	if finalSequence > s.remoteFinalSeq {
		s.remoteFinalSeq = finalSequence
	}
	s.remoteCloseSeen = true
	ready := s.recvSeq > s.remoteFinalSeq
	s.mu.Unlock()
	if ready {
		s.closeWithError(io.EOF)
	}
}

func (s *Session) acknowledge(ack uint64) {
	if ack == 0 {
		return
	}
	s.mu.Lock()
	for sequence := range s.pending {
		if sequence <= ack {
			delete(s.pending, sequence)
		}
	}
	s.stats.PendingFrames = len(s.pending)
	s.mu.Unlock()
	s.signal()
}

func (s *Session) receiveData(p *path, frame Frame) {
	s.receiveMu.Lock()
	defer s.receiveMu.Unlock()
	s.mu.Lock()
	if frame.Sequence < s.recvSeq {
		s.stats.DuplicateFrames++
		ack := s.recvSeq - 1
		s.mu.Unlock()
		_ = s.send(p, Frame{Type: FrameAck, SessionID: s.id, Ack: ack})
		return
	}
	if _, exists := s.reorder[frame.Sequence]; !exists {
		s.reorder[frame.Sequence] = append([]byte(nil), frame.Payload...)
	}
	var ready [][]byte
	for {
		payload, exists := s.reorder[s.recvSeq]
		if !exists {
			break
		}
		delete(s.reorder, s.recvSeq)
		s.recvSeq++
		ready = append(ready, payload)
	}
	ack := s.recvSeq - 1
	closeReady := s.remoteCloseSeen && s.recvSeq > s.remoteFinalSeq
	s.mu.Unlock()
	for _, payload := range ready {
		select {
		case s.incoming <- payload:
		case <-s.closed:
			return
		}
	}
	_ = s.send(p, Frame{Type: FrameAck, SessionID: s.id, Ack: ack})
	if closeReady {
		s.closeWithError(io.EOF)
	}
}

func (s *Session) receivedAck() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.recvSeq - 1
}

func (s *Session) retransmitLoop() {
	ticker := time.NewTicker(s.options.RetransmitInterval / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.retransmitPending()
		case <-s.retransmit:
			s.retransmitPending()
		case <-s.closed:
			return
		}
	}
}

func (s *Session) retransmitPending() {
	s.retransmitMu.Lock()
	defer s.retransmitMu.Unlock()
	now := time.Now()
	var sequences []uint64
	s.mu.Lock()
	if len(s.paths) == 0 {
		s.mu.Unlock()
		return
	}
	for sequence, entry := range s.pending {
		if entry.busy {
			continue
		}
		if entry.sentAt.IsZero() || now.Sub(entry.sentAt) >= s.options.RetransmitInterval {
			if entry.retries >= s.options.MaxRetries {
				s.mu.Unlock()
				s.closeWithError(ErrRetransmitLimit)
				return
			}
			sequences = append(sequences, sequence)
		}
	}
	s.mu.Unlock()
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	for _, sequence := range sequences {
		_ = s.transmit(sequence, true)
	}
}

func (s *Session) dropPath(p *path, _ error) {
	s.mu.Lock()
	if current := s.paths[p.id]; current == p {
		delete(s.paths, p.id)
		p.failed = true
		s.stats.PathFailures++
		s.stats.ActivePaths = len(s.paths)
	}
	s.mu.Unlock()
	_ = p.conn.Close()
	s.signal()
	s.requestRetransmit()
}

func (s *Session) requestRetransmit() {
	select {
	case s.retransmit <- struct{}{}:
	default:
	}
}

func (s *Session) signal() {
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

func (s *Session) IsClosed() bool {
	select {
	case <-s.closed:
		return true
	default:
		return false
	}
}

func (s *Session) Done() <-chan struct{} { return s.closed }

func (s *Session) HasPath(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.paths {
		if p.key == key {
			return true
		}
	}
	return false
}

func (s *Session) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	stats := s.stats
	stats.ActivePaths = len(s.paths)
	stats.PendingFrames = len(s.pending)
	return stats
}

func (s *Session) Close() error {
	s.mu.Lock()
	if s.closing || s.err != nil {
		s.mu.Unlock()
		return nil
	}
	s.closing = true
	deadline := time.Now().Add(s.options.CloseTimeout)
	s.mu.Unlock()
	for {
		s.mu.Lock()
		pending := len(s.pending)
		err := s.err
		s.mu.Unlock()
		if pending == 0 || err != nil {
			break
		}
		if time.Now().After(deadline) {
			s.closeWithError(ErrCloseTimeout)
			return ErrCloseTimeout
		}
		if err := s.wait(deadline); err != nil && !errors.Is(err, ErrClosed) {
			s.closeWithError(ErrCloseTimeout)
			return ErrCloseTimeout
		}
	}
	s.closeWithError(io.EOF)
	return nil
}

func (s *Session) closeWithError(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		paths := make([]*path, 0, len(s.paths))
		for _, p := range s.paths {
			paths = append(paths, p)
		}
		ack := s.recvSeq - 1
		finalSequence := s.sendSeq - 1
		s.err = err
		s.paths = make(map[uint64]*path)
		s.stats.ActivePaths = 0
		s.mu.Unlock()
		for _, p := range paths {
			_ = p.conn.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
			_ = s.send(p, Frame{Type: FrameClose, SessionID: s.id, Sequence: finalSequence, Ack: ack})
			_ = p.conn.Close()
		}
		close(s.closed)
		sessionRegistry.Lock()
		sessionRegistry.totals.SentFrames += s.stats.SentFrames
		sessionRegistry.totals.ReceivedFrames += s.stats.ReceivedFrames
		sessionRegistry.totals.Retransmits += s.stats.Retransmits
		sessionRegistry.totals.DuplicateFrames += s.stats.DuplicateFrames
		sessionRegistry.totals.PathFailures += s.stats.PathFailures
		delete(sessionRegistry.items, s)
		sessionRegistry.Unlock()
		s.signal()
	})
}

func (s *Session) LocalAddr() net.Addr  { s.mu.Lock(); defer s.mu.Unlock(); return s.local }
func (s *Session) RemoteAddr() net.Addr { s.mu.Lock(); defer s.mu.Unlock(); return s.remote }
func (s *Session) SetDeadline(t time.Time) error {
	s.mu.Lock()
	s.readDeadline, s.writeDeadline = t, t
	s.mu.Unlock()
	s.signal()
	return nil
}
func (s *Session) SetReadDeadline(t time.Time) error {
	s.mu.Lock()
	s.readDeadline = t
	s.mu.Unlock()
	s.signal()
	return nil
}
func (s *Session) SetWriteDeadline(t time.Time) error {
	s.mu.Lock()
	s.writeDeadline = t
	s.mu.Unlock()
	s.signal()
	return nil
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "TOT operation timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }
