package tot

import (
	"context"
	"net"
	"time"

	"github.com/go-gost/core/limiter"
	"github.com/go-gost/core/listener"
	"github.com/go-gost/core/logger"
	md "github.com/go-gost/core/metadata"
	admission "github.com/go-gost/x/admission/wrapper"
	xnet "github.com/go-gost/x/internal/net"
	"github.com/go-gost/x/internal/net/proxyproto"
	coretot "github.com/go-gost/x/internal/util/tot"
	climiter "github.com/go-gost/x/limiter/conn/wrapper"
	limiterwrapper "github.com/go-gost/x/limiter/traffic/wrapper"
	metrics "github.com/go-gost/x/metrics/wrapper"
	stats "github.com/go-gost/x/observer/stats/wrapper"
	"github.com/go-gost/x/registry"
)

func init() {
	registry.ListenerRegistry().Register("tot", NewListener)
}

type totListener struct {
	ln       net.Listener
	acceptor *coretot.Acceptor
	logger   logger.Logger
	md       metadata
	options  listener.Options
}

func NewListener(opts ...listener.Option) listener.Listener {
	options := listener.Options{}
	for _, opt := range opts {
		opt(&options)
	}
	return &totListener{logger: options.Logger, options: options}
}

func (l *totListener) Init(md md.Metadata) error {
	if err := l.parseMetadata(md); err != nil {
		return err
	}
	network := "tcp"
	if xnet.IsIPv4(l.options.Addr) {
		network = "tcp4"
	}
	listenConfig := net.ListenConfig{}
	if l.md.mptcp {
		listenConfig.SetMultipathTCP(true)
	}
	ln, err := listenConfig.Listen(context.Background(), network, l.options.Addr)
	if err != nil {
		return err
	}
	ln = proxyproto.WrapListener(l.options.ProxyProtocol, ln, 10*time.Second)
	ln = metrics.WrapListener(l.options.Service, ln)
	ln = stats.WrapListener(ln, l.options.Stats)
	ln = admission.WrapListener(l.options.Admission, ln)
	ln = limiterwrapper.WrapListener(l.options.Service, ln, l.options.TrafficLimiter)
	acceptor, err := coretot.NewAcceptor(ln, coretot.AcceptorOptions{
		Session:   l.md.session,
		Handshake: l.md.handshake,
		Backlog:   l.md.backlog,
		IdleTTL:   l.md.idleTTL,
	})
	if err != nil {
		_ = ln.Close()
		return err
	}
	l.ln = climiter.WrapListener(l.options.ConnLimiter, acceptor)
	l.acceptor = acceptor
	return nil
}

func (l *totListener) Accept() (net.Conn, error) {
	conn, err := l.ln.Accept()
	if err != nil {
		return nil, err
	}
	return limiterwrapper.WrapConn(
		conn,
		l.options.TrafficLimiter,
		conn.RemoteAddr().String(),
		limiter.ScopeOption(limiter.ScopeConn),
		limiter.ServiceOption(l.options.Service),
		limiter.NetworkOption(conn.LocalAddr().Network()),
		limiter.SrcOption(conn.RemoteAddr().String()),
	), nil
}

func (l *totListener) Addr() net.Addr {
	if l.acceptor != nil {
		return l.acceptor.Addr()
	}
	return nil
}

func (l *totListener) Close() error {
	if l.acceptor != nil {
		return l.acceptor.Close()
	}
	if l.ln != nil {
		return l.ln.Close()
	}
	return nil
}
