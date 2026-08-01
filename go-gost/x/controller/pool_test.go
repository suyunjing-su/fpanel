package controller

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestPoolRotatesFromActiveAndTracksHealth(t *testing.T) {
	pool, err := New([]string{" http://primary/ ", "http://backup", "http://primary"})
	if err != nil {
		t.Fatal(err)
	}
	if got := pool.Candidates(); !reflect.DeepEqual(got, []string{"http://primary", "http://backup"}) {
		t.Fatalf("candidates = %#v", got)
	}
	pool.Fail("http://primary", errors.New("dial failed"))
	pool.Succeed("http://backup")
	if got := pool.Candidates(); !reflect.DeepEqual(got, []string{"http://backup", "http://primary"}) {
		t.Fatalf("promoted candidates = %#v", got)
	}
	statuses := pool.Status()
	if statuses[0].ConsecutiveFailures != 1 || statuses[0].LastError != "dial failed" || statuses[0].Active {
		t.Fatalf("primary status = %#v", statuses[0])
	}
	if !statuses[1].Active || statuses[1].LastSuccessAt == 0 {
		t.Fatalf("backup status = %#v", statuses[1])
	}
}

func TestPoolProbesHigherPriorityControllerAfterCooldown(t *testing.T) {
	pool, err := New([]string{"http://primary", "http://backup", "http://tertiary"})
	if err != nil {
		t.Fatal(err)
	}
	failure := time.Now().Add(-failbackProbeInterval - time.Second)
	pool.entries[0].ConsecutiveFailures = 1
	pool.entries[0].LastFailureAt = failure.UnixMilli()
	pool.entries[0].LastError = "dial failed"
	pool.entries[0].Active = false
	pool.entries[1].Active = true
	pool.active = 1

	got := pool.candidates(time.Now())
	want := []string{"http://primary", "http://backup", "http://tertiary"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
	pool.Succeed("http://primary")
	if got := pool.Candidates(); !reflect.DeepEqual(got, []string{"http://primary", "http://backup", "http://tertiary"}) {
		t.Fatalf("primary was not restored after successful probe: %#v", got)
	}
}

func TestPoolDefersFailedFailbackProbeUntilCooldown(t *testing.T) {
	pool, err := New([]string{"http://primary", "http://backup", "http://tertiary"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	pool.entries[0].ConsecutiveFailures = 1
	pool.entries[0].LastFailureAt = now.Add(-time.Second).UnixMilli()
	pool.entries[0].LastError = "dial failed"
	pool.entries[0].Active = false
	pool.entries[1].Active = true
	pool.active = 1

	got := pool.candidates(now)
	want := []string{"http://backup", "http://tertiary", "http://primary"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
}
