package speedlimits

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCreateRequestAcceptsDisplayTunnelName(t *testing.T) {
	var request CreateRequest
	decoder := json.NewDecoder(bytes.NewBufferString(`{"name":"standard","speed":100,"tunnelId":7,"tunnelName":"primary","status":1}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("decode speed limit request: %v", err)
	}
	if request.TunnelID != 7 || request.TunnelName != "primary" {
		t.Fatalf("unexpected request: %#v", request)
	}
}
