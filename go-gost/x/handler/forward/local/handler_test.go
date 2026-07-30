package local

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/go-gost/core/chain"
	"github.com/go-gost/core/handler"
	xhop "github.com/go-gost/x/hop"
	xlogger "github.com/go-gost/x/logger"
	xselector "github.com/go-gost/x/selector"
)

func TestHandlerFallsThroughMultipleFIFOEndpoints(t *testing.T) {
	nodes := []*chain.Node{
		chain.NewNode("endpoint-1", "127.0.0.1:10001"),
		chain.NewNode("endpoint-2", "127.0.0.1:10002"),
		chain.NewNode("endpoint-3", "127.0.0.1:10003"),
	}
	hp := xhop.NewHop(
		xhop.NodeOption(nodes...),
		xhop.SelectorOption(xselector.NewSelector(
			xselector.FIFOStrategy[*chain.Node](),
			xselector.FailFilter[*chain.Node](1, time.Hour),
		)),
		xhop.LoggerOption(xlogger.NewLogger()),
	)
	router := &sequenceRouter{errors: []error{
		errors.New("endpoint-1 down"),
		errors.New("endpoint-2 down"),
		nil,
	}}
	h := NewHandler(handler.RouterOption(router)).(*forwardHandler)
	h.Forward(hp)

	client, server := net.Pipe()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- h.Handle(context.Background(), server)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Handle() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Handle() did not complete")
	}

	if router.calls != 3 {
		t.Fatalf("Dial() calls = %d, want 3", router.calls)
	}
	if nodes[0].Marker().Count() == 0 || nodes[1].Marker().Count() == 0 {
		t.Fatal("failed endpoints were not marked")
	}
	if nodes[2].Marker().Count() != 0 {
		t.Fatal("successful endpoint remained failed")
	}
}

type sequenceRouter struct {
	errors []error
	calls  int
}

func (r *sequenceRouter) Options() *chain.RouterOptions {
	return &chain.RouterOptions{}
}

func (r *sequenceRouter) Dial(context.Context, string, string) (net.Conn, error) {
	index := r.calls
	r.calls++
	if index < len(r.errors) && r.errors[index] != nil {
		return nil, r.errors[index]
	}
	left, right := net.Pipe()
	right.Close()
	return left, nil
}

func (r *sequenceRouter) Bind(context.Context, string, string, ...chain.BindOption) (net.Listener, error) {
	return nil, errors.New("not implemented")
}
