package nodeconfig

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestBuildTOTTransportMetadata(t *testing.T) {
	db := openTestDatabase(t)
	execFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,status,created_at,updated_at) VALUES(1,'admin','hash','admin',0,1,1,1)`)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,tcp_listen_addr,udp_listen_addr,created_at,updated_at) VALUES
		(1,'entry','10.0.0.1','203.0.113.1',10000,20000,'entry-secret',1,'0.0.0.0','[::]',1,1),
		(2,'relay','10.0.0.2','203.0.113.2',10000,20000,'relay-secret',1,'0.0.0.0','[::]',1,1),
		(3,'exit','10.0.0.3','203.0.113.3',10000,20000,'exit-secret',1,'0.0.0.0','[::]',1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,tot_enabled,tot_secret,tot_path_count,tot_paths,tot_max_payload,tot_window,tot_retransmit_interval_ms,tot_max_retries,tot_recovery_period_ms,tot_handshake_timeout_ms,tot_max_clock_skew_ms,tot_idle_ttl_ms,tot_mptcp,created_at,updated_at) VALUES(1,'tot',2,1,1,1,1,'tot-secret-0123456789',3,'["198.51.100.10:7200","[2001:db8::20]:7200"]',4096,64,50,8,500,2000,5000,60000,1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES
		(1,1,1,1,7000,'fifo',0,'tcp'),
		(2,1,2,2,7100,'fifo',1,'tcp'),
		(3,1,3,3,7200,'round',0,'tcp')`)
	execFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,strategy,status,sort_index,created_at,updated_at) VALUES(1,1,'forward',1,'192.0.2.1:443','fifo',1,0,1,1)`)
	execFixture(t, db, `INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10000)`)

	document, err := NewRepository(db).Build(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	chain := namedItem(document.Chains, "chains_1")
	if chain == nil {
		t.Fatalf("missing TOT chain: %#v", document.Chains)
	}
	hops := chain["hops"].([]map[string]any)
	dialer := hops[0]["nodes"].([]map[string]any)[0]["dialer"].(map[string]any)
	if dialer["type"] != "tot" {
		t.Fatalf("unexpected TOT dialer: %#v", dialer)
	}
	dialerMetadata := dialer["metadata"].(map[string]any)
	paths, ok := dialerMetadata["paths"].([]string)
	if dialerMetadata["secret"] != "tot-secret-0123456789" || dialerMetadata["pathCount"] != 2 || dialerMetadata["window"] != 64 || dialerMetadata["mptcp"] != true || !ok || len(paths) != 2 || paths[0] != "198.51.100.10:7200" {
		t.Fatalf("incomplete TOT dialer metadata: %#v", dialerMetadata)
	}

	document, err = NewRepository(db).Build(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	service := namedItem(document.Services, "1_relay_tcp")
	if service == nil {
		t.Fatalf("missing TOT relay service: %#v", document.Services)
	}
	listener := service["listener"].(map[string]any)
	if listener["type"] != "tot" {
		t.Fatalf("unexpected TOT listener: %#v", listener)
	}
	listenerMetadata := listener["metadata"].(map[string]any)
	if listenerMetadata["secret"] != "tot-secret-0123456789" || listenerMetadata["mptcp"] != true || listenerMetadata["idleTTL"] != 60*time.Second {
		t.Fatalf("incomplete TOT listener metadata: %#v", listenerMetadata)
	}
}

func TestBuildHybridTunnelWithUserPolicies(t *testing.T) {
	db := openTestDatabase(t)
	ctx := context.Background()
	execFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,flow_quota_bytes,forward_quota,status,created_at,updated_at) VALUES(1,'user','hash','user',0,0,0,1,1,1)`)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,tcp_listen_addr,udp_listen_addr,created_at,updated_at) VALUES
		(1,'entry','10.0.0.1','2001:db8::1',10000,20000,'entry-secret',1,'0.0.0.0','[::]',1,1),
		(2,'exit-a','10.0.0.2','2001:db8::2',10000,20000,'exit-a-secret',1,'0.0.0.0','[::]',1,1),
		(3,'exit-b','10.0.0.3','203.0.113.3',10000,20000,'exit-b-secret',1,'0.0.0.0','[::]',1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'hybrid',2,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol,speed_limit_mbps) VALUES
		(1,1,1,1,7000,'fifo',0,'udp+quic',0),
		(2,1,3,2,8000,'round',0,'udp+quic',0),
		(3,1,3,3,8100,'round',0,'udp+quic',0)`)
	execFixture(t, db, `INSERT INTO user_tunnels(id,user_id,tunnel_id,status,created_at,updated_at) VALUES(1,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO user_tunnel_entry_policies(id,user_tunnel_id,tunnel_id,entry_node_id,speed_limit_mbps,status,created_at,updated_at) VALUES(1,1,1,1,16,1,1,1)`)
	execFixture(t, db, `INSERT INTO user_tunnel_exit_policies(id,user_tunnel_id,tunnel_id,exit_node_id,status,created_at,updated_at) VALUES
		(1,1,1,2,1,1,1),(2,1,1,3,0,1,1)`)
	execFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,strategy,status,sort_index,created_at,updated_at) VALUES(1,1,'forward',1,'192.0.2.10:443','fifo',1,0,1,1)`)
	execFixture(t, db, `INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10000)`)

	document, err := NewRepository(db).Build(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, serviceName := range []string{"1_1_1_tcp", "1_1_1_udp"} {
		if namedItem(document.Services, serviceName) == nil {
			t.Fatalf("missing forward service %s", serviceName)
		}
	}
	if chainNameOfService(t, namedItem(document.Services, "1_1_1_tcp")) != "chains_1_user_1_tcp" {
		t.Fatal("tcp service does not use its policy-specific chain")
	}
	if chainNameOfService(t, namedItem(document.Services, "1_1_1_udp")) != "chains_1_user_1_udp" {
		t.Fatal("udp service does not use its policy-specific chain")
	}
	assertChainAddresses(t, namedItem(document.Chains, "chains_1_user_1_tcp"), []string{"[2001:db8::2]:8000"})
	assertChainAddresses(t, namedItem(document.Chains, "chains_1_user_1_udp"), []string{"[2001:db8::2]:8001"})
	limiter := namedItem(document.Limiters, "user_entry_1_1_1")
	if limiter == nil {
		t.Fatal("missing user entry limiter")
	}
	limits, ok := limiter["limits"].([]string)
	if !ok || len(limits) != 1 || limits[0] != "$ 2MB 2MB" {
		t.Fatalf("unexpected limiter: %#v", limiter)
	}

	exitDocument, err := NewRepository(db).Build(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if namedItem(exitDocument.Services, "1_relay_udp_quic_tcp") == nil || namedItem(exitDocument.Services, "1_relay_udp_quic_udp") == nil {
		t.Fatalf("hybrid exit services are incomplete: %#v", exitDocument.Services)
	}
}

func TestBuildMultihopHonorsExitPolicyAndRequiredRelay(t *testing.T) {
	db := openTestDatabase(t)
	execFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,status,created_at,updated_at) VALUES(1,'user','hash','user',0,1,1,1)`)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES
		(1,'entry','10.0.0.1','203.0.113.1',10000,20000,'entry',1,1,1),
		(2,'relay','10.0.0.2','203.0.113.2',10000,20000,'relay',1,1,1),
		(3,'exit-a','10.0.0.3','203.0.113.3',10000,20000,'exit-a',1,1,1),
		(4,'exit-b','10.0.0.4','203.0.113.4',10000,20000,'exit-b',1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'multihop',2,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES
		(1,1,1,1,7000,'fifo',0,'tcp'),
		(2,1,2,2,7100,'fifo',1,'tcp'),
		(3,1,3,3,7200,'round',0,'tcp'),
		(4,1,3,4,7300,'round',0,'tcp')`)
	execFixture(t, db, `INSERT INTO user_tunnels(id,user_id,tunnel_id,status,created_at,updated_at) VALUES(1,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO user_tunnel_exit_policies(id,user_tunnel_id,tunnel_id,exit_node_id,status,created_at,updated_at) VALUES
		(1,1,1,3,1,1,1),(2,1,1,4,0,1,1)`)
	execFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,strategy,status,sort_index,created_at,updated_at) VALUES(1,1,'forward',1,'192.0.2.10:443','fifo',1,0,1,1)`)
	execFixture(t, db, `INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10000)`)

	repository := NewRepository(db)
	document, err := repository.Build(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	chain := namedItem(document.Chains, "chains_1_user_1")
	if chain == nil {
		t.Fatal("missing user-specific multihop chain")
	}
	hops, ok := chain["hops"].([]map[string]any)
	if !ok || len(hops) != 2 {
		t.Fatalf("unexpected multihop chain: %#v", chain)
	}
	assertHopAddresses(t, hops[0], []string{"203.0.113.2:7100"})
	assertHopAddresses(t, hops[1], []string{"203.0.113.3:7200"})
	if chainNameOfService(t, namedItem(document.Services, "1_1_1_tcp")) != "chains_1_user_1" {
		t.Fatal("forward does not reference the user-specific multihop chain")
	}

	execFixture(t, db, `UPDATE nodes SET status=0 WHERE id=2`)
	document, err = repository.Build(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if namedItem(document.Services, "1_1_1_tcp") != nil || namedItem(document.Chains, "chains_1_user_1") != nil {
		t.Fatal("required offline relay was bypassed")
	}
}

func TestBuildFiltersUnavailableForwards(t *testing.T) {
	db := openTestDatabase(t)
	execFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,flow_quota_bytes,ingress_bytes,forward_quota,status,created_at,updated_at) VALUES
		(1,'admin','hash','admin',0,0,0,0,1,1,1),
		(2,'missing-permission','hash','user',0,0,0,0,1,1,1),
		(3,'quota-user','hash','user',0,100,100,0,1,1,1)`)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'entry','10.0.0.1','203.0.113.1',10000,20000,'secret',1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'direct',1,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES(1,1,1,1,7000,'fifo',0,'tcp')`)
	execFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,interface_name,strategy,status,sort_index,created_at,updated_at) VALUES
		(1,1,'admin',1,'192.0.2.1:80','eth0','fifo',1,0,1,1),
		(2,2,'missing',1,'192.0.2.2:80','','fifo',1,1,1,1),
		(3,3,'quota',1,'192.0.2.3:80','','fifo',1,2,1,1)`)
	execFixture(t, db, `INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10001),(2,1,10002),(3,1,10003)`)

	document, err := NewRepository(db).Build(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if namedItem(document.Services, "1_1_0_tcp") == nil || namedItem(document.Services, "1_1_0_udp") == nil {
		t.Fatal("administrator forward without user tunnel was filtered")
	}
	if namedItem(document.Services, "2_2_0_tcp") != nil {
		t.Fatal("regular user forward without tunnel permission was emitted")
	}
	if namedItem(document.Services, "3_3_0_tcp") != nil {
		t.Fatal("quota-exhausted user forward was emitted")
	}
	metadata, ok := namedItem(document.Services, "1_1_0_tcp")["metadata"].(map[string]any)
	if !ok || metadata["interface"] != "eth0" {
		t.Fatalf("direct interface metadata is missing: %#v", metadata)
	}
}

func TestBuildAdvancedForwardControls(t *testing.T) {
	db := openTestDatabase(t)
	execFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,status,created_at,updated_at) VALUES(1,'admin','hash','admin',0,1,1,1)`)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'entry','10.0.0.1','203.0.113.1',10000,20000,'secret',1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'direct',1,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES(1,1,1,1,7000,'fifo',0,'tcp')`)
	execFixture(t, db, `INSERT INTO endpoint_groups(id,name,strategy,max_fails,fail_timeout_ms,probe_interval_ms,probe_timeout_ms,status,created_at,updated_at) VALUES(1,'origins','round',2,45000,7000,1500,1,1,1)`)
	execFixture(t, db, `INSERT INTO endpoints(id,group_id,name,address,priority,backup,status,sort_index,created_at,updated_at) VALUES
		(1,1,'primary','192.0.2.10:443',20,0,1,0,1,1),
		(2,1,'backup','192.0.2.20:443',10,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO route_rule_sets(id,name,status,created_at,updated_at) VALUES(1,'hosts',1,1,1)`)
	execFixture(t, db, `INSERT INTO route_rules(id,rule_set_id,name,match_type,value,secondary_value,negate,priority,status,sort_index,created_at,updated_at) VALUES(1,1,'api','host','api.example.com','',0,200,1,0,1,1)`)
	execFixture(t, db, `INSERT INTO route_rule_endpoints(rule_id,endpoint_id) VALUES(1,1)`)
	execFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,interface_name,strategy,endpoint_group_id,route_rule_set_id,max_connections,max_connections_per_ip,source_ranges,source_whitelist,proxy_protocol_receive,proxy_protocol_send,status,sort_index,created_at,updated_at) VALUES(1,1,'advanced',1,'198.51.100.5:443','eth0','fifo',1,1,100,5,'192.0.2.0/24,2001:db8::1',1,2,1,1,0,1,1)`)
	execFixture(t, db, `INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10000)`)

	document, err := NewRepository(db).Build(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	service := namedItem(document.Services, "1_1_0_tcp")
	if service == nil {
		t.Fatal("advanced TCP service is missing")
	}
	if service["climiter"] != "forward_conn_1" || service["admission"] != "forward_source_1" {
		t.Fatalf("advanced resources are not bound: %#v", service)
	}
	metadata, _ := service["metadata"].(map[string]any)
	if metadata["interface"] != "eth0" || metadata["proxyProtocol"] != 2 {
		t.Fatalf("service metadata is incomplete: %#v", metadata)
	}
	handler, _ := service["handler"].(map[string]any)
	handlerMetadata, _ := handler["metadata"].(map[string]any)
	if handlerMetadata["proxyProtocol"] != 1 || handlerMetadata["sniffing"] != true {
		t.Fatalf("handler metadata is incomplete: %#v", handlerMetadata)
	}
	forwarder, _ := service["forwarder"].(map[string]any)
	selector, _ := forwarder["selector"].(map[string]any)
	if selector["strategy"] != "round" || selector["maxFails"] != 2 || selector["failTimeout"] != int64(45_000_000_000) {
		t.Fatalf("endpoint selector is incomplete: %#v", selector)
	}
	if forwarder["probePeriod"] != int64(7_000_000_000) || forwarder["probeTimeout"] != int64(1_500_000_000) {
		t.Fatalf("endpoint probes are incomplete: %#v", forwarder)
	}
	nodes, ok := forwarder["nodes"].([]map[string]any)
	if !ok || len(nodes) != 4 {
		t.Fatalf("unexpected endpoint nodes: %#v", forwarder["nodes"])
	}
	matcher, _ := nodes[0]["matcher"].(map[string]any)
	if matcher["rule"] != "Host(`api.example.com`)" || matcher["priority"] != 1_000_200 {
		t.Fatalf("route matcher is incomplete: %#v", matcher)
	}
	backupMetadata, _ := nodes[2]["metadata"].(map[string]any)
	if backupMetadata["backup"] != true {
		t.Fatalf("backup endpoint is not marked: %#v", nodes[2])
	}
	if nodes[3]["addr"] != "198.51.100.5:443" {
		t.Fatalf("legacy fallback endpoint is missing: %#v", nodes)
	}
	climiter := namedItem(document.CLimiters, "forward_conn_1")
	limits, _ := climiter["limits"].([]string)
	if len(limits) != 2 || limits[0] != "$ 100" || limits[1] != "$$ 5" {
		t.Fatalf("unexpected connection limits: %#v", climiter)
	}
	admission := namedItem(document.Admissions, "forward_source_1")
	matchers, _ := admission["matchers"].([]string)
	if admission["whitelist"] != true || len(matchers) != 2 || matchers[1] != "2001:db8::1" {
		t.Fatalf("unexpected admission: %#v", admission)
	}

	snapshot, err := DecodeSnapshot([]byte(`{"services":[],"chains":[],"limiters":[]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.CLimiters == nil || snapshot.Admissions == nil {
		t.Fatal("snapshot omitted authoritative advanced resource arrays")
	}
	if Equivalent(document, Document{Services: document.Services, Chains: document.Chains, Limiters: document.Limiters}) {
		t.Fatal("advanced resource drift was ignored")
	}
}

func TestEquivalentNormalizesInheritedHopInterface(t *testing.T) {
	expected := Document{Chains: []map[string]any{{
		"name": "chain",
		"hops": []map[string]any{{
			"name":      "hop",
			"interface": "eth0",
			"nodes":     []map[string]any{{"name": "node", "addr": "example.com:443"}},
		}},
	}}, Services: []map[string]any{}, Limiters: []map[string]any{}}
	actual := Document{Chains: []map[string]any{{
		"name": "chain",
		"hops": []map[string]any{{
			"name":      "hop",
			"interface": "eth0",
			"nodes":     []map[string]any{{"name": "node", "addr": "example.com:443", "interface": "eth0"}},
		}},
	}}, Services: []map[string]any{}, Limiters: []map[string]any{}}
	if !Equivalent(expected, actual) {
		t.Fatal("inherited node interface caused configuration drift")
	}
	actual.Chains[0]["hops"].([]map[string]any)[0]["nodes"].([]map[string]any)[0]["interface"] = "eth1"
	if Equivalent(expected, actual) {
		t.Fatal("material node interface drift was ignored")
	}
}

func TestEquivalentIgnoresRuntimeServiceStatus(t *testing.T) {
	expected := Document{Services: []map[string]any{{"name": "service", "addr": ":1"}}, Chains: []map[string]any{}, Limiters: []map[string]any{}}
	actual := Document{Services: []map[string]any{{"name": "service", "addr": ":1", "status": map[string]any{"state": "running"}}}, Chains: []map[string]any{}, Limiters: []map[string]any{}}
	if !Equivalent(expected, actual) {
		t.Fatal("runtime status caused configuration drift")
	}
	actual.Services[0]["addr"] = ":2"
	if Equivalent(expected, actual) {
		t.Fatal("material service drift was ignored")
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func execFixture(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatal(err)
	}
}

func namedItem(items []map[string]any, name string) map[string]any {
	for _, item := range items {
		if item["name"] == name {
			return item
		}
	}
	return nil
}

func chainNameOfService(t *testing.T, service map[string]any) string {
	t.Helper()
	if service == nil {
		t.Fatal("service is missing")
	}
	handler, ok := service["handler"].(map[string]any)
	if !ok {
		t.Fatalf("service handler is invalid: %#v", service)
	}
	chain, _ := handler["chain"].(string)
	return chain
}

func assertHopAddresses(t *testing.T, hop map[string]any, expected []string) {
	t.Helper()
	nodes, ok := hop["nodes"].([]map[string]any)
	if !ok || len(nodes) != len(expected) {
		t.Fatalf("chain nodes are invalid: %#v", hop)
	}
	for index, address := range expected {
		if nodes[index]["addr"] != address {
			t.Fatalf("unexpected chain address %v, want %v", nodes[index]["addr"], address)
		}
	}
}

func assertChainAddresses(t *testing.T, chain map[string]any, expected []string) {
	t.Helper()
	if chain == nil {
		t.Fatal("chain is missing")
	}
	hops, ok := chain["hops"].([]map[string]any)
	if !ok || len(hops) != 1 {
		t.Fatalf("chain hops are invalid: %#v", chain)
	}
	assertHopAddresses(t, hops[0], expected)
}
