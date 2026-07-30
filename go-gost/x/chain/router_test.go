package chain

import (
	"context"
	"errors"
	"net"
	"testing"

	corechain "github.com/go-gost/core/chain"
	"github.com/go-gost/core/selector"
	xlogger "github.com/go-gost/x/logger"
	xselector "github.com/go-gost/x/selector"
)

func TestRouterDialRetriesFailedFIFOChain(t *testing.T) {
	primary := &markedChainer{name: "primary", route: errorRoute{}}
	backup := &markedChainer{name: "backup", route: successRoute{}}
	group := NewChainGroup(primary, backup).WithSelector(xselector.NewSelector(
		xselector.FIFOStrategy[corechain.Chainer](),
		xselector.FailFilter[corechain.Chainer](1, 0),
	))
	router := NewRouter(
		corechain.ChainRouterOption(group),
		corechain.RetriesRouterOption(1),
		corechain.LoggerRouterOption(xlogger.NewLogger()),
	)

	conn, err := router.Dial(context.Background(), "tcp", "192.0.2.1:443")
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	if primary.calls != 1 {
		t.Fatalf("primary Route() calls = %d, want 1", primary.calls)
	}
	if backup.calls != 1 {
		t.Fatalf("backup Route() calls = %d, want 1", backup.calls)
	}
}

type markedChainer struct {
	name   string
	route  corechain.Route
	marker selector.Marker
	calls  int
}

func (c *markedChainer) Route(context.Context, string, string, ...corechain.RouteOption) corechain.Route {
	c.calls++
	return &markedRoute{Route: c.route, marker: c.Marker()}
}

func (c *markedChainer) Marker() selector.Marker {
	if c.marker == nil {
		c.marker = selector.NewFailMarker()
	}
	return c.marker
}

func (c *markedChainer) Name() string {
	return c.name
}

type markedRoute struct {
	corechain.Route
	marker selector.Marker
}

func (r *markedRoute) Dial(ctx context.Context, network, address string, opts ...corechain.DialOption) (net.Conn, error) {
	conn, err := r.Route.Dial(ctx, network, address, opts...)
	if err != nil {
		r.marker.Mark()
	} else {
		r.marker.Reset()
	}
	return conn, err
}

type errorRoute struct{}

func (errorRoute) Dial(context.Context, string, string, ...corechain.DialOption) (net.Conn, error) {
	return nil, errors.New("primary unavailable")
}

func (errorRoute) Bind(context.Context, string, string, ...corechain.BindOption) (net.Listener, error) {
	return nil, errors.New("primary unavailable")
}

func (errorRoute) Nodes() []*corechain.Node {
	return nil
}

type successRoute struct{}

func (successRoute) Dial(context.Context, string, string, ...corechain.DialOption) (net.Conn, error) {
	left, right := net.Pipe()
	right.Close()
	return left, nil
}

func (successRoute) Bind(context.Context, string, string, ...corechain.BindOption) (net.Listener, error) {
	return nil, errors.New("not implemented")
}

func (successRoute) Nodes() []*corechain.Node {
	return nil
}
