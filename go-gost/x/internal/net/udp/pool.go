package udp

import (
	"net"
	"sync"
	"time"

	"github.com/go-gost/core/logger"
)

type connPool struct {
	m      sync.Map
	ttl    time.Duration
	closed chan struct{}
	logger logger.Logger
}

func newConnPool(ttl time.Duration) *connPool {
	p := &connPool{
		ttl:    ttl,
		closed: make(chan struct{}),
	}
	go p.idleCheck()
	return p
}

func (p *connPool) WithLogger(logger logger.Logger) *connPool {
	p.logger = logger
	return p
}

func (p *connPool) Get(key any) (c net.Conn, ok bool) {
	if p == nil {
		return
	}

	v, ok := p.m.Load(key)
	if ok {
		c, ok = v.(net.Conn)
	}
	return
}

func (p *connPool) Set(key any, c net.Conn) {
	if p == nil {
		return
	}

	p.m.Store(key, c)
}

func (p *connPool) Delete(key any) {
	if p == nil {
		return
	}
	p.m.Delete(key)
}

func (p *connPool) Close() {
	if p == nil {
		return
	}

	select {
	case <-p.closed:
		return
	default:
	}

	close(p.closed)

	p.m.Range(func(k, v any) bool {
		if c, ok := v.(net.Conn); ok && c != nil {
			c.Close()
		}
		return true
	})
}

func (p *connPool) idleCheck() {
	ticker := time.NewTicker(p.ttl)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			size := 0
			idles := 0
			p.m.Range(func(key, value any) bool {
				c, ok := value.(net.Conn)
				if !ok || c == nil {
					p.Delete(key)
					return true
				}
				size++

				if idle, ok := c.(interface{ IsIdle() bool }); ok && idle.IsIdle() {
					idles++
					p.Delete(key)
					c.Close()
					return true
				}

				if idle, ok := c.(interface{ SetIdle(bool) }); ok {
					idle.SetIdle(true)
				}

				return true
			})

			if idles > 0 && p.logger != nil {
				p.logger.Debugf("connection pool: size=%d, idle=%d", size, idles)
			}
		case <-p.closed:
			return
		}
	}
}
