package wrapper

import (
	"errors"
	"net"
	"sync"
	"syscall"

	limiter "github.com/go-gost/core/limiter/conn"
	"github.com/go-gost/core/metadata"
)

var (
	errUnsupport = errors.New("unsupported operation")
)

// serverConn is a server side Conn with metrics supported.
type serverConn struct {
	net.Conn
	limiter   limiter.Limiter
	closeErr  error
	closeOnce sync.Once
}

func WrapConn(limiter limiter.Limiter, c net.Conn) net.Conn {
	if limiter == nil {
		return c
	}
	return &serverConn{
		Conn:    c,
		limiter: limiter,
	}
}

func (c *serverConn) SyscallConn() (rc syscall.RawConn, err error) {
	if sc, ok := c.Conn.(syscall.Conn); ok {
		rc, err = sc.SyscallConn()
		return
	}
	err = errUnsupport
	return
}

func (c *serverConn) Close() error {
	c.closeOnce.Do(func() {
		c.limiter.Allow(-1)
		c.closeErr = c.Conn.Close()
	})
	return c.closeErr
}

func (c *serverConn) IsIdle() bool {
	if idle, ok := c.Conn.(interface{ IsIdle() bool }); ok {
		return idle.IsIdle()
	}
	return false
}

func (c *serverConn) SetIdle(idle bool) {
	if current, ok := c.Conn.(interface{ SetIdle(bool) }); ok {
		current.SetIdle(idle)
	}
}

func (c *serverConn) Unwrap() net.Conn { return c.Conn }

func (c *serverConn) Metadata() metadata.Metadata {
	if md, ok := c.Conn.(metadata.Metadatable); ok {
		return md.Metadata()
	}
	return nil
}
