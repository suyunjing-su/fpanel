package tot

import (
	"errors"
	"net"
	"sync"
	"time"
)

type AcceptorOptions struct {
	Session   Options
	Handshake HandshakeOptions
	Backlog   int
	IdleTTL   time.Duration
}

func (o *AcceptorOptions) defaults() error {
	o.Session.defaults()
	if err := o.Handshake.defaults(); err != nil {
		return err
	}
	if o.Backlog <= 0 {
		o.Backlog = 128
	}
	if o.IdleTTL <= 0 {
		o.IdleTTL = 5 * time.Minute
	}
	return nil
}

type managedSession struct {
	session  *Session
	lastSeen time.Time
}

type Acceptor struct {
	listener  net.Listener
	options   AcceptorOptions
	ready     chan net.Conn
	errors    chan error
	closed    chan struct{}
	handshake chan struct{}
	closeOnce sync.Once

	mu       sync.Mutex
	sessions map[uint64]*managedSession
	nonces   map[string]time.Time
}

func NewAcceptor(listener net.Listener, options AcceptorOptions) (*Acceptor, error) {
	if listener == nil {
		return nil, errors.New("TOT listener is required")
	}
	if err := options.defaults(); err != nil {
		return nil, err
	}
	a := &Acceptor{
		listener:  listener,
		options:   options,
		ready:     make(chan net.Conn, options.Backlog),
		errors:    make(chan error, 1),
		closed:    make(chan struct{}),
		handshake: make(chan struct{}, options.Backlog),
		sessions:  make(map[uint64]*managedSession),
		nonces:    make(map[string]time.Time),
	}
	go a.acceptLoop()
	go a.cleanupLoop()
	return a, nil
}

func (a *Acceptor) Accept() (net.Conn, error) {
	select {
	case conn := <-a.ready:
		return conn, nil
	case err := <-a.errors:
		if err == nil {
			return nil, ErrClosed
		}
		return nil, err
	case <-a.closed:
		return nil, ErrClosed
	}
}

func (a *Acceptor) Close() error {
	var err error
	a.closeOnce.Do(func() {
		close(a.closed)
		err = a.listener.Close()
		a.mu.Lock()
		for _, managed := range a.sessions {
			_ = managed.session.Close()
		}
		a.sessions = make(map[uint64]*managedSession)
		a.mu.Unlock()
	})
	return err
}

func (a *Acceptor) Addr() net.Addr { return a.listener.Addr() }

func (a *Acceptor) acceptLoop() {
	var tempDelay time.Duration
	for {
		conn, err := a.listener.Accept()
		if err != nil {
			select {
			case <-a.closed:
				return
			default:
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 100 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if tempDelay > time.Second {
					tempDelay = time.Second
				}
				time.Sleep(tempDelay)
				continue
			}
			select {
			case a.errors <- err:
			default:
			}
			return
		}
		tempDelay = 0
		select {
		case a.handshake <- struct{}{}:
			go a.acceptPath(conn)
		case <-a.closed:
			_ = conn.Close()
			return
		}
	}
}

func (a *Acceptor) acceptPath(conn net.Conn) {
	defer func() { <-a.handshake }()
	sessionID, err := ServerHandshake(conn, a.options.Handshake, a.recordNonce)
	if err != nil {
		_ = conn.Close()
		return
	}
	a.mu.Lock()
	managed := a.sessions[sessionID]
	fresh := managed == nil
	if fresh {
		sessionOptions := a.options.Session
		sessionOptions.Role = RoleServer
		managed = &managedSession{session: NewSession(sessionID, sessionOptions)}
		a.sessions[sessionID] = managed
	}
	managed.lastSeen = time.Now()
	a.mu.Unlock()
	if err := managed.session.AddPath(conn); err != nil {
		_ = conn.Close()
		if fresh {
			a.removeSession(sessionID, managed)
		}
		return
	}
	if fresh {
		select {
		case a.ready <- managed.session:
		case <-a.closed:
			a.removeSession(sessionID, managed)
		case <-time.After(a.options.Handshake.Timeout):
			a.removeSession(sessionID, managed)
		}
	}
}

func (a *Acceptor) recordNonce(nonce []byte) bool {
	key := string(nonce)
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if expiry, exists := a.nonces[key]; exists && expiry.After(now) {
		return false
	}
	a.nonces[key] = now.Add(2 * a.options.Handshake.MaxClockSkew)
	return true
}

func (a *Acceptor) cleanupLoop() {
	interval := a.options.IdleTTL / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < time.Second {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			a.mu.Lock()
			for nonce, expiry := range a.nonces {
				if !expiry.After(now) {
					delete(a.nonces, nonce)
				}
			}
			for id, managed := range a.sessions {
				stats := managed.session.Stats()
				if stats.ActivePaths == 0 && now.Sub(managed.lastSeen) >= a.options.IdleTTL {
					delete(a.sessions, id)
					_ = managed.session.Close()
				}
			}
			a.mu.Unlock()
		case <-a.closed:
			return
		}
	}
}

func (a *Acceptor) removeSession(id uint64, expected *managedSession) {
	a.mu.Lock()
	if a.sessions[id] == expected {
		delete(a.sessions, id)
	}
	a.mu.Unlock()
	_ = expected.session.Close()
}
