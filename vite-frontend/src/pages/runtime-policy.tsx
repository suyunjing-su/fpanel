import { useCallback, useEffect, useMemo, useState } from "react";
import { Alert } from "@heroui/alert";
import { Button } from "@heroui/button";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Input } from "@heroui/input";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
} from "@heroui/modal";
import { Select, SelectItem } from "@heroui/select";
import { Spinner } from "@heroui/spinner";
import { Switch } from "@heroui/switch";
import toast from "react-hot-toast";

import {
  createEndpointGroup,
  createNodeGroup,
  createRouteRuleSet,
  createTunnelNodeGroupBinding,
  deleteEndpointGroup,
  deleteNodeGroup,
  deleteRouteRuleSet,
  deleteTunnelNodeGroupBinding,
  getEndpointGroups,
  getNodeGroups,
  getNodeList,
  getRouteRuleSets,
  getTunnelList,
  getTunnelNodeGroupBindings,
  updateEndpointGroup,
  updateNodeGroup,
  updateRouteRuleSet,
  updateTunnelNodeGroupBinding,
} from "@/api";
import type {
  EndpointGroup,
  EndpointGroupRequest,
  NodeGroup,
  NodeGroupMember,
  NodeGroupRequest,
  RouteRule,
  RouteRuleSet,
  RouteRuleSetRequest,
  RuntimeEndpoint,
  TunnelNodeGroupBinding,
} from "@/types";
import { isAdmin } from "@/utils/auth";

interface Option {
  id: number;
  name: string;
  type?: number;
}

type Strategy = "fifo" | "round" | "rand";

const emptyEndpoint = (): RuntimeEndpoint => ({
  name: "",
  address: "",
  priority: 0,
  weight: 1,
  backup: 0,
  status: 1,
  sortIndex: 0,
});

const emptyRule = (): RouteRule => ({
  name: "",
  matchType: "client_ip",
  value: "",
  secondaryValue: "",
  negate: 0,
  priority: 100,
  status: 1,
  sortIndex: 0,
  endpointIds: [],
});

const emptyMember = (sortIndex: number): NodeGroupMember => ({
  nodeId: 0,
  priority: 0,
  backup: 0,
  sortIndex,
});

const endpointGroupDefaults = (): EndpointGroupRequest => ({
  name: "",
  description: "",
  strategy: "fifo",
  maxFails: 1,
  failTimeoutMs: 600000,
  probeIntervalMs: 10000,
  probeTimeoutMs: 3000,
  status: 1,
  endpoints: [emptyEndpoint()],
});

const ruleSetDefaults = (): RouteRuleSetRequest => ({
  name: "",
  description: "",
  status: 1,
  rules: [emptyRule()],
});

const nodeGroupDefaults = (): NodeGroupRequest => ({
  name: "",
  description: "",
  strategy: "fifo",
  maxFails: 1,
  failTimeoutMs: 600000,
  status: 1,
  members: [emptyMember(0)],
});

const bindingDefaults = (): Omit<TunnelNodeGroupBinding, "id"> => ({
  tunnelId: 0,
  groupId: 0,
  chainType: 1,
  port: 0,
  strategy: "fifo",
  hopIndex: 0,
  protocol: "tcp",
  flowQuotaBytes: 0,
  speedLimitMbps: 0,
});

const toInt = (value: string, fallback = 0) => {
  const parsed = Number.parseInt(value, 10);
  return Number.isFinite(parsed) ? parsed : fallback;
};

const strategyOptions = [
  { key: "fifo", label: "FIFO（主备顺序）" },
  { key: "round", label: "轮询" },
  { key: "rand", label: "随机（按权重）" },
] as const;

const ruleOptions = [
  ["client_ip", "客户端 IP"],
  ["protocol", "协议"],
  ["host", "Host"],
  ["host_regexp", "Host 正则"],
  ["method", "方法"],
  ["path", "路径"],
  ["path_regexp", "路径正则"],
  ["path_prefix", "路径前缀"],
  ["header", "请求头"],
  ["header_regexp", "请求头正则"],
  ["query", "查询参数"],
  ["query_regexp", "查询参数正则"],
] as const;

const strategySelect = (
  value: Strategy,
  onChange: (value: Strategy) => void,
) => (
  <Select
    label="选择策略"
    selectedKeys={new Set([value])}
    onSelectionChange={(keys) => {
      const selected = Array.from(keys)[0];
      if (selected) onChange(String(selected) as Strategy);
    }}
  >
    {strategyOptions.map((option) => (
      <SelectItem key={option.key}>{option.label}</SelectItem>
    ))}
  </Select>
);

export default function RuntimePolicyPage() {
  const [tab, setTab] = useState<"endpoints" | "rules" | "nodes" | "bindings">(
    "endpoints",
  );
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [endpointGroups, setEndpointGroups] = useState<EndpointGroup[]>([]);
  const [ruleSets, setRuleSets] = useState<RouteRuleSet[]>([]);
  const [nodeGroups, setNodeGroups] = useState<NodeGroup[]>([]);
  const [bindings, setBindings] = useState<TunnelNodeGroupBinding[]>([]);
  const [nodes, setNodes] = useState<Option[]>([]);
  const [tunnels, setTunnels] = useState<Option[]>([]);
  const [endpointEditor, setEndpointEditor] = useState<
    EndpointGroupRequest | EndpointGroup | null
  >(null);
  const [ruleEditor, setRuleEditor] = useState<
    RouteRuleSetRequest | RouteRuleSet | null
  >(null);
  const [nodeEditor, setNodeEditor] = useState<
    NodeGroupRequest | NodeGroup | null
  >(null);
  const [bindingEditor, setBindingEditor] = useState<
    (Omit<TunnelNodeGroupBinding, "id"> & { id?: number }) | null
  >(null);

  const loadData = useCallback(async () => {
    setLoading(true);
    try {
      const [
        endpointResponse,
        ruleResponse,
        nodeGroupResponse,
        bindingResponse,
        nodeResponse,
        tunnelResponse,
      ] = await Promise.all([
        getEndpointGroups(),
        getRouteRuleSets(),
        getNodeGroups(),
        getTunnelNodeGroupBindings(),
        getNodeList(),
        getTunnelList(),
      ]);
      const failed = [
        endpointResponse,
        ruleResponse,
        nodeGroupResponse,
        bindingResponse,
        nodeResponse,
        tunnelResponse,
      ].find((response) => response.code !== 0);
      if (failed) throw new Error(failed.msg || "运行策略加载失败");
      setEndpointGroups(endpointResponse.data || []);
      setRuleSets(ruleResponse.data || []);
      setNodeGroups(nodeGroupResponse.data || []);
      setBindings(bindingResponse.data || []);
      setNodes(
        (nodeResponse.data || []).map((node: { id: number; name: string }) => ({
          id: node.id,
          name: node.name,
        })),
      );
      setTunnels(
        (tunnelResponse.data || []).map(
          (tunnel: { id: number; name: string; type?: number }) => ({
            id: tunnel.id,
            name: tunnel.name,
            type: tunnel.type,
          }),
        ),
      );
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "运行策略加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (isAdmin()) void loadData();
  }, [loadData]);

  const save = async (action: () => Promise<{ code: number; msg: string }>) => {
    setSaving(true);
    try {
      const response = await action();
      if (response.code !== 0) throw new Error(response.msg || "保存失败");
      toast.success("保存成功");
      await loadData();
      setEndpointEditor(null);
      setRuleEditor(null);
      setNodeEditor(null);
      setBindingEditor(null);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const remove = async (
    label: string,
    id: number,
    action: (id: number) => Promise<{ code: number; msg: string }>,
  ) => {
    if (
      !window.confirm(`确定删除${label}吗？已被转发或隧道引用的资源不能删除。`)
    )
      return;
    setSaving(true);
    try {
      const response = await action(id);
      if (response.code !== 0) throw new Error(response.msg || "删除失败");
      toast.success("删除成功");
      await loadData();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "删除失败");
    } finally {
      setSaving(false);
    }
  };

  const endpointById = useMemo(
    () =>
      new Map(
        endpointGroups.flatMap((group) =>
          group.endpoints.map((endpoint) => [endpoint.id || 0, endpoint]),
        ),
      ),
    [endpointGroups],
  );

  if (!isAdmin()) {
    return (
      <Alert color="danger" className="m-6">
        只有管理员可以管理运行策略。
      </Alert>
    );
  }

  if (loading) {
    return (
      <div className="h-full flex items-center justify-center">
        <Spinner label="正在加载运行策略" />
      </div>
    );
  }

  return (
    <div className="p-4 lg:p-6 space-y-4 h-full overflow-y-auto">
      <Card className="panel-shell">
        <CardBody className="p-5">
          <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
            <div>
              <p className="text-xs uppercase tracking-[0.14em] panel-muted">
                Runtime Policy
              </p>
              <h1 className="text-2xl font-semibold mt-1">运行策略</h1>
              <p className="text-sm panel-muted mt-1">
                管理端点健康、权重路由、节点组故障转移与隧道绑定。
              </p>
            </div>
            <Button
              variant="flat"
              color="primary"
              onPress={() => void loadData()}
              isLoading={saving}
            >
              刷新
            </Button>
          </div>
        </CardBody>
      </Card>

      <div className="flex gap-2 overflow-x-auto pb-1">
        {[
          ["endpoints", "端点组"],
          ["rules", "路由规则集"],
          ["nodes", "节点组"],
          ["bindings", "隧道绑定"],
        ].map(([key, label]) => (
          <Button
            key={key}
            color={tab === key ? "primary" : "default"}
            variant={tab === key ? "solid" : "flat"}
            onPress={() => setTab(key as typeof tab)}
          >
            {label}
          </Button>
        ))}
      </div>

      {tab === "endpoints" && (
        <EndpointGroupsSection
          groups={endpointGroups}
          onAdd={() => setEndpointEditor(endpointGroupDefaults())}
          onEdit={setEndpointEditor}
          onDelete={(id) => void remove("端点组", id, deleteEndpointGroup)}
        />
      )}
      {tab === "rules" && (
        <RuleSetsSection
          ruleSets={ruleSets}
          endpointGroups={endpointGroups}
          onAdd={() => setRuleEditor(ruleSetDefaults())}
          onEdit={setRuleEditor}
          onDelete={(id) => void remove("路由规则集", id, deleteRouteRuleSet)}
        />
      )}
      {tab === "nodes" && (
        <NodeGroupsSection
          groups={nodeGroups}
          nodes={nodes}
          onAdd={() => setNodeEditor(nodeGroupDefaults())}
          onEdit={setNodeEditor}
          onDelete={(id) => void remove("节点组", id, deleteNodeGroup)}
        />
      )}
      {tab === "bindings" && (
        <BindingsSection
          bindings={bindings}
          nodeGroups={nodeGroups}
          tunnels={tunnels}
          onAdd={() => setBindingEditor(bindingDefaults())}
          onEdit={setBindingEditor}
          onDelete={(id) =>
            void remove("隧道节点组绑定", id, deleteTunnelNodeGroupBinding)
          }
        />
      )}

      <EndpointEditor
        value={endpointEditor}
        saving={saving}
        onClose={() => setEndpointEditor(null)}
        onSave={(value) =>
          void save(() =>
            "id" in value && value.id
              ? updateEndpointGroup(value)
              : createEndpointGroup(value),
          )
        }
      />
      <RuleEditor
        value={ruleEditor}
        saving={saving}
        endpointById={endpointById}
        onClose={() => setRuleEditor(null)}
        onSave={(value) =>
          void save(() =>
            "id" in value && value.id
              ? updateRouteRuleSet(value)
              : createRouteRuleSet(value),
          )
        }
      />
      <NodeEditor
        value={nodeEditor}
        saving={saving}
        nodes={nodes}
        onClose={() => setNodeEditor(null)}
        onSave={(value) =>
          void save(() =>
            "id" in value && value.id
              ? updateNodeGroup(value)
              : createNodeGroup(value),
          )
        }
      />
      <BindingEditor
        value={bindingEditor}
        saving={saving}
        nodeGroups={nodeGroups}
        tunnels={tunnels}
        onClose={() => setBindingEditor(null)}
        onSave={(value) =>
          void save(() =>
            value.id
              ? updateTunnelNodeGroupBinding(value as TunnelNodeGroupBinding)
              : createTunnelNodeGroupBinding(value),
          )
        }
      />
    </div>
  );
}

function SectionHeader({
  title,
  description,
  onAdd,
}: {
  title: string;
  description: string;
  onAdd: () => void;
}) {
  return (
    <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
      <div>
        <h2 className="text-lg font-semibold">{title}</h2>
        <p className="text-sm panel-muted">{description}</p>
      </div>
      <Button color="primary" onPress={onAdd}>
        新增
      </Button>
    </div>
  );
}

function EmptyState({ text }: { text: string }) {
  return (
    <div className="rounded-xl border border-dashed border-slate-300 dark:border-slate-700 p-8 text-center panel-muted">
      {text}
    </div>
  );
}

function EndpointGroupsSection({
  groups,
  onAdd,
  onEdit,
  onDelete,
}: {
  groups: EndpointGroup[];
  onAdd: () => void;
  onEdit: (group: EndpointGroup) => void;
  onDelete: (id: number) => void;
}) {
  return (
    <Card className="panel-shell">
      <CardHeader>
        <SectionHeader
          title="端点组"
          description="为转发提供健康探测、主备与按权重随机选择。"
          onAdd={onAdd}
        />
      </CardHeader>
      <CardBody className="gap-3">
        {groups.length === 0 ? (
          <EmptyState text="暂无端点组" />
        ) : (
          groups.map((group) => (
            <Card
              key={group.id}
              shadow="sm"
              className="border border-slate-200 dark:border-slate-700"
            >
              <CardBody className="gap-3">
                <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-3">
                  <div>
                    <h3 className="font-semibold">{group.name}</h3>
                    <p className="text-sm panel-muted">
                      {group.description || "无描述"} · {group.strategy} ·
                      失败阈值 {group.maxFails}
                    </p>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant="flat"
                      onPress={() => onEdit(group)}
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      color="danger"
                      variant="flat"
                      onPress={() => onDelete(group.id)}
                    >
                      删除
                    </Button>
                  </div>
                </div>
                <div className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">
                  {group.endpoints.map((endpoint) => (
                    <div
                      key={endpoint.id}
                      className="rounded-lg bg-default-50 p-3 text-sm"
                    >
                      <div className="flex justify-between gap-2">
                        <span className="font-medium">{endpoint.name}</span>
                        <span className="panel-muted">
                          权重 {endpoint.weight}
                        </span>
                      </div>
                      <p className="font-mono text-xs mt-1 break-all">
                        {endpoint.address}
                      </p>
                      <p className="text-xs panel-muted mt-1">
                        优先级 {endpoint.priority} ·{" "}
                        {endpoint.backup ? "备用" : "主用"}
                      </p>
                    </div>
                  ))}
                </div>
              </CardBody>
            </Card>
          ))
        )}
      </CardBody>
    </Card>
  );
}

function RuleSetsSection({
  ruleSets,
  endpointGroups,
  onAdd,
  onEdit,
  onDelete,
}: {
  ruleSets: RouteRuleSet[];
  endpointGroups: EndpointGroup[];
  onAdd: () => void;
  onEdit: (set: RouteRuleSet) => void;
  onDelete: (id: number) => void;
}) {
  const endpointNames = new Map(
    endpointGroups.flatMap((group) =>
      group.endpoints.map((endpoint) => [endpoint.id || 0, endpoint.name]),
    ),
  );
  return (
    <Card className="panel-shell">
      <CardHeader>
        <SectionHeader
          title="路由规则集"
          description="按客户端 IP、协议和 TCP 七层属性选择端点。UDP 只使用可观测规则。"
          onAdd={onAdd}
        />
      </CardHeader>
      <CardBody className="gap-3">
        {ruleSets.length === 0 ? (
          <EmptyState text="暂无路由规则集" />
        ) : (
          ruleSets.map((set) => (
            <Card
              key={set.id}
              shadow="sm"
              className="border border-slate-200 dark:border-slate-700"
            >
              <CardBody className="gap-2">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h3 className="font-semibold">{set.name}</h3>
                    <p className="text-sm panel-muted">
                      {set.description || "无描述"} · {set.rules.length} 条规则
                    </p>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant="flat"
                      onPress={() => onEdit(set)}
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      color="danger"
                      variant="flat"
                      onPress={() => onDelete(set.id)}
                    >
                      删除
                    </Button>
                  </div>
                </div>
                {set.rules.map((rule) => (
                  <div
                    key={rule.id}
                    className="rounded-lg bg-default-50 p-3 text-sm"
                  >
                    <span className="font-medium">{rule.name}</span>
                    <span className="panel-muted ml-2">
                      {rule.negate ? "不匹配" : "匹配"} {rule.matchType}
                    </span>
                    <span className="font-mono ml-2 break-all">
                      {rule.value}
                    </span>
                    <p className="text-xs panel-muted mt-1">
                      端点：
                      {rule.endpointIds
                        .map((id) => endpointNames.get(id) || id)
                        .join("、") || "未选择"}
                    </p>
                  </div>
                ))}
              </CardBody>
            </Card>
          ))
        )}
      </CardBody>
    </Card>
  );
}

function NodeGroupsSection({
  groups,
  nodes,
  onAdd,
  onEdit,
  onDelete,
}: {
  groups: NodeGroup[];
  nodes: Option[];
  onAdd: () => void;
  onEdit: (group: NodeGroup) => void;
  onDelete: (id: number) => void;
}) {
  const nodeNames = new Map(nodes.map((node) => [node.id, node.name]));
  return (
    <Card className="panel-shell">
      <CardHeader>
        <SectionHeader
          title="节点组"
          description="为入口、出口或中继链路配置故障转移与主备节点。"
          onAdd={onAdd}
        />
      </CardHeader>
      <CardBody className="gap-3">
        {groups.length === 0 ? (
          <EmptyState text="暂无节点组" />
        ) : (
          groups.map((group) => (
            <Card
              key={group.id}
              shadow="sm"
              className="border border-slate-200 dark:border-slate-700"
            >
              <CardBody className="gap-2">
                <div className="flex items-center justify-between gap-3">
                  <div>
                    <h3 className="font-semibold">{group.name}</h3>
                    <p className="text-sm panel-muted">
                      {group.description || "无描述"} · {group.strategy} ·{" "}
                      {group.members.length} 个节点
                    </p>
                  </div>
                  <div className="flex gap-2">
                    <Button
                      size="sm"
                      variant="flat"
                      onPress={() => onEdit(group)}
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      color="danger"
                      variant="flat"
                      onPress={() => onDelete(group.id)}
                    >
                      删除
                    </Button>
                  </div>
                </div>
                {group.members.map((member) => (
                  <div
                    key={member.nodeId}
                    className="flex justify-between rounded-lg bg-default-50 p-3 text-sm"
                  >
                    <span>
                      {nodeNames.get(member.nodeId) || `节点 ${member.nodeId}`}
                    </span>
                    <span className="panel-muted">
                      优先级 {member.priority} ·{" "}
                      {member.backup ? "备用" : "主用"}
                    </span>
                  </div>
                ))}
              </CardBody>
            </Card>
          ))
        )}
      </CardBody>
    </Card>
  );
}

function BindingsSection({
  bindings,
  nodeGroups,
  tunnels,
  onAdd,
  onEdit,
  onDelete,
}: {
  bindings: TunnelNodeGroupBinding[];
  nodeGroups: NodeGroup[];
  tunnels: Option[];
  onAdd: () => void;
  onEdit: (binding: TunnelNodeGroupBinding) => void;
  onDelete: (id: number) => void;
}) {
  const names = new Map(nodeGroups.map((group) => [group.id, group.name]));
  const tunnelNames = new Map(
    tunnels.map((tunnel) => [tunnel.id, tunnel.name]),
  );
  return (
    <Card className="panel-shell">
      <CardHeader>
        <SectionHeader
          title="隧道节点组绑定"
          description="把节点组展开到隧道拓扑，并配置端口、协议和链路级配额。"
          onAdd={onAdd}
        />
      </CardHeader>
      <CardBody className="gap-3">
        {bindings.length === 0 ? (
          <EmptyState text="暂无隧道节点组绑定" />
        ) : (
          bindings.map((binding) => (
            <div
              key={binding.id}
              className="flex flex-col md:flex-row md:items-center md:justify-between gap-3 rounded-xl border border-slate-200 dark:border-slate-700 p-4"
            >
              <div>
                <p className="font-medium">
                  {tunnelNames.get(binding.tunnelId) ||
                    `隧道 ${binding.tunnelId}`}{" "}
                  → {names.get(binding.groupId) || `节点组 ${binding.groupId}`}
                </p>
                <p className="text-sm panel-muted">
                  {binding.protocol} · 端口 {binding.port} · 链路类型{" "}
                  {binding.chainType} · hop {binding.hopIndex}
                </p>
              </div>
              <div className="flex gap-2">
                <Button
                  size="sm"
                  variant="flat"
                  onPress={() => onEdit(binding)}
                >
                  编辑
                </Button>
                <Button
                  size="sm"
                  color="danger"
                  variant="flat"
                  onPress={() => onDelete(binding.id)}
                >
                  删除
                </Button>
              </div>
            </div>
          ))
        )}
      </CardBody>
    </Card>
  );
}

function EditorModal({
  title,
  isOpen,
  saving,
  onClose,
  onSave,
  children,
}: {
  title: string;
  isOpen: boolean;
  saving: boolean;
  onClose: () => void;
  onSave: () => void;
  children: React.ReactNode;
}) {
  return (
    <Modal
      isOpen={isOpen}
      onClose={onClose}
      size="4xl"
      scrollBehavior="outside"
    >
      <ModalContent>
        <ModalHeader>{title}</ModalHeader>
        <ModalBody className="gap-4">{children}</ModalBody>
        <ModalFooter>
          <Button variant="flat" onPress={onClose}>
            取消
          </Button>
          <Button color="primary" onPress={onSave} isLoading={saving}>
            保存
          </Button>
        </ModalFooter>
      </ModalContent>
    </Modal>
  );
}

function EndpointEditor({
  value,
  saving,
  onClose,
  onSave,
}: {
  value: EndpointGroupRequest | EndpointGroup | null;
  saving: boolean;
  onClose: () => void;
  onSave: (value: EndpointGroupRequest | EndpointGroup) => void;
}) {
  const [form, setForm] = useState<EndpointGroupRequest | EndpointGroup | null>(
    value,
  );
  useEffect(() => setForm(value), [value]);
  if (!form) return null;
  const updateEndpoint = (index: number, patch: Partial<RuntimeEndpoint>) =>
    setForm({
      ...form,
      endpoints: form.endpoints.map((endpoint, itemIndex) =>
        itemIndex === index ? { ...endpoint, ...patch } : endpoint,
      ),
    });
  return (
    <EditorModal
      title={"id" in form ? "编辑端点组" : "新增端点组"}
      isOpen={!!value}
      saving={saving}
      onClose={onClose}
      onSave={() => onSave(form)}
    >
      <div className="grid md:grid-cols-2 gap-3">
        <Input
          label="名称"
          value={form.name}
          onValueChange={(name) => setForm({ ...form, name })}
        />
        <Input
          label="描述"
          value={form.description}
          onValueChange={(description) => setForm({ ...form, description })}
        />
        {strategySelect(form.strategy, (strategy) =>
          setForm({ ...form, strategy }),
        )}
        <Input
          type="number"
          label="最大失败次数"
          min={1}
          value={String(form.maxFails)}
          onValueChange={(value) =>
            setForm({ ...form, maxFails: toInt(value, 1) })
          }
        />
        <Input
          type="number"
          label="失败冷却（毫秒）"
          min={1000}
          value={String(form.failTimeoutMs)}
          onValueChange={(value) =>
            setForm({ ...form, failTimeoutMs: toInt(value, 600000) })
          }
        />
        <Input
          type="number"
          label="探测周期（毫秒）"
          min={1000}
          value={String(form.probeIntervalMs)}
          onValueChange={(value) =>
            setForm({ ...form, probeIntervalMs: toInt(value, 10000) })
          }
        />
        <Input
          type="number"
          label="探测超时（毫秒）"
          min={100}
          value={String(form.probeTimeoutMs)}
          onValueChange={(value) =>
            setForm({ ...form, probeTimeoutMs: toInt(value, 3000) })
          }
        />
        <Switch
          isSelected={form.status === 1}
          onValueChange={(status) =>
            setForm({ ...form, status: status ? 1 : 0 })
          }
        >
          启用端点组
        </Switch>
      </div>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold">端点</h3>
          <Button
            size="sm"
            variant="flat"
            onPress={() =>
              setForm({
                ...form,
                endpoints: [
                  ...form.endpoints,
                  { ...emptyEndpoint(), sortIndex: form.endpoints.length },
                ],
              })
            }
          >
            添加端点
          </Button>
        </div>
        {form.endpoints.map((endpoint, index) => (
          <div
            key={endpoint.id || `new-${index}`}
            className="grid md:grid-cols-4 xl:grid-cols-8 gap-2 rounded-xl border border-slate-200 dark:border-slate-700 p-3"
          >
            <Input
              label="名称"
              value={endpoint.name}
              onValueChange={(name) => updateEndpoint(index, { name })}
            />
            <Input
              label="地址"
              placeholder="host:port"
              value={endpoint.address}
              onValueChange={(address) => updateEndpoint(index, { address })}
            />
            <Input
              type="number"
              label="优先级"
              min={0}
              value={String(endpoint.priority)}
              onValueChange={(value) =>
                updateEndpoint(index, { priority: toInt(value) })
              }
            />
            <Input
              type="number"
              label="权重 1-100"
              min={1}
              max={100}
              value={String(endpoint.weight)}
              onValueChange={(value) =>
                updateEndpoint(index, { weight: toInt(value, 1) })
              }
            />
            <Input
              type="number"
              label="排序"
              min={0}
              value={String(endpoint.sortIndex)}
              onValueChange={(value) =>
                updateEndpoint(index, { sortIndex: toInt(value) })
              }
            />
            <Switch
              size="sm"
              isSelected={endpoint.status === 1}
              onValueChange={(status) =>
                updateEndpoint(index, { status: status ? 1 : 0 })
              }
            >
              启用
            </Switch>
            <Switch
              size="sm"
              isSelected={endpoint.backup === 1}
              onValueChange={(backup) =>
                updateEndpoint(index, { backup: backup ? 1 : 0 })
              }
            >
              备用
            </Switch>
            <Button
              size="sm"
              color="danger"
              variant="light"
              onPress={() =>
                setForm({
                  ...form,
                  endpoints: form.endpoints.filter(
                    (_, itemIndex) => itemIndex !== index,
                  ),
                })
              }
              isDisabled={form.endpoints.length <= 1}
            >
              移除
            </Button>
          </div>
        ))}
      </div>
    </EditorModal>
  );
}

function RuleEditor({
  value,
  saving,
  endpointById,
  onClose,
  onSave,
}: {
  value: RouteRuleSetRequest | RouteRuleSet | null;
  saving: boolean;
  endpointById: Map<number, RuntimeEndpoint>;
  onClose: () => void;
  onSave: (value: RouteRuleSetRequest | RouteRuleSet) => void;
}) {
  const [form, setForm] = useState<RouteRuleSetRequest | RouteRuleSet | null>(
    value,
  );
  useEffect(() => setForm(value), [value]);
  if (!form) return null;
  const updateRule = (index: number, patch: Partial<RouteRule>) =>
    setForm({
      ...form,
      rules: form.rules.map((rule, itemIndex) =>
        itemIndex === index ? { ...rule, ...patch } : rule,
      ),
    });
  return (
    <EditorModal
      title={"id" in form ? "编辑路由规则集" : "新增路由规则集"}
      isOpen={!!value}
      saving={saving}
      onClose={onClose}
      onSave={() => onSave(form)}
    >
      <div className="grid md:grid-cols-2 gap-3">
        <Input
          label="名称"
          value={form.name}
          onValueChange={(name) => setForm({ ...form, name })}
        />
        <Input
          label="描述"
          value={form.description}
          onValueChange={(description) => setForm({ ...form, description })}
        />
        <Switch
          isSelected={form.status === 1}
          onValueChange={(status) =>
            setForm({ ...form, status: status ? 1 : 0 })
          }
        >
          启用规则集
        </Switch>
      </div>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold">规则</h3>
          <Button
            size="sm"
            variant="flat"
            onPress={() =>
              setForm({
                ...form,
                rules: [
                  ...form.rules,
                  { ...emptyRule(), sortIndex: form.rules.length },
                ],
              })
            }
          >
            添加规则
          </Button>
        </div>
        {form.rules.map((rule, index) => (
          <div
            key={rule.id || `new-${index}`}
            className="space-y-2 rounded-xl border border-slate-200 dark:border-slate-700 p-3"
          >
            <div className="grid md:grid-cols-5 gap-2">
              <Input
                label="名称"
                value={rule.name}
                onValueChange={(name) => updateRule(index, { name })}
              />
              <Select
                label="匹配类型"
                selectedKeys={new Set([rule.matchType])}
                onSelectionChange={(keys) => {
                  const selected = Array.from(keys)[0];
                  if (selected)
                    updateRule(index, {
                      matchType: String(selected) as RouteRule["matchType"],
                    });
                }}
              >
                {ruleOptions.map(([key, label]) => (
                  <SelectItem key={key}>{label}</SelectItem>
                ))}
              </Select>
              <Input
                label="匹配值"
                value={rule.value}
                onValueChange={(value) => updateRule(index, { value })}
              />
              <Input
                label="第二参数"
                description="Header/Query 的匹配值，其余类型留空"
                value={rule.secondaryValue}
                onValueChange={(secondaryValue) =>
                  updateRule(index, { secondaryValue })
                }
              />
              <Input
                label="优先级"
                type="number"
                min={1}
                value={String(rule.priority)}
                onValueChange={(value) =>
                  updateRule(index, { priority: toInt(value, 100) })
                }
              />
              <Input
                label="排序"
                type="number"
                min={0}
                value={String(rule.sortIndex)}
                onValueChange={(value) =>
                  updateRule(index, { sortIndex: toInt(value) })
                }
              />
            </div>
            <div className="flex flex-wrap items-center gap-4">
              <Switch
                size="sm"
                isSelected={rule.negate === 1}
                onValueChange={(negate) =>
                  updateRule(index, { negate: negate ? 1 : 0 })
                }
              >
                否定匹配
              </Switch>
              <Switch
                size="sm"
                isSelected={rule.status === 1}
                onValueChange={(status) =>
                  updateRule(index, { status: status ? 1 : 0 })
                }
              >
                启用
              </Switch>
              <Select
                className="min-w-60"
                label="目标端点"
                selectionMode="multiple"
                selectedKeys={new Set(rule.endpointIds.map(String))}
                onSelectionChange={(keys) =>
                  updateRule(index, {
                    endpointIds: Array.from(keys).map(Number),
                  })
                }
              >
                {Array.from(endpointById.entries()).map(([id, endpoint]) => (
                  <SelectItem key={String(id)}>
                    {endpoint.name} · {endpoint.address}
                  </SelectItem>
                ))}
              </Select>
              <Button
                size="sm"
                color="danger"
                variant="light"
                onPress={() =>
                  setForm({
                    ...form,
                    rules: form.rules.filter(
                      (_, itemIndex) => itemIndex !== index,
                    ),
                  })
                }
                isDisabled={form.rules.length <= 1}
              >
                移除规则
              </Button>
            </div>
          </div>
        ))}
      </div>
    </EditorModal>
  );
}

function NodeEditor({
  value,
  saving,
  nodes,
  onClose,
  onSave,
}: {
  value: NodeGroupRequest | NodeGroup | null;
  saving: boolean;
  nodes: Option[];
  onClose: () => void;
  onSave: (value: NodeGroupRequest | NodeGroup) => void;
}) {
  const [form, setForm] = useState<NodeGroupRequest | NodeGroup | null>(value);
  useEffect(() => setForm(value), [value]);
  if (!form) return null;
  const updateMember = (index: number, patch: Partial<NodeGroupMember>) =>
    setForm({
      ...form,
      members: form.members.map((member, itemIndex) =>
        itemIndex === index ? { ...member, ...patch } : member,
      ),
    });
  return (
    <EditorModal
      title={"id" in form ? "编辑节点组" : "新增节点组"}
      isOpen={!!value}
      saving={saving}
      onClose={onClose}
      onSave={() => onSave(form)}
    >
      <div className="grid md:grid-cols-2 gap-3">
        <Input
          label="名称"
          value={form.name}
          onValueChange={(name) => setForm({ ...form, name })}
        />
        <Input
          label="描述"
          value={form.description}
          onValueChange={(description) => setForm({ ...form, description })}
        />
        {strategySelect(form.strategy, (strategy) =>
          setForm({ ...form, strategy }),
        )}
        <Input
          type="number"
          label="最大失败次数"
          min={1}
          value={String(form.maxFails)}
          onValueChange={(value) =>
            setForm({ ...form, maxFails: toInt(value, 1) })
          }
        />
        <Input
          type="number"
          label="失败冷却（毫秒）"
          min={1000}
          value={String(form.failTimeoutMs)}
          onValueChange={(value) =>
            setForm({ ...form, failTimeoutMs: toInt(value, 600000) })
          }
        />
        <Switch
          isSelected={form.status === 1}
          onValueChange={(status) =>
            setForm({ ...form, status: status ? 1 : 0 })
          }
        >
          启用节点组
        </Switch>
      </div>
      <div className="space-y-3">
        <div className="flex items-center justify-between">
          <h3 className="font-semibold">成员节点</h3>
          <Button
            size="sm"
            variant="flat"
            onPress={() =>
              setForm({
                ...form,
                members: [...form.members, emptyMember(form.members.length)],
              })
            }
          >
            添加节点
          </Button>
        </div>
        {form.members.map((member, index) => (
          <div
            key={`${member.nodeId}-${index}`}
            className="grid md:grid-cols-5 gap-2 rounded-xl border border-slate-200 dark:border-slate-700 p-3"
          >
            <Select
              label="节点"
              selectedKeys={
                member.nodeId ? new Set([String(member.nodeId)]) : new Set()
              }
              onSelectionChange={(keys) => {
                const selected = Array.from(keys)[0];
                if (selected) updateMember(index, { nodeId: Number(selected) });
              }}
            >
              {nodes.map((node) => (
                <SelectItem key={String(node.id)}>{node.name}</SelectItem>
              ))}
            </Select>
            <Input
              type="number"
              label="优先级"
              min={0}
              value={String(member.priority)}
              onValueChange={(value) =>
                updateMember(index, { priority: toInt(value) })
              }
            />
            <Input
              type="number"
              label="排序"
              min={0}
              value={String(member.sortIndex)}
              onValueChange={(value) =>
                updateMember(index, { sortIndex: toInt(value) })
              }
            />
            <Switch
              size="sm"
              isSelected={member.backup === 1}
              onValueChange={(backup) =>
                updateMember(index, { backup: backup ? 1 : 0 })
              }
            >
              备用
            </Switch>
            <Button
              size="sm"
              color="danger"
              variant="light"
              onPress={() =>
                setForm({
                  ...form,
                  members: form.members.filter(
                    (_, itemIndex) => itemIndex !== index,
                  ),
                })
              }
              isDisabled={form.members.length <= 1}
            >
              移除
            </Button>
          </div>
        ))}
      </div>
    </EditorModal>
  );
}

function BindingEditor({
  value,
  saving,
  nodeGroups,
  tunnels,
  onClose,
  onSave,
}: {
  value: (Omit<TunnelNodeGroupBinding, "id"> & { id?: number }) | null;
  saving: boolean;
  nodeGroups: NodeGroup[];
  tunnels: Option[];
  onClose: () => void;
  onSave: (value: Omit<TunnelNodeGroupBinding, "id"> & { id?: number }) => void;
}) {
  const [form, setForm] = useState<
    (Omit<TunnelNodeGroupBinding, "id"> & { id?: number }) | null
  >(value);
  useEffect(() => setForm(value), [value]);
  if (!form) return null;
  const selectedTunnel = tunnels.find((tunnel) => tunnel.id === form.tunnelId);
  const directTunnel = selectedTunnel?.type === 1;
  const isRelay = form.chainType === 2;
  return (
    <EditorModal
      title={form.id ? "编辑隧道节点组绑定" : "新增隧道节点组绑定"}
      isOpen={!!value}
      saving={saving}
      onClose={onClose}
      onSave={() => onSave(form)}
    >
      <div className="grid md:grid-cols-2 gap-3">
        <Select
          label="隧道"
          selectedKeys={
            form.tunnelId ? new Set([String(form.tunnelId)]) : new Set()
          }
          onSelectionChange={(keys) => {
            const selected = Array.from(keys)[0];
            if (selected) {
              const tunnelId = Number(selected);
              const tunnel = tunnels.find((item) => item.id === tunnelId);
              setForm({
                ...form,
                tunnelId,
                chainType: tunnel?.type === 1 ? 1 : form.chainType,
                hopIndex: tunnel?.type === 1 ? 0 : form.hopIndex,
              });
            }
          }}
        >
          {tunnels.map((tunnel) => (
            <SelectItem key={String(tunnel.id)}>{tunnel.name}</SelectItem>
          ))}
        </Select>
        <Select
          label="节点组"
          selectedKeys={
            form.groupId ? new Set([String(form.groupId)]) : new Set()
          }
          onSelectionChange={(keys) => {
            const selected = Array.from(keys)[0];
            if (selected) setForm({ ...form, groupId: Number(selected) });
          }}
        >
          {nodeGroups.map((group) => (
            <SelectItem key={String(group.id)}>{group.name}</SelectItem>
          ))}
        </Select>
        <Select
          label="链路类型"
          selectedKeys={new Set([String(form.chainType)])}
          onSelectionChange={(keys) => {
            const selected = Array.from(keys)[0];
            if (selected) {
              const chainType = Number(selected);
              setForm({
                ...form,
                chainType,
                hopIndex: chainType === 2 ? Math.max(1, form.hopIndex) : 0,
              });
            }
          }}
        >
          <SelectItem key="1">入口</SelectItem>
          {!directTunnel ? <SelectItem key="2">中继</SelectItem> : null}
          {!directTunnel ? <SelectItem key="3">出口</SelectItem> : null}
        </Select>
        {strategySelect(form.strategy, (strategy) =>
          setForm({ ...form, strategy }),
        )}
        <Input
          type="number"
          label="监听端口"
          min={1}
          max={65535}
          value={String(form.port || "")}
          onValueChange={(value) => setForm({ ...form, port: toInt(value) })}
        />
        <Input
          type="number"
          label="Hop 索引"
          min={isRelay ? 1 : 0}
          isDisabled={!isRelay}
          value={String(form.hopIndex)}
          onValueChange={(value) =>
            setForm({ ...form, hopIndex: toInt(value) })
          }
        />
        <Select
          label="协议"
          selectedKeys={new Set([form.protocol])}
          onSelectionChange={(keys) => {
            const selected = Array.from(keys)[0];
            if (selected)
              setForm({
                ...form,
                protocol: String(
                  selected,
                ) as TunnelNodeGroupBinding["protocol"],
              });
          }}
        >
          <SelectItem key="tcp">TCP</SelectItem>
          <SelectItem key="udp+quic">UDP + QUIC</SelectItem>
          <SelectItem key="udp+kcp">UDP + KCP</SelectItem>
          <SelectItem key="mptcp">MPTCP</SelectItem>
        </Select>
        <Input
          type="number"
          label="流量配额（字节）"
          min={0}
          value={String(form.flowQuotaBytes)}
          onValueChange={(value) =>
            setForm({ ...form, flowQuotaBytes: toInt(value) })
          }
        />
        <Input
          type="number"
          label="限速（Mbps）"
          min={0}
          value={String(form.speedLimitMbps)}
          onValueChange={(value) =>
            setForm({ ...form, speedLimitMbps: toInt(value) })
          }
        />
      </div>
      <Alert color="primary" variant="flat">
        直连隧道只能配置入口绑定；中继绑定的 Hop 索引从 1 开始，入口和出口必须为
        0。
      </Alert>
    </EditorModal>
  );
}
