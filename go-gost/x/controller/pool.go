package controller

import (
	"errors"
	"strings"
	"sync"
	"time"
)

type Status struct {
	Address             string `json:"address"`
	Active              bool   `json:"active"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastFailureAt       int64  `json:"lastFailureAt"`
	LastError           string `json:"lastError"`
}

type Pool struct {
	mu      sync.RWMutex
	entries []Status
	active  int
}

func New(addresses []string) (*Pool, error) {
	normalized := Normalize(addresses)
	if len(normalized) == 0 {
		return nil, errors.New("at least one controller address is required")
	}
	entries := make([]Status, len(normalized))
	for index, address := range normalized {
		entries[index] = Status{Address: address, Active: index == 0}
	}
	return &Pool{entries: entries}, nil
}

func Normalize(addresses []string) []string {
	seen := make(map[string]struct{}, len(addresses))
	result := make([]string, 0, len(addresses))
	for _, address := range addresses {
		address = strings.TrimRight(strings.TrimSpace(address), "/")
		if address == "" {
			continue
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		result = append(result, address)
	}
	return result
}

func (p *Pool) Candidates() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.entries) == 0 {
		return nil
	}
	result := make([]string, 0, len(p.entries))
	for offset := 0; offset < len(p.entries); offset++ {
		index := (p.active + offset) % len(p.entries)
		result = append(result, p.entries[index].Address)
	}
	return result
}

func (p *Pool) Succeed(address string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.entries {
		entry := &p.entries[index]
		entry.Active = entry.Address == address
		if entry.Address == address {
			entry.ConsecutiveFailures = 0
			entry.LastSuccessAt = time.Now().UnixMilli()
			entry.LastError = ""
			p.active = index
		}
	}
}

func (p *Pool) Fail(address string, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for index := range p.entries {
		entry := &p.entries[index]
		if entry.Address != address {
			continue
		}
		entry.ConsecutiveFailures++
		entry.LastFailureAt = time.Now().UnixMilli()
		if err != nil {
			entry.LastError = err.Error()
		}
		return
	}
}

func (p *Pool) Status() []Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	result := make([]Status, len(p.entries))
	copy(result, p.entries)
	return result
}
