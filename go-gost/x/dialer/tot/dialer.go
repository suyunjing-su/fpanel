package tot

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/go-gost/core/dialer"
	"github.com/go-gost/core/logger"
	md "github.com/go-gost/core/metadata"
	coretot "github.com/go-gost/x/internal/util/tot"
	"github.com/go-gost/x/registry"
)

func init() {
	registry.DialerRegistry().Register("tot", NewDialer)
}

type networkDialer interface {
	Dial(context.Context, string, string) (net.Conn, error)
}

type pathTarget struct {
	key     string
	address string
}

type sessionState struct {
	session *coretot.Session
	cancel  context.CancelFunc
}

type totDialer struct {
	mu       sync.Mutex
	sessions map[string]*sessionState
	md       metadata
	logger   logger.Logger
}

func NewDialer(opts ...dialer.Option) dialer.Dialer {
	options := &dialer.Options{}
	for _, opt := range opts {
		opt(options)
	}
	return &totDialer{sessions: make(map[string]*sessionState), logger: options.Logger}
}

func (d *totDialer) Init(md md.Metadata) error { return d.parseMetadata(md) }

func (d *totDialer) Dial(ctx context.Context, addr string, opts ...dialer.DialOption) (net.Conn, error) {
	var options dialer.DialOptions
	for _, opt := range opts {
		opt(&options)
	}
	if options.Dialer == nil {
		return nil, errors.New("TOT network dialer is required")
	}
	key := addr
	d.mu.Lock()
	state := d.sessions[key]
	if state != nil && state.session.IsClosed() {
		delete(d.sessions, key)
		state.cancel()
		state = nil
	}
	if state == nil {
		id, err := coretot.NewSessionID()
		if err != nil {
			d.mu.Unlock()
			return nil, err
		}
		sessionCtx, cancel := context.WithCancel(context.Background())
		state = &sessionState{session: coretot.NewSession(id, d.md.session), cancel: cancel}
		d.sessions[key] = state
		go d.maintainPaths(sessionCtx, state.session, addr, options.Dialer)
	}
	d.mu.Unlock()
	if err := d.ensureInitialPath(ctx, state.session, addr, options.Dialer); err != nil {
		d.mu.Lock()
		if d.sessions[key] == state && state.session.Stats().ActivePaths == 0 {
			delete(d.sessions, key)
			state.cancel()
			_ = state.session.Close()
		}
		d.mu.Unlock()
		return nil, err
	}
	return state.session, nil
}

func (d *totDialer) Handshake(_ context.Context, conn net.Conn, _ ...dialer.HandshakeOption) (net.Conn, error) {
	if _, ok := conn.(*coretot.Session); !ok {
		return nil, errors.New("TOT unrecognized connection")
	}
	return conn, nil
}

func (d *totDialer) ensureInitialPath(ctx context.Context, session *coretot.Session, addr string, netDialer networkDialer) error {
	var lastErr error
	for _, target := range d.targets(addr) {
		if session.HasPath(target.key) {
			return nil
		}
		if err := d.addPath(ctx, session, target, netDialer); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	if lastErr == nil {
		lastErr = coretot.ErrNoPath
	}
	return lastErr
}

func (d *totDialer) maintainPaths(ctx context.Context, session *coretot.Session, addr string, netDialer networkDialer) {
	ticker := time.NewTicker(d.md.recoveryPeriod)
	defer ticker.Stop()
	for {
		d.reconcilePaths(ctx, session, addr, netDialer)
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (d *totDialer) reconcilePaths(ctx context.Context, session *coretot.Session, addr string, netDialer networkDialer) {
	for _, target := range d.targets(addr) {
		if session.HasPath(target.key) {
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, d.handshakeTimeout())
		err := d.addPath(attemptCtx, session, target, netDialer)
		cancel()
		if err != nil && d.logger != nil {
			d.logger.Warnf("TOT path %s unavailable: %v", target.address, err)
		}
	}
}

func (d *totDialer) addPath(ctx context.Context, session *coretot.Session, target pathTarget, netDialer networkDialer) error {
	conn, err := netDialer.Dial(ctx, "tcp", target.address)
	if err != nil {
		return err
	}
	if err := coretot.ClientHandshake(conn, session.ID(), d.md.handshake); err != nil {
		_ = conn.Close()
		return err
	}
	if err := session.AddNamedPath(target.key, conn); err != nil {
		_ = conn.Close()
		return err
	}
	return nil
}

func (d *totDialer) targets(addr string) []pathTarget {
	addresses := d.md.paths
	if len(addresses) == 0 {
		addresses = make([]string, d.md.pathCount)
		for index := range addresses {
			addresses[index] = addr
		}
	}
	targets := make([]pathTarget, len(addresses))
	for index, address := range addresses {
		targets[index] = pathTarget{key: address + "#" + strconv.Itoa(index+1), address: address}
	}
	return targets
}

func (d *totDialer) handshakeTimeout() time.Duration {
	if d.md.handshake.Timeout > 0 {
		return d.md.handshake.Timeout
	}
	return 5 * time.Second
}
