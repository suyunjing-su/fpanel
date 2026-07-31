package hop

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/go-gost/core/chain"
	"github.com/go-gost/core/routing"
	xlogger "github.com/go-gost/x/logger"
	xselector "github.com/go-gost/x/selector"
)

func TestFIFOSelectSkipsFailedEndpointsAndRestoresPriority(t *testing.T) {
	nodes := []*chain.Node{
		chain.NewNode("endpoint-1", "127.0.0.1:10001"),
		chain.NewNode("endpoint-2", "127.0.0.1:10002"),
		chain.NewNode("endpoint-3", "127.0.0.1:10003"),
	}
	h := newTestHop(nodes)

	assertSelected(t, h, nodes[0])
	nodes[0].Marker().Mark()
	assertSelected(t, h, nodes[1])
	nodes[1].Marker().Mark()
	assertSelected(t, h, nodes[2])
	nodes[0].Marker().Reset()
	assertSelected(t, h, nodes[0])
}

func TestProbeRestoresRecoveredEndpoint(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	primary := chain.NewNode("primary", listener.Addr().String())
	backup := chain.NewNode("backup", "127.0.0.1:1")
	h := newTestHop([]*chain.Node{primary, backup})

	primary.Marker().Mark()
	assertSelected(t, h, backup)

	h.probe(context.Background())
	assertSelected(t, h, primary)
}

func TestMatchedPriorityEndpointsStillFailOver(t *testing.T) {
	matcher := routingMatcherFunc(func(*routing.Request) bool { return true })
	nodes := []*chain.Node{
		chain.NewNode("matched-primary", "127.0.0.1:10001", chain.MatcherNodeOption(matcher), chain.PriorityNodeOption(100)),
		chain.NewNode("matched-backup", "127.0.0.1:10002", chain.MatcherNodeOption(matcher), chain.PriorityNodeOption(100)),
		chain.NewNode("lower-priority", "127.0.0.1:10003", chain.MatcherNodeOption(matcher), chain.PriorityNodeOption(50)),
	}
	h := newTestHop(nodes)

	assertSelected(t, h, nodes[0])
	nodes[0].Marker().Mark()
	assertSelected(t, h, nodes[1])
	nodes[1].Marker().Mark()
	assertSelected(t, h, nodes[2])
}

type routingMatcherFunc func(*routing.Request) bool

func (f routingMatcherFunc) Match(request *routing.Request) bool { return f(request) }

func newTestHop(nodes []*chain.Node) *chainHop {
	return &chainHop{
		nodes: nodes,
		options: options{
			selector: xselector.NewSelector(
				xselector.FIFOStrategy[*chain.Node](),
				xselector.FailFilter[*chain.Node](1, time.Hour),
			),
			probeTimeout: 100 * time.Millisecond,
			logger:       xlogger.NewLogger(),
		},
	}
}

func assertSelected(t *testing.T, h *chainHop, want *chain.Node) {
	t.Helper()
	if got := h.Select(context.Background()); got != want {
		t.Fatalf("selected %s, want %s", nodeName(got), nodeName(want))
	}
}

func nodeName(node *chain.Node) string {
	if node == nil {
		return "<nil>"
	}
	return node.Name
}
