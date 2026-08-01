import { useState, useEffect, useMemo } from "react";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Button } from "@heroui/button";
import { Input } from "@heroui/input";
import { Textarea } from "@heroui/input";
import { Select, SelectItem } from "@heroui/select";
import {
  Modal,
  ModalContent,
  ModalHeader,
  ModalBody,
  ModalFooter,
} from "@heroui/modal";
import { Chip } from "@heroui/chip";
import { Spinner } from "@heroui/spinner";
import { Alert } from "@heroui/alert";
import { Progress } from "@heroui/progress";
import { Accordion, AccordionItem } from "@heroui/accordion";
import toast from "react-hot-toast";

import {
  batchDeleteNodes,
  createNode,
  getNodeList,
  updateNode,
  deleteNode,
  getNodeInstallCommand,
} from "@/api";
import { useBatchDeleteSelection } from "@/hooks/useBatchDeleteSelection";

interface ControllerStatus {
  address: string;
  active: boolean;
  consecutiveFailures: number;
  lastSuccessAt: number;
  lastFailureAt: number;
  lastError: string;
}

interface TOTTelemetry {
  sessions: number;
  activePaths: number;
  pendingFrames: number;
  sentFrames: number;
  receivedFrames: number;
  retransmits: number;
  duplicateFrames: number;
  pathFailures: number;
}

interface Node {
  id: number;
  name: string;
  ip: string;
  serverIp: string;
  port: string;
  maxBandwidthMbps?: number | null;
  tcpListenAddr?: string;
  udpListenAddr?: string;
  interfaceName?: string;
  version?: string;
  http?: number; // 0 关 1 开
  tls?: number; // 0 关 1 开
  socks?: number; // 0 关 1 开
  status: number; // 1: 在线, 0: 离线
  controllers: ControllerStatus[];
  tot: TOTTelemetry;
  connectionStatus: "online" | "offline";
  systemInfo?: {
    cpuUsage: number;
    memoryUsage: number;
    uploadTraffic: number;
    downloadTraffic: number;
    uploadSpeed: number;
    downloadSpeed: number;
    uptime: number;
  } | null;
  copyLoading?: boolean;
}

interface NodeForm {
  id: number | null;
  name: string;
  serverIp: string;
  port: string;
  maxBandwidthMbps: string;
  tcpListenAddr: string;
  udpListenAddr: string;
  interfaceName: string;
  http: number; // 0 关 1 开
  tls: number; // 0 关 1 开
  socks: number; // 0 关 1 开
}

export default function NodePage() {
  const [nodeList, setNodeList] = useState<Node[]>([]);
  const [loading, setLoading] = useState(false);
  const [dialogVisible, setDialogVisible] = useState(false);
  const [dialogTitle, setDialogTitle] = useState("");
  const [isEdit, setIsEdit] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [nodeToDelete, setNodeToDelete] = useState<Node | null>(null);
  const [protocolDisabled, setProtocolDisabled] = useState(false);
  const [form, setForm] = useState<NodeForm>({
    id: null,
    name: "",
    serverIp: "",
    port: "1000-65535",
    maxBandwidthMbps: "",
    tcpListenAddr: "[::]",
    udpListenAddr: "[::]",
    interfaceName: "",
    http: 0,
    tls: 0,
    socks: 0,
  });
  const [errors, setErrors] = useState<Record<string, string>>({});
  const [keyword, setKeyword] = useState("");
  const [statusFilter, setStatusFilter] = useState<
    "all" | "online" | "offline"
  >("all");

  // 安装命令相关状态
  const [installCommandModal, setInstallCommandModal] = useState(false);
  const [installCommand, setInstallCommand] = useState("");
  const [currentNodeName, setCurrentNodeName] = useState("");

  useEffect(() => {
    loadNodes();
    const refreshTimer = setInterval(loadNodes, 10000);
    return () => clearInterval(refreshTimer);
  }, []);

  // 加载节点列表
  const loadNodes = async () => {
    setLoading(true);
    try {
      const res = await getNodeList();
      if (res.code === 0) {
        const latestNodeList = res.data.map((node: any) => ({
          ...node,
          connectionStatus: node.status === 1 ? "online" : "offline",
          controllers: Array.isArray(node.controllers) ? node.controllers : [],
          tot: {
            sessions: Number(node.tot?.sessions) || 0,
            activePaths: Number(node.tot?.activePaths) || 0,
            pendingFrames: Number(node.tot?.pendingFrames) || 0,
            sentFrames: Number(node.tot?.sentFrames) || 0,
            receivedFrames: Number(node.tot?.receivedFrames) || 0,
            retransmits: Number(node.tot?.retransmits) || 0,
            duplicateFrames: Number(node.tot?.duplicateFrames) || 0,
            pathFailures: Number(node.tot?.pathFailures) || 0,
          },
          systemInfo:
            node.status === 1
              ? {
                  cpuUsage: Number(node.cpu_usage) || 0,
                  memoryUsage: Number(node.memory_usage) || 0,
                  uploadTraffic: Number(node.bytes_transmitted) || 0,
                  downloadTraffic: Number(node.bytes_received) || 0,
                  uploadSpeed: 0,
                  downloadSpeed: 0,
                  uptime: Number(node.uptime) || 0,
                }
              : null,
          copyLoading: false,
        }));
        setNodeList(latestNodeList);
      } else {
        toast.error(res.msg || "加载节点列表失败");
      }
    } catch (error) {
      toast.error("网络错误，请重试");
    } finally {
      setLoading(false);
    }
  };

  const batchSelection = useBatchDeleteSelection<Node>({
    items: nodeList,
    entityLabel: "节点",
    batchDeleteApi: batchDeleteNodes,
    reloadData: loadNodes,
  });

  const filteredNodes = useMemo(() => {
    const lowerKeyword = keyword.trim().toLowerCase();
    return nodeList.filter((node) => {
      if (statusFilter === "online" && node.connectionStatus !== "online") {
        return false;
      }
      if (statusFilter === "offline" && node.connectionStatus !== "offline") {
        return false;
      }
      if (!lowerKeyword) {
        return true;
      }
      return [
        node.name,
        node.serverIp,
        node.version || "",
        String(node.id),
      ].some((field) => field?.toLowerCase().includes(lowerKeyword));
    });
  }, [nodeList, keyword, statusFilter]);

  // 格式化速度
  const formatSpeed = (bytesPerSecond: number): string => {
    if (bytesPerSecond === 0) return "0 B/s";

    const k = 1024;
    const sizes = ["B/s", "KB/s", "MB/s", "GB/s", "TB/s"];
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k));

    return (
      parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(2)) + " " + sizes[i]
    );
  };

  // 格式化开机时间
  const formatUptime = (seconds: number): string => {
    if (seconds === 0) return "-";

    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);

    if (days > 0) {
      return `${days}天${hours}小时`;
    } else if (hours > 0) {
      return `${hours}小时${minutes}分钟`;
    } else {
      return `${minutes}分钟`;
    }
  };

  // 格式化流量
  const formatTraffic = (bytes: number): string => {
    if (bytes === 0) return "0 B";

    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));

    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
  };

  // 获取进度条颜色
  const getProgressColor = (
    value: number,
    offline = false,
  ): "default" | "primary" | "secondary" | "success" | "warning" | "danger" => {
    if (offline) return "default";
    if (value <= 50) return "success";
    if (value <= 80) return "warning";
    return "danger";
  };

  // 验证IP地址格式
  const validateIp = (ip: string): boolean => {
    if (!ip || !ip.trim()) return false;

    const trimmedIp = ip.trim();

    // IPv4格式验证
    const ipv4Regex =
      /^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/;

    // IPv6格式验证
    const ipv6Regex =
      /^(([0-9a-fA-F]{1,4}:){7,7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(:[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(ffff(:0{1,4}){0,1}:){0,1}((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])|([0-9a-fA-F]{1,4}:){1,4}:((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3,3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9]))$/;

    if (
      ipv4Regex.test(trimmedIp) ||
      ipv6Regex.test(trimmedIp) ||
      trimmedIp === "localhost"
    ) {
      return true;
    }

    // 验证域名格式
    if (/^\d+$/.test(trimmedIp)) return false;

    const domainRegex =
      /^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?)+$/;
    const singleLabelDomain = /^[a-zA-Z][a-zA-Z0-9\-]{0,62}$/;

    return domainRegex.test(trimmedIp) || singleLabelDomain.test(trimmedIp);
  };

  // 验证端口格式：支持 80,443,100-600
  const validatePort = (
    portStr: string,
  ): { valid: boolean; error?: string } => {
    if (!portStr || !portStr.trim()) {
      return { valid: false, error: "请输入端口" };
    }

    const trimmed = portStr.trim();
    const parts = trimmed
      .split(",")
      .map((p) => p.trim())
      .filter((p) => p);

    if (parts.length === 0) {
      return { valid: false, error: "请输入有效的端口" };
    }

    for (const part of parts) {
      // 检查是否是端口范围 (如 100-600)
      if (part.includes("-")) {
        const range = part.split("-").map((p) => p.trim());
        if (range.length !== 2) {
          return { valid: false, error: `端口范围格式错误: ${part}` };
        }

        const start = parseInt(range[0]);
        const end = parseInt(range[1]);

        if (isNaN(start) || isNaN(end)) {
          return { valid: false, error: `端口必须是数字: ${part}` };
        }

        if (start < 1 || start > 65535 || end < 1 || end > 65535) {
          return {
            valid: false,
            error: `端口范围必须在 1-65535 之间: ${part}`,
          };
        }

        if (start >= end) {
          return { valid: false, error: `起始端口必须小于结束端口: ${part}` };
        }
      } else {
        // 单个端口
        const port = parseInt(part);
        if (isNaN(port)) {
          return { valid: false, error: `端口必须是数字: ${part}` };
        }

        if (port < 1 || port > 65535) {
          return { valid: false, error: `端口必须在 1-65535 之间: ${part}` };
        }
      }
    }

    return { valid: true };
  };

  // 表单验证
  const validateForm = (): boolean => {
    const newErrors: Record<string, string> = {};

    if (!form.name.trim()) {
      newErrors.name = "请输入节点名称";
    } else if (form.name.trim().length < 2) {
      newErrors.name = "节点名称长度至少2位";
    } else if (form.name.trim().length > 50) {
      newErrors.name = "节点名称长度不能超过50位";
    }

    if (!form.serverIp.trim()) {
      newErrors.serverIp = "请输入服务器IP地址";
    } else if (!validateIp(form.serverIp.trim())) {
      newErrors.serverIp = "请输入有效的IPv4、IPv6地址或域名";
    }

    const portValidation = validatePort(form.port);
    if (!portValidation.valid) {
      newErrors.port = portValidation.error || "端口格式错误";
    }

    const trimmedBandwidth = form.maxBandwidthMbps.trim();
    if (trimmedBandwidth) {
      if (!/^\d+$/.test(trimmedBandwidth)) {
        newErrors.maxBandwidthMbps = "最大带宽必须为正整数";
      } else {
        const parsedBandwidth = parseInt(trimmedBandwidth, 10);
        if (parsedBandwidth <= 0) {
          newErrors.maxBandwidthMbps = "最大带宽必须大于0";
        } else if (parsedBandwidth > 1000000) {
          newErrors.maxBandwidthMbps = "最大带宽不能超过1000000 Mbps";
        }
      }
    }

    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // 新增节点
  const handleAdd = () => {
    setDialogTitle("新增节点");
    setIsEdit(false);
    setDialogVisible(true);
    resetForm();
    setProtocolDisabled(true);
  };

  // 编辑节点
  const handleEdit = (node: Node) => {
    setDialogTitle("编辑节点");
    setIsEdit(true);
    setForm({
      id: node.id,
      name: node.name,
      serverIp: node.serverIp || "",
      port: node.port || "1000-65535",
      maxBandwidthMbps:
        node.maxBandwidthMbps != null ? String(node.maxBandwidthMbps) : "",
      tcpListenAddr: node.tcpListenAddr || "[::]",
      udpListenAddr: node.udpListenAddr || "[::]",
      interfaceName: node.interfaceName || "",
      http: typeof node.http === "number" ? node.http : 1,
      tls: typeof node.tls === "number" ? node.tls : 1,
      socks: typeof node.socks === "number" ? node.socks : 1,
    });
    const offline = node.connectionStatus !== "online";
    setProtocolDisabled(offline);
    setDialogVisible(true);
  };

  // 删除节点
  const handleDelete = (node: Node) => {
    setNodeToDelete(node);
    setDeleteModalOpen(true);
  };

  const confirmDelete = async () => {
    if (!nodeToDelete) return;

    setDeleteLoading(true);
    try {
      const res = await deleteNode(nodeToDelete.id);
      if (res.code === 0) {
        toast.success("删除成功");
        setNodeList((prev) => prev.filter((n) => n.id !== nodeToDelete.id));
        setDeleteModalOpen(false);
        setNodeToDelete(null);
      } else {
        toast.error(res.msg || "删除失败");
      }
    } catch (error) {
      toast.error("网络错误，请重试");
    } finally {
      setDeleteLoading(false);
    }
  };

  // 复制安装命令
  const handleCopyInstallCommand = async (node: Node) => {
    setNodeList((prev) =>
      prev.map((n) => (n.id === node.id ? { ...n, copyLoading: true } : n)),
    );

    try {
      const res = await getNodeInstallCommand(node.id);
      if (res.code === 0 && res.data) {
        try {
          await navigator.clipboard.writeText(res.data);
          toast.success("安装命令已复制到剪贴板");
        } catch (copyError) {
          // 复制失败，显示安装命令模态框
          setInstallCommand(res.data);
          setCurrentNodeName(node.name);
          setInstallCommandModal(true);
        }
      } else {
        toast.error(res.msg || "获取安装命令失败");
      }
    } catch (error) {
      toast.error("获取安装命令失败");
    } finally {
      setNodeList((prev) =>
        prev.map((n) => (n.id === node.id ? { ...n, copyLoading: false } : n)),
      );
    }
  };

  // 手动复制安装命令
  const handleManualCopy = async () => {
    try {
      await navigator.clipboard.writeText(installCommand);
      toast.success("安装命令已复制到剪贴板");
      setInstallCommandModal(false);
    } catch (error) {
      toast.error("复制失败，请手动选择文本复制");
    }
  };

  // 提交表单
  const handleSubmit = async () => {
    if (!validateForm()) return;

    setSubmitLoading(true);

    try {
      const apiCall = isEdit ? updateNode : createNode;
      const trimmedBandwidth = form.maxBandwidthMbps.trim();
      const data = {
        id: isEdit ? form.id : null,
        name: form.name,
        serverIp: form.serverIp,
        port: form.port,
        maxBandwidthMbps: trimmedBandwidth
          ? parseInt(trimmedBandwidth, 10)
          : null,
        tcpListenAddr: form.tcpListenAddr,
        udpListenAddr: form.udpListenAddr,
        interfaceName: form.interfaceName,
      };

      const res = await apiCall(data);
      if (res.code === 0) {
        toast.success(isEdit ? "更新成功" : "创建成功");
        setDialogVisible(false);

        if (isEdit) {
          setNodeList((prev) =>
            prev.map((n) =>
              n.id === form.id
                ? {
                    ...n,
                    name: form.name,
                    serverIp: form.serverIp,
                    port: form.port,
                    maxBandwidthMbps: data.maxBandwidthMbps,
                    tcpListenAddr: form.tcpListenAddr,
                    udpListenAddr: form.udpListenAddr,
                    interfaceName: form.interfaceName,
                    http: form.http,
                    tls: form.tls,
                    socks: form.socks,
                  }
                : n,
            ),
          );
        } else {
          loadNodes();
        }
      } else {
        toast.error(res.msg || (isEdit ? "更新失败" : "创建失败"));
      }
    } catch (error) {
      toast.error("网络错误，请重试");
    } finally {
      setSubmitLoading(false);
    }
  };

  // 重置表单
  const resetForm = () => {
    setForm({
      id: null,
      name: "",
      serverIp: "",
      port: "1000-65535",
      maxBandwidthMbps: "",
      tcpListenAddr: "[::]",
      udpListenAddr: "[::]",
      interfaceName: "",
      http: 0,
      tls: 0,
      socks: 0,
    });
    setErrors({});
  };

  return (
    <div className="px-3 lg:px-6 py-4 lg:py-6 space-y-4">
      <Card className="panel-shell">
        <CardBody className="p-4 lg:p-5">
          <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
            <div>
              <p className="text-xs uppercase tracking-[0.14em] panel-muted">
                Node Monitor
              </p>
              <h1 className="text-xl lg:text-2xl font-semibold mt-1">
                节点状态与资源监控
              </h1>
            </div>
            <div className="grid grid-cols-3 gap-2 w-full lg:w-auto lg:min-w-[360px]">
              <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-white/70 dark:bg-slate-900/60">
                <p className="text-xs panel-muted">节点总数</p>
                <p className="text-sm font-semibold mt-1">
                  {filteredNodes.length}
                </p>
              </div>
              <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-white/70 dark:bg-slate-900/60">
                <p className="text-xs panel-muted">在线</p>
                <p className="text-sm font-semibold mt-1 text-emerald-600 dark:text-emerald-300">
                  {
                    filteredNodes.filter(
                      (item) => item.connectionStatus === "online",
                    ).length
                  }
                </p>
              </div>
              <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-white/70 dark:bg-slate-900/60">
                <p className="text-xs panel-muted">离线</p>
                <p className="text-sm font-semibold mt-1 text-rose-600 dark:text-rose-300">
                  {
                    filteredNodes.filter(
                      (item) => item.connectionStatus === "offline",
                    ).length
                  }
                </p>
              </div>
            </div>
          </div>
        </CardBody>
      </Card>

      {/* 页面头部 */}
      <div className="panel-shell p-3 lg:p-4 flex flex-col gap-3">
        <div className="flex flex-col lg:flex-row lg:items-center gap-3">
          <Input
            size="sm"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            placeholder="搜索节点名称、IP、版本或 ID"
            className="w-full lg:max-w-sm"
          />
          <Select
            selectedKeys={new Set([statusFilter])}
            onSelectionChange={(keys) => {
              const value = Array.from(keys)[0] as
                | "all"
                | "online"
                | "offline"
                | undefined;
              setStatusFilter(value || "all");
            }}
            size="sm"
            className="w-full lg:w-[180px]"
            aria-label="节点状态"
          >
            <SelectItem key="all">全部状态</SelectItem>
            <SelectItem key="online">在线</SelectItem>
            <SelectItem key="offline">离线</SelectItem>
          </Select>
        </div>

        <div className="flex items-center justify-between gap-3 flex-wrap">
          <div className="text-sm panel-muted">
            当前显示 {filteredNodes.length} 个节点
          </div>
          <div className="flex items-center gap-3">
            {(keyword || statusFilter !== "all") && (
              <Button
                size="sm"
                variant="flat"
                color="default"
                onPress={() => {
                  setKeyword("");
                  setStatusFilter("all");
                }}
              >
                清除筛选
              </Button>
            )}
            {batchSelection.selectedCount > 0 && (
              <Button
                size="sm"
                variant="flat"
                color="danger"
                onPress={batchSelection.openBatchDeleteModal}
              >
                删除({batchSelection.selectedCount})
              </Button>
            )}
            <Button
              size="sm"
              variant="flat"
              color="default"
              onPress={batchSelection.toggleSelectAll}
              isDisabled={nodeList.length === 0}
            >
              {batchSelection.isAllSelected ? "取消全选" : "全选"}
            </Button>
            {batchSelection.selectedCount > 0 && (
              <Button
                size="sm"
                variant="flat"
                color="default"
                onPress={batchSelection.clearSelection}
              >
                清空选择
              </Button>
            )}

            <Button
              size="sm"
              variant="flat"
              color="primary"
              onPress={handleAdd}
            >
              新增
            </Button>
          </div>
        </div>
      </div>

      {/* 节点列表 */}
      {loading ? (
        <div className="flex items-center justify-center h-64">
          <div className="flex items-center gap-3">
            <Spinner size="sm" />
            <span className="text-default-600">正在加载...</span>
          </div>
        </div>
      ) : filteredNodes.length === 0 ? (
        <Card className="panel-shell">
          <CardBody className="text-center py-16">
            <div className="flex flex-col items-center gap-4">
              <div className="w-16 h-16 bg-default-100 rounded-full flex items-center justify-center">
                <svg
                  className="w-8 h-8 text-default-400"
                  fill="none"
                  stroke="currentColor"
                  viewBox="0 0 24 24"
                >
                  <path
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    strokeWidth={1.5}
                    d="M5 12h14M5 12l4-4m-4 4l4 4"
                  />
                </svg>
              </div>
              <div>
                <h3 className="text-lg font-semibold text-foreground">
                  暂无节点配置
                </h3>
                <p className="text-default-500 text-sm mt-1">
                  还没有创建任何节点配置，点击上方按钮开始创建
                </p>
              </div>
            </div>
          </CardBody>
        </Card>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 2xl:grid-cols-5 gap-4">
          {filteredNodes.map((node) => (
            <Card key={node.id} className="panel-shell panel-card-hover">
              <CardHeader className="pb-2">
                <div className="flex justify-between items-start w-full">
                  <div className="flex items-start gap-2 flex-1 min-w-0">
                    <input
                      type="checkbox"
                      className="mt-0.5 h-4 w-4 rounded border-default-300 text-danger focus:ring-danger"
                      checked={batchSelection.selectedIds.includes(node.id)}
                      onChange={() =>
                        batchSelection.toggleItemSelection(node.id)
                      }
                      aria-label={`选择节点 ${node.name}`}
                    />
                    <h3 className="font-semibold text-foreground truncate text-sm">
                      {node.name}
                    </h3>
                  </div>
                  <div className="flex items-center gap-1.5 ml-2">
                    <Chip
                      color={
                        node.connectionStatus === "online"
                          ? "success"
                          : "danger"
                      }
                      variant="flat"
                      size="sm"
                      className="text-xs"
                    >
                      {node.connectionStatus === "online" ? "在线" : "离线"}
                    </Chip>
                  </div>
                </div>
              </CardHeader>

              <CardBody className="pt-0 pb-3">
                {/* 基础信息 */}
                <div className="space-y-2 mb-4">
                  <div className="flex justify-between items-center text-sm min-w-0">
                    <span className="text-default-600 flex-shrink-0">IP</span>
                    <div className="text-right text-xs min-w-0 flex-1 ml-2">
                      <span
                        className="font-mono truncate block"
                        title={node.serverIp.trim()}
                      >
                        {node.serverIp.trim()}
                      </span>
                    </div>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-default-600">版本</span>
                    <span className="text-xs">{node.version || "未知"}</span>
                  </div>
                  <div className="flex justify-between text-sm">
                    <span className="text-default-600">控制器</span>
                    {node.controllers.length > 0 ? (
                      <Chip
                        color={
                          node.controllers.some(
                            (controller) =>
                              controller.active &&
                              controller.consecutiveFailures === 0,
                          )
                            ? "success"
                            : "danger"
                        }
                        variant="flat"
                        size="sm"
                        title={node.controllers
                          .map(
                            (controller) =>
                              `${controller.address}${controller.lastError ? `: ${controller.lastError}` : ""}`,
                          )
                          .join("\n")}
                      >
                        {node.controllers.find(
                          (controller) => controller.active,
                        )?.address || "不可用"}
                      </Chip>
                    ) : (
                      <span className="text-xs text-default-400">等待遥测</span>
                    )}
                  </div>
                  {node.controllers.some(
                    (controller) => controller.consecutiveFailures > 0,
                  ) && (
                    <div className="rounded-md border border-warning-200 bg-warning-50 px-2 py-1.5 text-xs text-warning-700 dark:border-warning-300/20 dark:bg-warning-100/10 dark:text-warning-400">
                      {node.controllers
                        .filter(
                          (controller) => controller.consecutiveFailures > 0,
                        )
                        .map((controller) => (
                          <div
                            key={controller.address}
                            className="truncate"
                            title={controller.lastError}
                          >
                            {controller.address}：连续失败{" "}
                            {controller.consecutiveFailures} 次
                          </div>
                        ))}
                    </div>
                  )}
                  <div className="flex justify-between text-sm">
                    <span className="text-default-600">开机时间</span>
                    <span className="text-xs">
                      {node.connectionStatus === "online" && node.systemInfo
                        ? formatUptime(node.systemInfo.uptime)
                        : "-"}
                    </span>
                  </div>
                </div>

                {/* 系统监控 */}
                <div className="space-y-3 mb-4">
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <div className="flex justify-between text-xs mb-1">
                        <span>CPU</span>
                        <span className="font-mono">
                          {node.connectionStatus === "online" && node.systemInfo
                            ? `${node.systemInfo.cpuUsage.toFixed(1)}%`
                            : "-"}
                        </span>
                      </div>
                      <Progress
                        value={
                          node.connectionStatus === "online" && node.systemInfo
                            ? node.systemInfo.cpuUsage
                            : 0
                        }
                        color={getProgressColor(
                          node.connectionStatus === "online" && node.systemInfo
                            ? node.systemInfo.cpuUsage
                            : 0,
                          node.connectionStatus !== "online",
                        )}
                        size="sm"
                        aria-label="CPU使用率"
                      />
                    </div>
                    <div>
                      <div className="flex justify-between text-xs mb-1">
                        <span>内存</span>
                        <span className="font-mono">
                          {node.connectionStatus === "online" && node.systemInfo
                            ? `${node.systemInfo.memoryUsage.toFixed(1)}%`
                            : "-"}
                        </span>
                      </div>
                      <Progress
                        value={
                          node.connectionStatus === "online" && node.systemInfo
                            ? node.systemInfo.memoryUsage
                            : 0
                        }
                        color={getProgressColor(
                          node.connectionStatus === "online" && node.systemInfo
                            ? node.systemInfo.memoryUsage
                            : 0,
                          node.connectionStatus !== "online",
                        )}
                        size="sm"
                        aria-label="内存使用率"
                      />
                    </div>
                  </div>

                  <div className="grid grid-cols-2 gap-2 text-xs">
                    <div className="text-center p-2 bg-default-50 dark:bg-default-100 rounded">
                      <div className="text-default-600 mb-0.5">上传</div>
                      <div className="font-mono">
                        {node.connectionStatus === "online" && node.systemInfo
                          ? formatSpeed(node.systemInfo.uploadSpeed)
                          : "-"}
                      </div>
                    </div>
                    <div className="text-center p-2 bg-default-50 dark:bg-default-100 rounded">
                      <div className="text-default-600 mb-0.5">下载</div>
                      <div className="font-mono">
                        {node.connectionStatus === "online" && node.systemInfo
                          ? formatSpeed(node.systemInfo.downloadSpeed)
                          : "-"}
                      </div>
                    </div>
                  </div>

                  {/* TOT 运行监控 */}
                  {node.tot.sessions > 0 && (
                    <div className="grid grid-cols-2 gap-2 text-xs mb-3">
                      <div className="rounded bg-default-50 dark:bg-default-100 p-2">
                        <div className="text-default-600">TOT 会话 / 路径</div>
                        <div className="font-mono">
                          {node.tot.sessions} / {node.tot.activePaths}
                        </div>
                      </div>
                      <div className="rounded bg-default-50 dark:bg-default-100 p-2">
                        <div className="text-default-600">待确认帧</div>
                        <div className="font-mono">
                          {node.tot.pendingFrames}
                        </div>
                      </div>
                      <div className="rounded bg-warning-50 dark:bg-warning-100/20 p-2">
                        <div className="text-warning-600">重传 / 路径故障</div>
                        <div className="font-mono">
                          {node.tot.retransmits} / {node.tot.pathFailures}
                        </div>
                      </div>
                      <div className="rounded bg-default-50 dark:bg-default-100 p-2">
                        <div className="text-default-600">收发帧</div>
                        <div className="font-mono">
                          {node.tot.receivedFrames} / {node.tot.sentFrames}
                        </div>
                      </div>
                    </div>
                  )}

                  <div className="grid grid-cols-2 gap-2 text-xs">
                    <div className="text-center p-2 bg-primary-50 dark:bg-primary-100/20 rounded border border-primary-200 dark:border-primary-300/20">
                      <div className="text-primary-600 dark:text-primary-400 mb-0.5">
                        ↑ 上行流量
                      </div>
                      <div className="font-mono text-primary-700 dark:text-primary-300">
                        {node.connectionStatus === "online" && node.systemInfo
                          ? formatTraffic(node.systemInfo.uploadTraffic)
                          : "-"}
                      </div>
                    </div>
                    <div className="text-center p-2 bg-success-50 dark:bg-success-100/20 rounded border border-success-200 dark:border-success-300/20">
                      <div className="text-success-600 dark:text-success-400 mb-0.5">
                        ↓ 下行流量
                      </div>
                      <div className="font-mono text-success-700 dark:text-success-300">
                        {node.connectionStatus === "online" && node.systemInfo
                          ? formatTraffic(node.systemInfo.downloadTraffic)
                          : "-"}
                      </div>
                    </div>
                  </div>
                </div>

                {/* 操作按钮 */}
                <div className="space-y-1.5">
                  <div className="flex gap-1.5">
                    <Button
                      size="sm"
                      variant="flat"
                      color="success"
                      onPress={() => handleCopyInstallCommand(node)}
                      isLoading={node.copyLoading}
                      className="flex-1 min-h-8"
                    >
                      安装
                    </Button>
                    <Button
                      size="sm"
                      variant="flat"
                      color="primary"
                      onPress={() => handleEdit(node)}
                      className="flex-1 min-h-8"
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      variant="flat"
                      color="danger"
                      onPress={() => handleDelete(node)}
                      className="flex-1 min-h-8"
                    >
                      删除
                    </Button>
                  </div>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {/* 新增/编辑节点对话框 */}
      <Modal
        isOpen={dialogVisible}
        onClose={() => setDialogVisible(false)}
        size="2xl"
        scrollBehavior="outside"
        backdrop="blur"
        placement="center"
      >
        <ModalContent>
          <ModalHeader>{dialogTitle}</ModalHeader>
          <ModalBody>
            <div className="space-y-4">
              <Input
                label="节点名称"
                placeholder="请输入节点名称"
                value={form.name}
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, name: e.target.value }))
                }
                isInvalid={!!errors.name}
                errorMessage={errors.name}
                variant="bordered"
              />

              <Input
                label="服务器IP"
                placeholder="请输入服务器IP地址，如: 192.168.1.100 或 example.com"
                value={form.serverIp}
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, serverIp: e.target.value }))
                }
                isInvalid={!!errors.serverIp}
                errorMessage={errors.serverIp}
                variant="bordered"
              />

              <Input
                label="可用端口"
                placeholder="例如: 80,443,1000-65535"
                value={form.port}
                onChange={(e) =>
                  setForm((prev) => ({ ...prev, port: e.target.value }))
                }
                isInvalid={!!errors.port}
                errorMessage={errors.port}
                variant="bordered"
                description="支持单个端口(80)、多个端口(80,443)或端口范围(1000-65535)，多个可用逗号分隔"
                classNames={{
                  input: "font-mono",
                }}
              />

              <Input
                label="节点最大带宽 (Mbps)"
                placeholder="留空表示不限制，例如: 1000"
                type="number"
                min={1}
                value={form.maxBandwidthMbps}
                onChange={(e) =>
                  setForm((prev) => ({
                    ...prev,
                    maxBandwidthMbps: e.target.value,
                  }))
                }
                isInvalid={!!errors.maxBandwidthMbps}
                errorMessage={errors.maxBandwidthMbps}
                variant="bordered"
                description="用于主备出口带宽判定，填写该节点允许使用的总带宽上限"
              />

              {/* 高级配置 */}
              <Accordion variant="bordered">
                <AccordionItem
                  key="advanced"
                  aria-label="高级配置"
                  title="高级配置"
                >
                  <div className="space-y-4 pb-2">
                    <Input
                      label="出口网卡名或IP"
                      placeholder="请输入出口网卡名或IP"
                      value={form.interfaceName}
                      onChange={(e) =>
                        setForm((prev) => ({
                          ...prev,
                          interfaceName: e.target.value,
                        }))
                      }
                      isInvalid={!!errors.interfaceName}
                      errorMessage={errors.interfaceName}
                      variant="bordered"
                      description="用于多IP服务器指定使用那个IP请求远程地址，不懂的默认为空就行"
                    />

                    <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                      <Input
                        label="TCP监听地址"
                        placeholder="请输入TCP监听地址"
                        value={form.tcpListenAddr}
                        onChange={(e) =>
                          setForm((prev) => ({
                            ...prev,
                            tcpListenAddr: e.target.value,
                          }))
                        }
                        isInvalid={!!errors.tcpListenAddr}
                        errorMessage={errors.tcpListenAddr}
                        variant="bordered"
                        startContent={
                          <div className="pointer-events-none flex items-center">
                            <span className="text-default-400 text-small">
                              TCP
                            </span>
                          </div>
                        }
                      />

                      <Input
                        label="UDP监听地址"
                        placeholder="请输入UDP监听地址"
                        value={form.udpListenAddr}
                        onChange={(e) =>
                          setForm((prev) => ({
                            ...prev,
                            udpListenAddr: e.target.value,
                          }))
                        }
                        isInvalid={!!errors.udpListenAddr}
                        errorMessage={errors.udpListenAddr}
                        variant="bordered"
                        startContent={
                          <div className="pointer-events-none flex items-center">
                            <span className="text-default-400 text-small">
                              UDP
                            </span>
                          </div>
                        }
                      />
                    </div>
                    {/* Node-reported protocol capabilities */}
                    <div>
                      <div className="text-sm font-medium text-default-700 mb-2">
                        节点协议能力
                      </div>
                      <div className="text-xs text-default-500 mb-2">
                        以下状态由在线节点上报，不能在面板中直接修改；如需变更，请更新节点本地配置并重新连接。
                      </div>
                      <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 bg-default-50 dark:bg-default-100 p-3 rounded-md border border-default-200 dark:border-default-100/30">
                        {[
                          ["HTTP", form.http],
                          ["TLS", form.tls],
                          ["SOCKS", form.socks],
                        ].map(([protocol, enabled]) => (
                          <div
                            key={protocol}
                            className="px-3 py-3 rounded-lg bg-white dark:bg-default-50 border border-default-200 dark:border-default-100/30"
                          >
                            <div className="text-sm font-medium text-default-700">
                              {protocol}
                            </div>
                            <div className="mt-1 text-xs text-default-500">
                              {protocolDisabled
                                ? "等待节点上报"
                                : enabled === 1
                                  ? "节点已开启"
                                  : "节点已关闭"}
                            </div>
                          </div>
                        ))}
                      </div>
                    </div>

                    <Alert
                      color="primary"
                      variant="flat"
                      description="协议能力来自节点本地 GOST 配置。面板只负责展示在线状态，不会将心跳快照误写回节点配置。"
                    />
                  </div>
                </AccordionItem>
              </Accordion>

              <Alert
                color="primary"
                variant="flat"
                description="服务器ip是你要添加的服务器的ip地址，不是面板的ip地址。"
                className="mt-4"
              />
            </div>
          </ModalBody>
          <ModalFooter>
            <Button variant="flat" onPress={() => setDialogVisible(false)}>
              取消
            </Button>
            <Button
              color="primary"
              onPress={handleSubmit}
              isLoading={submitLoading}
            >
              {submitLoading ? "提交中..." : "确定"}
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {/* 删除确认模态框 */}
      <Modal
        isOpen={deleteModalOpen}
        onOpenChange={setDeleteModalOpen}
        size="2xl"
        scrollBehavior="outside"
        backdrop="blur"
        placement="center"
      >
        <ModalContent>
          {(onClose) => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold">确认删除</h2>
              </ModalHeader>
              <ModalBody>
                <p>
                  确定要删除节点 <strong>"{nodeToDelete?.name}"</strong> 吗？
                </p>
                <p className="text-small text-default-500">
                  此操作不可恢复，请谨慎操作。
                </p>
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={onClose}>
                  取消
                </Button>
                <Button
                  color="danger"
                  onPress={confirmDelete}
                  isLoading={deleteLoading}
                >
                  {deleteLoading ? "删除中..." : "确认删除"}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      <Modal
        isOpen={batchSelection.modalOpen}
        onOpenChange={batchSelection.setModalOpen}
        size="2xl"
        scrollBehavior="outside"
        backdrop="blur"
        placement="center"
      >
        <ModalContent>
          {() => (
            <>
              <ModalHeader className="flex flex-col gap-1">
                <h2 className="text-xl font-bold text-danger">确认批量删除</h2>
              </ModalHeader>
              <ModalBody>
                <p>
                  确定要删除已选择的{" "}
                  <strong>{batchSelection.selectedCount}</strong> 个节点吗？
                </p>
                <p className="text-small text-default-500">
                  此操作不可恢复，请谨慎操作。
                </p>
                {batchSelection.lastResult &&
                  batchSelection.failures.length > 0 && (
                    <div className="mt-3 space-y-2">
                      <p className="text-small text-warning">
                        已成功删除 {batchSelection.lastResult.successCount}{" "}
                        个，失败 {batchSelection.lastResult.failureCount} 个
                      </p>
                      <div className="max-h-48 overflow-y-auto rounded border border-warning-200 bg-warning-50 p-2 text-xs">
                        {batchSelection.failures.map((item) => (
                          <p key={item.id} className="text-warning-700">
                            {item.name}: {item.reason}
                          </p>
                        ))}
                      </div>
                    </div>
                  )}
              </ModalBody>
              <ModalFooter>
                <Button variant="light" onPress={batchSelection.closeModal}>
                  {batchSelection.failures.length > 0 ? "关闭" : "取消"}
                </Button>
                {batchSelection.failures.length > 0 && (
                  <Button
                    color="warning"
                    onPress={batchSelection.retryFailedDeletes}
                    isLoading={batchSelection.deleting}
                  >
                    重试失败项
                  </Button>
                )}
                <Button
                  color="danger"
                  onPress={batchSelection.confirmBatchDelete}
                  isLoading={batchSelection.deleting}
                  isDisabled={batchSelection.failures.length > 0}
                >
                  {batchSelection.failures.length > 0 ? "已完成" : "确认删除"}
                </Button>
              </ModalFooter>
            </>
          )}
        </ModalContent>
      </Modal>

      {/* 安装命令模态框 */}
      <Modal
        isOpen={installCommandModal}
        onClose={() => setInstallCommandModal(false)}
        size="2xl"
        scrollBehavior="outside"
        backdrop="blur"
        placement="center"
      >
        <ModalContent>
          <ModalHeader>安装命令 - {currentNodeName}</ModalHeader>
          <ModalBody>
            <div className="space-y-4">
              <p className="text-sm text-default-600">
                请复制以下安装命令到服务器上执行：
              </p>
              <div className="relative">
                <Textarea
                  value={installCommand}
                  readOnly
                  variant="bordered"
                  minRows={6}
                  maxRows={10}
                  className="font-mono text-sm"
                  classNames={{
                    input: "font-mono text-sm",
                  }}
                />
                <Button
                  size="sm"
                  color="primary"
                  variant="flat"
                  className="absolute top-2 right-2"
                  onPress={handleManualCopy}
                >
                  复制
                </Button>
              </div>
              <div className="text-xs text-default-500">
                💡 提示：如果复制按钮失效，请手动选择上方文本进行复制
              </div>
            </div>
          </ModalBody>
          <ModalFooter>
            <Button
              variant="flat"
              onPress={() => setInstallCommandModal(false)}
            >
              关闭
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>
    </div>
  );
}
