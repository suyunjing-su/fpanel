package nodehub

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/nodehub/crypto"
)

type ControllerStatus struct {
	Address             string `json:"address"`
	Active              bool   `json:"active"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastFailureAt       int64  `json:"lastFailureAt"`
	LastError           string `json:"lastError"`
}

type SystemInfo struct {
	Uptime             uint64             `json:"uptime"`
	BytesReceived      uint64             `json:"bytes_received"`
	BytesTransmitted   uint64             `json:"bytes_transmitted"`
	CPUUsage           float64            `json:"cpu_usage"`
	MemoryUsage        float64            `json:"memory_usage"`
	DiskUsage          float64            `json:"disk_usage"`
	ControllerStatuses []ControllerStatus `json:"controllers"`
	TOT                TOTTelemetry       `json:"tot"`
}

type TOTTelemetry struct {
	Sessions        int    `json:"sessions"`
	ActivePaths     int    `json:"activePaths"`
	PendingFrames   int    `json:"pendingFrames"`
	SentFrames      uint64 `json:"sentFrames"`
	ReceivedFrames  uint64 `json:"receivedFrames"`
	Retransmits     uint64 `json:"retransmits"`
	DuplicateFrames uint64 `json:"duplicateFrames"`
	PathFailures    uint64 `json:"pathFailures"`
}

type CommandMessage struct {
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"requestId,omitempty"`
}
type CommandResponse struct {
	Type      string `json:"type"`
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	Data      any    `json:"data,omitempty"`
	RequestID string `json:"requestId,omitempty"`
}

type TCPPingRequest struct {
	IP      string `json:"ip"`
	Port    int    `json:"port"`
	Count   int    `json:"count"`
	Timeout int    `json:"timeout"`
}

type TCPPingResponse struct {
	IP          string  `json:"ip"`
	Port        int     `json:"port"`
	Success     bool    `json:"success"`
	AverageTime float64 `json:"averageTime"`
	PacketLoss  float64 `json:"packetLoss"`
	Error       string  `json:"errorMessage,omitempty"`
}

type TransportPingRequest = TCPPingRequest
type TransportPingResponse = TCPPingResponse

type PortProbeRequest struct {
	Ports         []int  `json:"ports"`
	TCPListenAddr string `json:"tcpListenAddr"`
	UDPListenAddr string `json:"udpListenAddr"`
}

type PortProbeResult struct {
	Port      int    `json:"port"`
	Available bool   `json:"available"`
	Error     string `json:"error,omitempty"`
}

type PortProbeResponse struct {
	Results []PortProbeResult `json:"results"`
}
type envelope struct {
	Encrypted bool   `json:"encrypted"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

func decodeMessage(cipher *crypto.Cipher, payload []byte) ([]byte, error) {
	var wrapper envelope
	if err := json.Unmarshal(payload, &wrapper); err == nil && wrapper.Encrypted {
		if wrapper.Data == "" {
			return nil, fmt.Errorf("encrypted message has no data")
		}
		return cipher.Decrypt(wrapper.Data)
	}
	return payload, nil
}

func encodeMessage(cipher *crypto.Cipher, payload []byte) ([]byte, error) {
	data, err := cipher.Encrypt(payload)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{Encrypted: true, Data: data, Timestamp: time.Now().Unix()})
}
