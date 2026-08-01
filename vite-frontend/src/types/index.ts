import { SVGProps } from "react";

export type IconSvgProps = SVGProps<SVGSVGElement> & {
  size?: number;
};

// 用户管理相关类型
export interface User {
  id: number;
  name?: string;
  user: string;
  pwd?: string;
  status: number; // 1-正常, 0-禁用
  flow: number; // 流量限制(GB)
  num: number; // 转发数量
  expTime?: number; // 过期时间戳
  flowResetTime?: number; // 流量重置日期(1-31号)
  createdTime?: number; // 创建时间戳
  inFlow?: number; // 下载流量(字节)
  outFlow?: number; // 上传流量(字节)
}

export interface UserForm {
  id?: number;
  name?: string;
  user: string;
  pwd?: string;
  status: number;
  flow: number;
  num: number;
  expTime: Date | null;
  flowResetTime: number;
  tunnelIds?: number[];
}

export interface UserTunnel {
  id: number;
  userId: number;
  tunnelId: number;
  tunnelName: string;
  status: number; // 1-正常, 0-禁用
  flow: number; // 流量限制(GB)
  num: number; // 转发数量
  expTime: number; // 过期时间戳
  flowResetTime: number; // 流量重置日期
  speedId?: number | null; // 限速规则ID
  speedLimitName?: string; // 限速规则名称
  inFlow?: number; // 下载流量(字节)
  outFlow?: number; // 上传流量(字节)
  tunnelFlow?: number; // 隧道流量计算类型(1-单向, 2-双向)
}

export interface UserTunnelForm {
  tunnelId: number | null;
  flow: number;
  num: number;
  expTime: Date | null;
  flowResetTime: number;
  speedId: number | null;
}

export interface Tunnel {
  id: number;
  name: string;
  entryNodeId: number;
  exitNodeId: number;
  entryNodeName?: string;
  exitNodeName?: string;
  status?: number;
  flow?: number; // 流量计算类型
}

export interface SpeedLimit {
  id: number;
  name: string;
  tunnelId: number;
  uploadSpeed: number;
  downloadSpeed: number;
}

export interface AuditEvent {
  id: number;
  actorId: number | null;
  action: string;
  resourceType: string;
  resourceId: string;
  outcome: "success" | "failure";
  requestId: string;
  remoteAddr: string;
  detail: string;
  createdAt: number;
}

export interface AuditPage {
  items: AuditEvent[];
  total: number;
}

export interface AuditListRequest {
  page: number;
  pageSize: number;
}

export interface RuntimeEndpoint {
  id?: number;
  name: string;
  address: string;
  priority: number;
  weight: number;
  backup: number;
  status: number;
  sortIndex: number;
}

export interface EndpointGroupRequest {
  name: string;
  description: string;
  strategy: "fifo" | "round" | "rand";
  maxFails: number;
  failTimeoutMs: number;
  probeIntervalMs: number;
  probeTimeoutMs: number;
  status: number;
  endpoints: RuntimeEndpoint[];
}

export interface EndpointGroup extends EndpointGroupRequest {
  id: number;
  createdTime?: number;
}

export interface RouteRule {
  id?: number;
  name: string;
  matchType:
    | "client_ip"
    | "protocol"
    | "host"
    | "host_regexp"
    | "method"
    | "path"
    | "path_regexp"
    | "path_prefix"
    | "header"
    | "header_regexp"
    | "query"
    | "query_regexp";
  value: string;
  secondaryValue: string;
  negate: number;
  priority: number;
  status: number;
  sortIndex: number;
  endpointIds: number[];
}

export interface RouteRuleSetRequest {
  name: string;
  description: string;
  status: number;
  rules: RouteRule[];
}

export interface RouteRuleSet extends RouteRuleSetRequest {
  id: number;
  createdTime?: number;
}

export interface NodeGroupMember {
  nodeId: number;
  priority: number;
  backup: number;
  sortIndex: number;
}

export interface NodeGroupRequest {
  name: string;
  description: string;
  strategy: "fifo" | "round" | "rand";
  maxFails: number;
  failTimeoutMs: number;
  status: number;
  members: NodeGroupMember[];
}

export interface NodeGroup extends NodeGroupRequest {
  id: number;
  createdTime?: number;
}

export interface TunnelNodeGroupBinding {
  id: number;
  tunnelId: number;
  groupId: number;
  chainType: number;
  port: number;
  strategy: "fifo" | "round" | "rand";
  hopIndex: number;
  protocol: "tcp" | "udp+quic" | "udp+kcp" | "mptcp";
  flowQuotaBytes: number;
  speedLimitMbps: number;
}

export interface ResourceID {
  id: number;
}

export interface Pagination {
  current: number;
  size: number;
  total: number;
}
