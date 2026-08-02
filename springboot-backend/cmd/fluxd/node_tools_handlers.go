package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/siteconfig"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
)

const (
	installReleaseVersion = "3.0.28-beta"
	diagnosisPingCount    = 4
	diagnosisPingTimeout  = 5000
	externalPingHost      = "www.google.com"
	externalPingPort      = 443
)

type tcpPinger interface {
	TCPPing(context.Context, int64, nodehub.TCPPingRequest) (nodehub.TCPPingResponse, error)
}

type transportPinger interface {
	UDPPing(context.Context, int64, nodehub.TransportPingRequest) (nodehub.TransportPingResponse, error)
	QUICPing(context.Context, int64, nodehub.TransportPingRequest) (nodehub.TransportPingResponse, error)
	KCPPing(context.Context, int64, nodehub.TransportPingRequest) (nodehub.TransportPingResponse, error)
}

type diagnosisResult struct {
	TunnelName  string                 `json:"tunnelName,omitempty"`
	TunnelType  string                 `json:"tunnelType,omitempty"`
	ForwardName string                 `json:"forwardName,omitempty"`
	Timestamp   int64                  `json:"timestamp"`
	Results     []diagnosisCheckResult `json:"results"`
}

type diagnosisCheckResult struct {
	Success       bool    `json:"success"`
	Description   string  `json:"description"`
	NodeName      string  `json:"nodeName"`
	NodeID        string  `json:"nodeId"`
	TargetIP      string  `json:"targetIp"`
	TargetPort    int     `json:"targetPort,omitempty"`
	Message       string  `json:"message,omitempty"`
	AverageTime   float64 `json:"averageTime,omitempty"`
	PacketLoss    float64 `json:"packetLoss,omitempty"`
	FromChainType int     `json:"fromChainType,omitempty"`
	FromInx       int     `json:"fromInx,omitempty"`
	ToChainType   int     `json:"toChainType,omitempty"`
	ToInx         int     `json:"toInx,omitempty"`
}

type diagnosisNode struct {
	Spec tunnels.NodeSpec
	Node nodes.Node
}

func registerNodeToolsRoutes(mux *http.ServeMux, nodeRepo *nodes.Repository, tunnelRepo *tunnels.Repository, forwardRepo *forwards.Repository, configRepo *siteconfig.Repository, pinger tcpPinger, isAdmin func(*http.Request) bool) {
	mux.HandleFunc("POST /api/v1/node/install", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			ID int64 `json:"id"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		secret, err := nodeRepo.SecretForInstall(r.Context(), request.ID)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, "节点不存在"))
			return
		}
		serverAddress, err := installServerAddress(r.Context(), configRepo)
		if err != nil {
			httpapi.WriteJSON(w, http.StatusBadRequest, httpapi.Failure(http.StatusBadRequest, err.Error()))
			return
		}
		command := verifiedInstallCommand(serverAddress, secret)
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(command))
	})

	mux.HandleFunc("POST /api/v1/tunnel/diagnose", func(w http.ResponseWriter, r *http.Request) {
		if !isAdmin(r) {
			httpapi.WriteJSON(w, http.StatusForbidden, httpapi.Failure(http.StatusForbidden, "无权限"))
			return
		}
		var request struct {
			TunnelID int64 `json:"tunnelId"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		result, err := diagnoseTunnel(r.Context(), tunnelRepo, nodeRepo, pinger, request.TunnelID)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(result))
	})

	mux.HandleFunc("POST /api/v1/forward/diagnose", func(w http.ResponseWriter, r *http.Request) {
		identity, ok := httpapi.IdentityFromContext(r.Context())
		if !ok {
			httpapi.WriteJSON(w, http.StatusUnauthorized, httpapi.Failure(http.StatusUnauthorized, "未登录或token已过期"))
			return
		}
		var request struct {
			ForwardID int64 `json:"forwardId"`
		}
		if !httpapi.DecodeJSON(w, r, &request) {
			return
		}
		result, err := diagnoseForward(r.Context(), forwardRepo, tunnelRepo, nodeRepo, pinger, request.ForwardID, identity.UserID, identity.RoleID == 0)
		if err != nil {
			status := http.StatusBadRequest
			if errors.Is(err, sql.ErrNoRows) {
				status = http.StatusNotFound
			}
			httpapi.WriteJSON(w, status, httpapi.Failure(status, err.Error()))
			return
		}
		httpapi.WriteJSON(w, http.StatusOK, httpapi.Success(result))
	})
}

func installServerAddress(ctx context.Context, configRepo *siteconfig.Repository) (string, error) {
	rawHost, err := configRepo.Get(ctx, "ip")
	if err != nil {
		return "", err
	}
	rawHost = strings.TrimSpace(rawHost)
	if rawHost == "" {
		return "", errors.New("请先前往网站配置中设置ip")
	}
	protocol, err := configRepo.Get(ctx, "protocol_type")
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(strings.TrimSpace(protocol), "https") {
		return "", errors.New("节点控制面必须使用 HTTPS")
	}
	return secureControlAddress(rawHost)
}

func secureControlAddress(rawHost string) (string, error) {
	value := strings.TrimSpace(rawHost)
	if value == "" {
		return "", errors.New("网站配置不能为空")
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", errors.New("网站配置必须是无路径、查询参数或凭据的 HTTPS 地址")
	}
	return "https://" + parsed.Host, nil
}

func verifiedInstallCommand(serverAddress, secret string) string {
	baseURL := "https://github.com/suyunjing-su/fpanel/releases/download/" + installReleaseVersion
	return fmt.Sprintf(`(tmp=$(mktemp -d) && trap 'rm -rf "$tmp"' EXIT && curl --fail --location --retry 3 --proto '=https' --tlsv1.2 %s/SHA256SUMS -o "$tmp/SHA256SUMS" && curl --fail --location --retry 3 --proto '=https' --tlsv1.2 %s/install.sh -o "$tmp/install.sh" && (cd "$tmp" && sha256sum --check --ignore-missing SHA256SUMS && test "$(awk '$2 == "install.sh" {print $1; exit}' SHA256SUMS)" = "$(sha256sum install.sh | awk '{print $1}')") && chmod 0755 "$tmp/install.sh" && bash "$tmp/install.sh" -a %s -s %s)`, shellQuote(baseURL), shellQuote(baseURL), shellQuote(serverAddress), shellQuote(secret))
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func diagnoseTunnel(ctx context.Context, tunnelRepo *tunnels.Repository, nodeRepo *nodes.Repository, pinger tcpPinger, tunnelID int64) (diagnosisResult, error) {
	tunnel, err := tunnelRepo.Get(ctx, tunnelID)
	if err != nil {
		return diagnosisResult{}, err
	}
	entryNodes, err := loadDiagnosisNodes(ctx, nodeRepo, tunnel.InNodeID)
	if err != nil {
		return diagnosisResult{}, err
	}
	chainNodes, err := loadDiagnosisChainNodes(ctx, nodeRepo, tunnel.ChainNodes)
	if err != nil {
		return diagnosisResult{}, err
	}
	exitNodes, err := loadDiagnosisNodes(ctx, nodeRepo, tunnel.OutNodeID)
	if err != nil {
		return diagnosisResult{}, err
	}
	checks := buildTunnelDiagnosisChecks(entryNodes, chainNodes, exitNodes, []hostPort{{Host: externalPingHost, Port: externalPingPort}})
	return diagnosisResult{TunnelName: tunnel.Name, TunnelType: tunnelTypeName(tunnel.Type), Timestamp: time.Now().UnixMilli(), Results: runDiagnosisChecks(ctx, pinger, checks)}, nil
}

func diagnoseForward(ctx context.Context, forwardRepo *forwards.Repository, tunnelRepo *tunnels.Repository, nodeRepo *nodes.Repository, pinger tcpPinger, forwardID, userID int64, admin bool) (diagnosisResult, error) {
	forward, err := forwardRepo.Get(ctx, forwardID, userID, admin)
	if err != nil {
		return diagnosisResult{}, err
	}
	tunnel, err := tunnelRepo.Get(ctx, forward.TunnelID)
	if err != nil {
		return diagnosisResult{}, err
	}
	entryNodes, err := loadDiagnosisNodes(ctx, nodeRepo, tunnel.InNodeID)
	if err != nil {
		return diagnosisResult{}, err
	}
	chainNodes, err := loadDiagnosisChainNodes(ctx, nodeRepo, tunnel.ChainNodes)
	if err != nil {
		return diagnosisResult{}, err
	}
	exitNodes, err := loadDiagnosisNodes(ctx, nodeRepo, tunnel.OutNodeID)
	if err != nil {
		return diagnosisResult{}, err
	}
	remotes, err := parseRemoteTargets(forward.RemoteAddr)
	if err != nil {
		return diagnosisResult{}, err
	}
	checks := buildTunnelDiagnosisChecks(entryNodes, chainNodes, exitNodes, remotes)
	return diagnosisResult{ForwardName: forward.Name, Timestamp: time.Now().UnixMilli(), Results: runDiagnosisChecks(ctx, pinger, checks)}, nil
}

type hostPort struct {
	Host string
	Port int
}

type diagnosisCheck struct {
	From        diagnosisNode
	Target      hostPort
	Description string
	ToChainType int
	ToInx       int
}

func loadDiagnosisNodes(ctx context.Context, nodeRepo *nodes.Repository, specs []tunnels.NodeSpec) ([]diagnosisNode, error) {
	result := make([]diagnosisNode, 0, len(specs))
	for _, spec := range specs {
		node, err := nodeRepo.Get(ctx, spec.NodeID)
		if err != nil {
			return nil, err
		}
		result = append(result, diagnosisNode{Spec: spec, Node: node})
	}
	return result, nil
}

func loadDiagnosisChainNodes(ctx context.Context, nodeRepo *nodes.Repository, chainSpecs [][]tunnels.NodeSpec) ([][]diagnosisNode, error) {
	result := make([][]diagnosisNode, 0, len(chainSpecs))
	for _, specs := range chainSpecs {
		nodes, err := loadDiagnosisNodes(ctx, nodeRepo, specs)
		if err != nil {
			return nil, err
		}
		result = append(result, nodes)
	}
	return result, nil
}

func buildTunnelDiagnosisChecks(entries []diagnosisNode, chains [][]diagnosisNode, exits []diagnosisNode, targets []hostPort) []diagnosisCheck {
	checks := make([]diagnosisCheck, 0)
	for _, entry := range entries {
		next := firstChainTargets(chains)
		if len(next) == 0 {
			next = exits
		}
		if len(next) == 0 {
			for _, target := range targets {
				checks = append(checks, diagnosisCheck{From: entry, Target: target, Description: "入口节点到目标地址", ToChainType: 0})
			}
			continue
		}
		for _, target := range nodeTargets(next) {
			checks = append(checks, diagnosisCheck{From: entry, Target: target.Target, Description: "入口节点到下一跳", ToChainType: target.ChainType, ToInx: target.Inx})
		}
	}
	for index, current := range chains {
		var next []diagnosisNode
		toChainType := 2
		toInx := index + 2
		if index+1 < len(chains) {
			next = chains[index+1]
		} else {
			next = exits
			toChainType = 3
			toInx = 0
		}
		for _, from := range current {
			if len(next) == 0 {
				for _, target := range targets {
					checks = append(checks, diagnosisCheck{From: from, Target: target, Description: fmt.Sprintf("第%d跳到目标地址", index+1)})
				}
				continue
			}
			for _, target := range nodeTargets(next) {
				checks = append(checks, diagnosisCheck{From: from, Target: target.Target, Description: fmt.Sprintf("第%d跳到下一跳", index+1), ToChainType: toChainType, ToInx: toInx})
			}
		}
	}
	for _, exit := range exits {
		for _, target := range targets {
			checks = append(checks, diagnosisCheck{From: exit, Target: target, Description: "出口节点到目标地址"})
		}
	}
	return checks
}

type nodeTarget struct {
	Target    hostPort
	ChainType int
	Inx       int
}

func firstChainTargets(chains [][]diagnosisNode) []diagnosisNode {
	if len(chains) == 0 {
		return nil
	}
	return chains[0]
}

func nodeTargets(items []diagnosisNode) []nodeTarget {
	result := make([]nodeTarget, 0, len(items))
	for _, item := range items {
		result = append(result, nodeTarget{Target: hostPort{Host: item.Node.ServerIP, Port: tcpDiagnosisPort(item.Spec)}, ChainType: item.Spec.ChainType, Inx: item.Spec.Inx})
	}
	return result
}

func tcpDiagnosisPort(spec tunnels.NodeSpec) int {
	protocol := strings.ToLower(strings.TrimSpace(spec.Protocol))
	if protocol == "tcp" || protocol == "" {
		return spec.Port
	}
	if spec.Port < 65535 {
		return spec.Port + 1
	}
	if spec.Port > 1 {
		return spec.Port - 1
	}
	return spec.Port
}

func runDiagnosisChecks(ctx context.Context, pinger tcpPinger, checks []diagnosisCheck) []diagnosisCheckResult {
	results := make([]diagnosisCheckResult, 0, len(checks))
	transport, _ := pinger.(transportPinger)
	for _, check := range checks {
		result := diagnosisCheckResult{
			Description:   check.Description,
			NodeName:      check.From.Node.Name,
			NodeID:        strconv.FormatInt(check.From.Node.ID, 10),
			TargetIP:      check.Target.Host,
			TargetPort:    check.Target.Port,
			FromChainType: check.From.Spec.ChainType,
			FromInx:       check.From.Spec.Inx,
			ToChainType:   check.ToChainType,
			ToInx:         check.ToInx,
		}
		protocol := strings.ToLower(strings.TrimSpace(check.From.Spec.Protocol))
		if protocol == "" {
			protocol = "tcp"
		}
		request := nodehub.TransportPingRequest{IP: check.Target.Host, Port: check.Target.Port, Count: diagnosisPingCount, Timeout: diagnosisPingTimeout}
		var response nodehub.TransportPingResponse
		var err error
		switch protocol {
		case "tcp":
			response, err = pinger.TCPPing(ctx, check.From.Node.ID, request)
		case "udp":
			if transport == nil {
				result.Message = "UDP diagnosis is unavailable on this node connection"
				results = append(results, result)
				continue
			}
			response, err = transport.UDPPing(ctx, check.From.Node.ID, request)
		case "quic":
			if transport == nil {
				result.Message = "QUIC diagnosis is unavailable on this node connection"
				results = append(results, result)
				continue
			}
			response, err = transport.QUICPing(ctx, check.From.Node.ID, request)
		case "kcp":
			if transport == nil {
				result.Message = "KCP diagnosis is unavailable on this node connection"
				results = append(results, result)
				continue
			}
			response, err = transport.KCPPing(ctx, check.From.Node.ID, request)
		default:
			result.Message = fmt.Sprintf("diagnosis is unsupported for %s transport", protocol)
			results = append(results, result)
			continue
		}
		if err != nil {
			result.Message = err.Error()
			results = append(results, result)
			continue
		}
		result.Success = response.Success
		result.AverageTime = response.AverageTime
		result.PacketLoss = response.PacketLoss
		if response.Error != "" {
			result.Message = response.Error
		}
		results = append(results, result)
	}
	return results
}

func parseRemoteTargets(value string) ([]hostPort, error) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	result := make([]hostPort, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		host, portValue, err := net.SplitHostPort(part)
		if err != nil {
			return nil, fmt.Errorf("invalid remote address: %s", part)
		}
		port, err := strconv.Atoi(portValue)
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid remote address: %s", part)
		}
		result = append(result, hostPort{Host: strings.Trim(host, "[]"), Port: port})
	}
	if len(result) == 0 {
		return nil, errors.New("remote address is required")
	}
	return result, nil
}

func tunnelTypeName(value int) string {
	if value == 1 {
		return "端口转发"
	}
	return "隧道转发"
}
