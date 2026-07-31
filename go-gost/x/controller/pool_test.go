package controller

import (
	"errors"
	"reflect"
	"testing"
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
