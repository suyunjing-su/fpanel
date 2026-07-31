package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-gost/x/controller"
)

func TestPostReportFallsBackAndPromotesController(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer primary.Close()
	var received []byte
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer backup.Close()
	pool, err := controller.New([]string{primary.URL, backup.URL})
	if err != nil {
		t.Fatal(err)
	}
	SetHTTPReportControllers(pool, "")
	success, err := postReport(context.Background(), "/flow/upload", []byte("payload"), "test", time.Second)
	if err != nil || !success {
		t.Fatalf("post report failed: success=%v err=%v", success, err)
	}
	if string(received) != "payload" {
		t.Fatalf("backup received %q", received)
	}
	if got := pool.Candidates(); !reflect.DeepEqual(got, []string{backup.URL, primary.URL}) {
		t.Fatalf("controllers were not promoted: %#v", got)
	}
	statuses := pool.Status()
	if statuses[0].ConsecutiveFailures != 1 || !statuses[1].Active {
		t.Fatalf("unexpected controller statuses: %#v", statuses)
	}
}
