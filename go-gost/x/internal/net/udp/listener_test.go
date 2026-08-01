package udp

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	connlimiter "github.com/go-gost/core/limiter/conn"
)

func TestListenerConnLimiterReleasesTokenWhenQueueFull(t *testing.T) {
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packetConn.Close()

	lim := &countingLimiter{limit: 1}
	ln := newTestListener(packetConn, 0, &testConnLimiter{limiter: lim})
	defer ln.connPool.Close()

	if c := ln.getConn(&net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10001}); c != nil {
		t.Fatal("queue-full UDP virtual connection was accepted")
	}
	if got := lim.current.Load(); got != 0 {
		t.Fatalf("limiter token was not released after queue rejection: current=%d", got)
	}
}

func TestListenerConnLimiterReusesQueuedVirtualConnection(t *testing.T) {
	packetConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer packetConn.Close()

	lim := &countingLimiter{limit: 1}
	ln := newTestListener(packetConn, 1, &testConnLimiter{limiter: lim})
	defer ln.connPool.Close()

	raddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 10002}
	first := ln.getConn(raddr)
	if first == nil {
		t.Fatal("first UDP virtual connection was rejected")
	}
	second := ln.getConn(raddr)
	if second != first {
		t.Fatal("existing UDP virtual connection was not reused")
	}
	if got := lim.current.Load(); got != 1 {
		t.Fatalf("limiter token count changed while reusing connection: current=%d", got)
	}

	queued := <-ln.cqueue
	if err := queued.Close(); err != nil {
		t.Fatal(err)
	}
	if err := queued.Close(); err != nil {
		t.Fatal(err)
	}
	if got := lim.current.Load(); got != 0 {
		t.Fatalf("limiter token was not released exactly once: current=%d", got)
	}
}

func newTestListener(packetConn net.PacketConn, backlog int, limiter connlimiter.ConnLimiter) *listener {
	return &listener{
		conn:     packetConn,
		cqueue:   make(chan net.Conn, backlog),
		connPool: newConnPool(time.Hour),
		closed:   make(chan struct{}),
		errChan:  make(chan error, 1),
		config: &ListenConfig{
			ReadQueueSize: 1,
			Keepalive:     true,
			TTL:           time.Hour,
			ConnLimiter:   limiter,
		},
	}
}

type testConnLimiter struct {
	limiter connlimiter.Limiter
}

func (l *testConnLimiter) Limiter(string) connlimiter.Limiter {
	return l.limiter
}

type countingLimiter struct {
	limit   int64
	current atomic.Int64
}

func (l *countingLimiter) Allow(n int) bool {
	current := l.current.Add(int64(n))
	if n > 0 && current > l.limit {
		l.current.Add(-int64(n))
		return false
	}
	return true
}

func (l *countingLimiter) Limit() int {
	return int(l.limit)
}
