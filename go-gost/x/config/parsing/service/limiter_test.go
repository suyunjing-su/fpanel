package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-gost/core/limiter"
	traffic_limiter "github.com/go-gost/core/limiter/traffic"
	"github.com/go-gost/x/registry"
)

type limiterTestTrafficLimiter struct {
	in  traffic_limiter.Limiter
	out traffic_limiter.Limiter
}

func (l limiterTestTrafficLimiter) In(context.Context, string, ...limiter.Option) traffic_limiter.Limiter {
	return l.in
}

func (l limiterTestTrafficLimiter) Out(context.Context, string, ...limiter.Option) traffic_limiter.Limiter {
	return l.out
}

type limiterTestValue struct {
	limit int
	waits []int
}

func (l *limiterTestValue) Wait(_ context.Context, n int) int {
	l.waits = append(l.waits, n)
	if n > l.limit {
		return l.limit
	}
	return n
}

func (l *limiterTestValue) Limit() int    { return l.limit }
func (l *limiterTestValue) Set(value int) { l.limit = value }

func TestTrafficLimiterGroupComposesAndDeduplicatesNames(t *testing.T) {
	first := &limiterTestValue{limit: 100}
	second := &limiterTestValue{limit: 40}
	firstName := fmt.Sprintf("composition-first-%s", t.Name())
	secondName := fmt.Sprintf("composition-second-%s", t.Name())
	registerTestTrafficLimiter(t, firstName, limiterTestTrafficLimiter{in: first, out: first})
	registerTestTrafficLimiter(t, secondName, limiterTestTrafficLimiter{in: second, out: second})

	group := composeTrafficLimiters("  "+firstName+" ", []string{secondName, firstName, " "})
	if group == nil {
		t.Fatal("missing composed limiter")
	}
	inbound := group.In(context.Background(), "service", limiter.ScopeOption(limiter.ScopeService))
	if inbound == nil || inbound.Limit() != 40 {
		t.Fatalf("unexpected composed limit: %#v", inbound)
	}
	if got := inbound.Wait(context.Background(), 80); got != 40 {
		t.Fatalf("wait=%d, want 40", got)
	}
	if len(first.waits) != 1 || len(second.waits) != 1 {
		t.Fatalf("duplicate limiter was invoked: first=%v second=%v", first.waits, second.waits)
	}
}

func TestTrafficLimiterGroupReflectsRegistryReplacements(t *testing.T) {
	firstName := fmt.Sprintf("replacement-first-%s", t.Name())
	secondName := fmt.Sprintf("replacement-second-%s", t.Name())
	first := &limiterTestValue{limit: 100}
	second := &limiterTestValue{limit: 40}
	registerTestTrafficLimiter(t, firstName, limiterTestTrafficLimiter{in: first, out: first})
	registerTestTrafficLimiter(t, secondName, limiterTestTrafficLimiter{in: second, out: second})

	group := composeTrafficLimiters(firstName, []string{secondName})
	value := group.In(context.Background(), "service")
	if value.Limit() != 40 {
		t.Fatalf("initial limit=%d", value.Limit())
	}
	secondReplacement := &limiterTestValue{limit: 20}
	registry.TrafficLimiterRegistry().Unregister(secondName)
	if err := registry.TrafficLimiterRegistry().Register(secondName, limiterTestTrafficLimiter{in: secondReplacement, out: secondReplacement}); err != nil {
		t.Fatal(err)
	}
	if value.Limit() != 20 {
		t.Fatalf("replacement limit=%d, want 20", value.Limit())
	}
	if got := value.Wait(context.Background(), 80); got != 20 {
		t.Fatalf("replacement wait=%d, want 20", got)
	}
}

func registerTestTrafficLimiter(t *testing.T, name string, value traffic_limiter.TrafficLimiter) {
	t.Helper()
	if err := registry.TrafficLimiterRegistry().Register(name, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { registry.TrafficLimiterRegistry().Unregister(name) })
}
