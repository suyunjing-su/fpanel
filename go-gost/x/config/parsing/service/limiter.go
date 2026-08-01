package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/go-gost/core/limiter"
	traffic_limiter "github.com/go-gost/core/limiter/traffic"
	"github.com/go-gost/x/registry"
)

func composeTrafficLimiters(primary string, names []string) traffic_limiter.TrafficLimiter {
	all := make([]string, 0, len(names)+1)
	all = append(all, primary)
	all = append(all, names...)
	unique := make(map[string]struct{}, len(all))
	limiterNames := make([]string, 0, len(all))
	for _, name := range all {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := unique[name]; ok {
			continue
		}
		unique[name] = struct{}{}
		limiterNames = append(limiterNames, name)
	}
	if len(limiterNames) == 0 {
		return nil
	}
	if len(limiterNames) == 1 {
		return registry.TrafficLimiterRegistry().Get(limiterNames[0])
	}
	return &trafficLimiterGroup{names: limiterNames}
}

type trafficLimiterGroup struct {
	names []string
}

func (g *trafficLimiterGroup) In(ctx context.Context, key string, opts ...limiter.Option) traffic_limiter.Limiter {
	return &trafficLimiterGroupValue{ctx: ctx, key: key, options: opts, names: g.names, inbound: true}
}

func (g *trafficLimiterGroup) Out(ctx context.Context, key string, opts ...limiter.Option) traffic_limiter.Limiter {
	return &trafficLimiterGroupValue{ctx: ctx, key: key, options: opts, names: g.names}
}

type trafficLimiterGroupValue struct {
	ctx     context.Context
	key     string
	options []limiter.Option
	names   []string
	inbound bool
}

func (g *trafficLimiterGroupValue) limits() []traffic_limiter.Limiter {
	limits := make([]traffic_limiter.Limiter, 0, len(g.names))
	for _, name := range g.names {
		candidate := registry.TrafficLimiterRegistry().Get(name)
		if candidate == nil {
			continue
		}
		var limit traffic_limiter.Limiter
		if g.inbound {
			limit = candidate.In(g.ctx, g.key, g.options...)
		} else {
			limit = candidate.Out(g.ctx, g.key, g.options...)
		}
		if limit != nil {
			limits = append(limits, limit)
		}
	}
	sort.SliceStable(limits, func(left, right int) bool {
		return limits[left].Limit() < limits[right].Limit()
	})
	return limits
}

func (g *trafficLimiterGroupValue) Wait(ctx context.Context, n int) int {
	for _, limit := range g.limits() {
		if allowed := limit.Wait(ctx, n); allowed < n {
			n = allowed
		}
		if n == 0 {
			return 0
		}
	}
	return n
}

func (g *trafficLimiterGroupValue) Limit() int {
	limits := g.limits()
	if len(limits) == 0 {
		return 0
	}
	return limits[0].Limit()
}

func (g *trafficLimiterGroupValue) Set(int) {}

func (g *trafficLimiterGroupValue) String() string {
	return fmt.Sprintf("composed traffic limiter (%d)", len(g.names))
}
