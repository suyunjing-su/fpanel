import { useState, useEffect, useMemo } from "react";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Button } from "@heroui/button";
import { Input } from "@heroui/input";
import { Select, SelectItem } from "@heroui/select";
import { Modal, ModalContent, ModalHeader, ModalBody, ModalFooter } from "@heroui/modal";
import { Chip } from "@heroui/chip";
import { Spinner } from "@heroui/spinner";
import toast from 'react-hot-toast';


import { 
  batchDeleteSpeedLimits,
  createSpeedLimit, 
  getAllUsers,
  getSpeedLimitList, 
  getSpeedLimitUserTunnelEntryPolicies,
  getSpeedLimitUserTunnelExitPolicies,
  getSpeedLimitUserTunnelPolicies,
  updateSpeedLimit, 
  updateSpeedLimitUserTunnelEntryPolicy,
  updateSpeedLimitUserTunnelExitPolicy,
  updateSpeedLimitUserTunnelPolicy,
  deleteSpeedLimit, 
  getTunnelList 
} from "@/api";
import { useBatchDeleteSelection } from "@/hooks/useBatchDeleteSelection";
import { PANEL_CARD_GRID_CLASS } from "@/config/layout";

interface SpeedLimitRule {
  id: number;
  name: string;
  speed: number;
  status: number;
  tunnelId: number;
  tunnelName: string;
  createdTime: string;
  updatedTime: string;
}

interface Tunnel {
  id: number;
  name: string;
}

interface UserOption {
  id: number;
  user: string;
}

interface UserTunnelPolicy {
  id: number;
  tunnelId: number;
  tunnelName: string;
  flow: number;
  num: number;
  flowResetTime: number;
  expTime: number;
  speedId?: number | null;
  speedLimitName?: string;
  status: number;
}

interface UserTunnelExitPolicy {
  id: number;
  userTunnelId: number;
  tunnelId: number;
  exitNodeId: number;
  exitNodeName?: string;
  flowQuotaGb?: number | null;
  usedFlow?: number;
  status: number;
  healthStatus?: number;
  lastLatencyMs?: number | null;
}

interface UserTunnelEntryPolicy {
  id: number;
  userTunnelId: number;
  tunnelId: number;
  entryNodeId: number;
  entryNodeName?: string;
  speedLimitMbps?: number | null;
  flowQuotaGb?: number | null;
  usedFlow?: number;
  status: number;
}

interface SpeedLimitForm {
  id?: number;
  name: string;
  speed: number;
  tunnelId: number | null;
  tunnelName: string;
  status: number;
}

export default function LimitPage() {
  const [loading, setLoading] = useState(true);
  const [rules, setRules] = useState<SpeedLimitRule[]>([]);
  const [tunnels, setTunnels] = useState<Tunnel[]>([]);
  const [users, setUsers] = useState<UserOption[]>([]);
  const [selectedUserId, setSelectedUserId] = useState<number | null>(null);
  const [userTunnelPolicies, setUserTunnelPolicies] = useState<UserTunnelPolicy[]>([]);
  const [userPolicyLoading, setUserPolicyLoading] = useState(false);
  const [policyModalOpen, setPolicyModalOpen] = useState(false);
  const [policySubmitting, setPolicySubmitting] = useState(false);
  const [editingPolicy, setEditingPolicy] = useState<UserTunnelPolicy | null>(null);
  const [editingPolicyForm, setEditingPolicyForm] = useState({
    flow: 0,
    num: 0,
    flowResetTime: 0,
    expTime: '',
    speedId: null as number | null,
    status: 1
  });
  const [exitPolicies, setExitPolicies] = useState<UserTunnelExitPolicy[]>([]);
  const [entryPolicies, setEntryPolicies] = useState<UserTunnelEntryPolicy[]>([]);
  const [entryPolicyLoading, setEntryPolicyLoading] = useState(false);
  const [entryPolicySavingId, setEntryPolicySavingId] = useState<number | null>(null);
  const [exitPolicyLoading, setExitPolicyLoading] = useState(false);
  const [exitPolicySavingId, setExitPolicySavingId] = useState<number | null>(null);
  
  // 模态框状态
  const [modalOpen, setModalOpen] = useState(false);
  const [deleteModalOpen, setDeleteModalOpen] = useState(false);
  const [isEdit, setIsEdit] = useState(false);
  const [submitLoading, setSubmitLoading] = useState(false);
  const [deleteLoading, setDeleteLoading] = useState(false);
  const [ruleToDelete, setRuleToDelete] = useState<SpeedLimitRule | null>(null);
  
  // 表单状态
  const [form, setForm] = useState<SpeedLimitForm>({
    name: '',
    speed: 100,
    tunnelId: null,
    tunnelName: '',
    status: 1
  });
  
  // 表单验证错误
  const [errors, setErrors] = useState<{[key: string]: string}>({});
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState<'all' | 'running' | 'abnormal'>('all');
  const [tunnelFilter, setTunnelFilter] = useState<number | 'all'>('all');

  useEffect(() => {
    loadData();
  }, []);

  useEffect(() => {
    if (selectedUserId) {
      loadUserTunnelPolicies(selectedUserId);
    } else {
      setUserTunnelPolicies([]);
    }
  }, [selectedUserId]);

  // 加载所有数据
  const loadData = async () => {
    setLoading(true);
    try {
      const [rulesRes, tunnelsRes, usersRes] = await Promise.all([
        getSpeedLimitList(),
        getTunnelList(),
        getAllUsers()
      ]);
      
      if (rulesRes.code === 0) {
        const latestRules: SpeedLimitRule[] = rulesRes.data || [];
        setRules(latestRules);
      } else {
        toast.error(rulesRes.msg || '获取限速规则失败');
      }
      
      if (tunnelsRes.code === 0) {
        setTunnels(tunnelsRes.data || []);
      } else {
        console.warn('获取隧道列表失败:', tunnelsRes.msg);
      }

      if (usersRes.code === 0) {
        const userItems: UserOption[] = (usersRes.data || [])
          .filter((item: any) => item.roleId !== 0)
          .map((item: any) => ({ id: item.id, user: item.user }));
        setUsers(userItems);
        if (userItems.length > 0 && !selectedUserId) {
          setSelectedUserId(userItems[0].id);
        }
      } else {
        toast.error(usersRes.msg || '获取用户列表失败');
      }
    } catch (error) {
      console.error('加载数据失败:', error);
      toast.error('加载数据失败');
    } finally {
      setLoading(false);
    }
  };

  const batchSelection = useBatchDeleteSelection<SpeedLimitRule>({
    items: rules,
    entityLabel: '限速规则',
    batchDeleteApi: batchDeleteSpeedLimits,
    reloadData: loadData
  });

  const loadUserTunnelPolicies = async (userId: number) => {
    setUserPolicyLoading(true);
    try {
      const response = await getSpeedLimitUserTunnelPolicies({ userId });
      if (response.code === 0) {
        setUserTunnelPolicies(response.data || []);
      } else {
        toast.error(response.msg || '获取用户隧道策略失败');
      }
    } catch (error) {
      console.error('加载用户隧道策略失败:', error);
      toast.error('加载用户隧道策略失败');
    } finally {
      setUserPolicyLoading(false);
    }
  };

  const loadExitPolicies = async (userTunnelId: number) => {
    setExitPolicyLoading(true);
    try {
      const response = await getSpeedLimitUserTunnelExitPolicies({ userTunnelId });
      if (response.code === 0) {
        setExitPolicies(response.data || []);
      } else {
        toast.error(response.msg || '获取出口配额策略失败');
      }
    } catch (error) {
      console.error('加载出口配额策略失败:', error);
      toast.error('加载出口配额策略失败');
    } finally {
      setExitPolicyLoading(false);
    }
  };

  const loadEntryPolicies = async (userTunnelId: number) => {
    setEntryPolicyLoading(true);
    try {
      const response = await getSpeedLimitUserTunnelEntryPolicies({ userTunnelId });
      if (response.code === 0) {
        setEntryPolicies(response.data || []);
      } else {
        toast.error(response.msg || '获取入口策略失败');
      }
    } catch (error) {
      console.error('加载入口策略失败:', error);
      toast.error('加载入口策略失败');
    } finally {
      setEntryPolicyLoading(false);
    }
  };

  const openPolicyModal = async (policy: UserTunnelPolicy) => {
    setEditingPolicy(policy);
    setEditingPolicyForm({
      flow: policy.flow,
      num: policy.num,
      flowResetTime: policy.flowResetTime,
      expTime: policy.expTime ? new Date(policy.expTime).toISOString().slice(0, 16) : '',
      speedId: policy.speedId ?? null,
      status: policy.status
    });
    await Promise.all([loadEntryPolicies(policy.id), loadExitPolicies(policy.id)]);
    setPolicyModalOpen(true);
  };

  const updateEntryPolicyDraft = (id: number, patch: Partial<UserTunnelEntryPolicy>) => {
    setEntryPolicies((prev) => prev.map((item) => (item.id === id ? { ...item, ...patch } : item)));
  };

  const handleEntryPolicySave = async (item: UserTunnelEntryPolicy) => {
    setEntryPolicySavingId(item.id);
    try {
      const response = await updateSpeedLimitUserTunnelEntryPolicy({
        id: item.id,
        speedLimitMbps: item.speedLimitMbps ?? 0,
        flowQuotaGb: item.flowQuotaGb ?? 0,
        status: item.status
      });
      if (response.code === 0) {
        toast.success(`入口 ${item.entryNodeName || item.entryNodeId} 策略已更新`);
        if (editingPolicy) {
          loadEntryPolicies(editingPolicy.id);
        }
      } else {
        toast.error(response.msg || '更新入口策略失败');
      }
    } catch (error) {
      console.error('更新入口策略失败:', error);
      toast.error('更新入口策略失败');
    } finally {
      setEntryPolicySavingId(null);
    }
  };

  const handlePolicySubmit = async () => {
    if (!editingPolicy) {
      return;
    }
    setPolicySubmitting(true);
    try {
      const response = await updateSpeedLimitUserTunnelPolicy({
        id: editingPolicy.id,
        flow: editingPolicyForm.flow,
        num: editingPolicyForm.num,
        flowResetTime: editingPolicyForm.flowResetTime,
        expTime: editingPolicyForm.expTime ? new Date(editingPolicyForm.expTime).getTime() : editingPolicy.expTime,
        speedId: editingPolicyForm.speedId,
        status: editingPolicyForm.status
      });
      if (response.code === 0) {
        toast.success('用户隧道策略更新成功');
        setPolicyModalOpen(false);
        if (selectedUserId) {
          loadUserTunnelPolicies(selectedUserId);
        }
      } else {
        toast.error(response.msg || '更新用户隧道策略失败');
      }
    } catch (error) {
      console.error('更新用户隧道策略失败:', error);
      toast.error('更新用户隧道策略失败');
    } finally {
      setPolicySubmitting(false);
    }
  };

  const updateExitPolicyDraft = (id: number, patch: Partial<UserTunnelExitPolicy>) => {
    setExitPolicies((prev) => prev.map((item) => (item.id === id ? { ...item, ...patch } : item)));
  };

  const handleExitPolicySave = async (item: UserTunnelExitPolicy) => {
    setExitPolicySavingId(item.id);
    try {
      const response = await updateSpeedLimitUserTunnelExitPolicy({
        id: item.id,
        flowQuotaGb: item.flowQuotaGb ?? 0,
        status: item.status
      });
      if (response.code === 0) {
        toast.success(`出口 ${item.exitNodeName || item.exitNodeId} 配额策略已更新`);
        if (editingPolicy) {
          loadExitPolicies(editingPolicy.id);
        }
      } else {
        toast.error(response.msg || '更新出口配额策略失败');
      }
    } catch (error) {
      console.error('更新出口配额策略失败:', error);
      toast.error('更新出口配额策略失败');
    } finally {
      setExitPolicySavingId(null);
    }
  };

  const filteredRules = useMemo(() => {
    const lowerKeyword = keyword.trim().toLowerCase();
    return rules.filter((rule) => {
      if (statusFilter === 'running' && rule.status !== 1) {
        return false;
      }
      if (statusFilter === 'abnormal' && rule.status === 1) {
        return false;
      }
      if (tunnelFilter !== 'all' && rule.tunnelId !== tunnelFilter) {
        return false;
      }
      if (!lowerKeyword) {
        return true;
      }
      return [rule.name, rule.tunnelName, String(rule.speed)].some((field) =>
        field?.toLowerCase().includes(lowerKeyword)
      );
    });
  }, [rules, keyword, statusFilter, tunnelFilter]);

  // 表单验证
  const validateForm = (): boolean => {
    const newErrors: {[key: string]: string} = {};
    
    if (!form.name.trim()) {
      newErrors.name = '请输入规则名称';
    } else if (form.name.length < 2 || form.name.length > 50) {
      newErrors.name = '规则名称长度应在2-50个字符之间';
    }
    
    if (!form.speed || form.speed < 1) {
      newErrors.speed = '请输入有效的速度限制（≥1 Mbps）';
    }
    
    if (!form.tunnelId) {
      newErrors.tunnelId = '请选择要绑定的隧道';
    }
    
    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // 新增规则
  const handleAdd = () => {
    setIsEdit(false);
    setForm({
      name: '',
      speed: 100,
      tunnelId: null,
      tunnelName: '',
      status: 1
    });
    setErrors({});
    setModalOpen(true);
  };

  // 编辑规则
  const handleEdit = (rule: SpeedLimitRule) => {
    setIsEdit(true);
    setForm({
      id: rule.id,
      name: rule.name,
      speed: rule.speed,
      tunnelId: rule.tunnelId,
      tunnelName: rule.tunnelName,
      status: rule.status
    });
    setErrors({});
    setModalOpen(true);
  };

  // 显示删除确认
  const handleDelete = (rule: SpeedLimitRule) => {
    setRuleToDelete(rule);
    setDeleteModalOpen(true);
  };

  // 确认删除规则
  const confirmDelete = async () => {
    if (!ruleToDelete) return;
    
    setDeleteLoading(true);
    try {
      const res = await deleteSpeedLimit(ruleToDelete.id);
      if (res.code === 0) {
        toast.success('删除成功');
        setDeleteModalOpen(false);
        loadData();
      } else {
        toast.error(res.msg || '删除失败');
      }
    } catch (error) {
      console.error('删除失败:', error);
      toast.error('删除失败');
    } finally {
      setDeleteLoading(false);
    }
  };

  // 提交表单
  const handleSubmit = async () => {
    if (!validateForm()) return;
    
    setSubmitLoading(true);
    try {
      let res;
      if (isEdit) {
        res = await updateSpeedLimit(form);
      } else {
        const { id, ...createData } = form;
        res = await createSpeedLimit(createData);
      }
      
      if (res.code === 0) {
        toast.success(isEdit ? '修改成功' : '创建成功');
        setModalOpen(false);
        loadData();
      } else {
        toast.error(res.msg || '操作失败');
      }
    } catch (error) {
      console.error('提交失败:', error);
      toast.error('操作失败');
    } finally {
      setSubmitLoading(false);
    }
  };

  if (loading) {
    return (
      
        <div className="flex items-center justify-center h-64">
          <div className="flex items-center gap-3">
            <Spinner size="sm" />
            <span className="text-default-600">正在加载...</span>
          </div>
        </div>
      
    );
  }

  return (
    
      <div className="px-3 lg:px-6 py-4 lg:py-6 space-y-4">
        <Card className="panel-shell">
          <CardBody className="p-4 lg:p-5">
            <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
              <div>
                <p className="text-xs uppercase tracking-[0.14em] panel-muted">Speed Policy</p>
                <h1 className="text-xl lg:text-2xl font-semibold mt-1">限速规则管理</h1>
              </div>
              <div className="grid grid-cols-3 gap-2 w-full lg:w-auto lg:min-w-[360px]">
                <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-white/70 dark:bg-slate-900/60">
                  <p className="text-xs panel-muted">规则总数</p>
                  <p className="text-sm font-semibold mt-1">{filteredRules.length}</p>
                </div>
                <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-white/70 dark:bg-slate-900/60">
                  <p className="text-xs panel-muted">运行中</p>
                  <p className="text-sm font-semibold mt-1 text-emerald-600 dark:text-emerald-300">{filteredRules.filter((item) => item.status === 1).length}</p>
                </div>
                <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-white/70 dark:bg-slate-900/60">
                  <p className="text-xs panel-muted">异常</p>
                  <p className="text-sm font-semibold mt-1 text-rose-600 dark:text-rose-300">{filteredRules.filter((item) => item.status !== 1).length}</p>
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
            placeholder="搜索规则名、隧道或速率"
            className="w-full lg:max-w-sm"
          />
          <Select
            selectedKeys={new Set([statusFilter])}
            onSelectionChange={(keys) => {
              const value = Array.from(keys)[0] as 'all' | 'running' | 'abnormal' | undefined;
              setStatusFilter(value || 'all');
            }}
            size="sm"
            className="w-full lg:w-[180px]"
            aria-label="规则状态"
          >
            <SelectItem key="all">全部状态</SelectItem>
            <SelectItem key="running">运行中</SelectItem>
            <SelectItem key="abnormal">异常</SelectItem>
          </Select>
          <Select
            selectedKeys={new Set([String(tunnelFilter)])}
            onSelectionChange={(keys) => {
              const value = Array.from(keys)[0] as string | undefined;
              setTunnelFilter(value && value !== 'all' ? Number(value) : 'all');
            }}
            size="sm"
            className="w-full lg:w-[220px]"
            aria-label="隧道筛选"
            items={[
              { key: 'all', label: '全部隧道' },
              ...tunnels.map((tunnel) => ({ key: String(tunnel.id), label: tunnel.name }))
            ]}
          >
            {(item) => <SelectItem key={item.key}>{item.label}</SelectItem>}
          </Select>
        </div>

        <div className="flex items-center justify-between gap-3 flex-wrap">
        <div className="text-sm panel-muted">当前显示 {filteredRules.length} 条规则</div>
        <div className="flex items-center gap-3">
        {(keyword || statusFilter !== 'all' || tunnelFilter !== 'all') && (
          <Button
            size="sm"
            variant="flat"
            color="default"
            onPress={() => {
              setKeyword('');
              setStatusFilter('all');
              setTunnelFilter('all');
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
          isDisabled={rules.length === 0}
        >
          {batchSelection.isAllSelected ? '取消全选' : '全选'}
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

        {/* 统一卡片网格 */}
        {filteredRules.length > 0 ? (
          <div className={PANEL_CARD_GRID_CLASS}>
            {filteredRules.map((rule) => (
              <Card key={rule.id} className="panel-shell panel-card-hover">
                <CardHeader className="pb-3">
                  <div className="flex justify-between items-start w-full">
                    <div className="flex items-start gap-2 min-w-0">
                      <input
                        type="checkbox"
                        className="mt-0.5 h-4 w-4 rounded border-default-300 text-danger focus:ring-danger"
                        checked={batchSelection.selectedIds.includes(rule.id)}
                        onChange={() => batchSelection.toggleItemSelection(rule.id)}
                        aria-label={`选择限速规则 ${rule.name}`}
                      />
                      <h3 className="font-semibold text-foreground truncate">{rule.name}</h3>
                    </div>
                    <Chip 
                      color={rule.status === 1 ? "success" : "danger"} 
                      variant="flat" 
                      size="sm"
                    >
                      {rule.status === 1 ? '运行' : '异常'}
                    </Chip>
                  </div>
                </CardHeader>
                <CardBody className="pt-0">
                  <div className="space-y-3">
                    <div className="flex justify-between items-center">
                      <span className="text-small text-default-600">速度限制</span>
                      <Chip color="secondary" variant="flat" size="sm">
                        {rule.speed} Mbps
                      </Chip>
                    </div>
                    <div className="flex justify-between items-center">
                      <span className="text-small text-default-600">绑定隧道</span>
                      {rule.tunnelName ? (
                        <Chip color="primary" variant="flat" size="sm">
                          {rule.tunnelName}
                        </Chip>
                      ) : (
                        <span className="text-default-400 text-small">未绑定</span>
                      )}
                    </div>
                  </div>
                  
                  <div className="flex gap-2 mt-4">
                    <Button
                      size="sm"
                      variant="flat"
                      color="primary"
                      onPress={() => handleEdit(rule)}
                      className="flex-1"
                      startContent={
                        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                          <path d="M13.586 3.586a2 2 0 112.828 2.828l-.793.793-2.828-2.828.793-.793zM11.379 5.793L3 14.172V17h2.828l8.38-8.379-2.83-2.828z" />
                        </svg>
                      }
                    >
                      编辑
                    </Button>
                    <Button
                      size="sm"
                      variant="flat"
                      color="danger"
                      onPress={() => handleDelete(rule)}
                      className="flex-1"
                      startContent={
                        <svg className="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
                          <path fillRule="evenodd" d="M9 2a1 1 0 000 2h2a1 1 0 100-2H9z" clipRule="evenodd" />
                          <path fillRule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8 7a1 1 0 012 0v4a1 1 0 11-2 0V7zM12 7a1 1 0 012 0v4a1 1 0 11-2 0V7z" clipRule="evenodd" />
                        </svg>
                      }
                    >
                      删除
                    </Button>
                  </div>
                </CardBody>
              </Card>
            ))}
          </div>
        ) : (
          /* 空状态 */
          <Card className="panel-shell">
            <CardBody className="text-center py-16">
              <div className="flex flex-col items-center gap-4">
                <div className="w-16 h-16 bg-default-100 rounded-full flex items-center justify-center">
                  <svg className="w-8 h-8 text-default-400" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M12 6v6l4 2m6-6a9 9 0 11-18 0 9 9 0 0118 0z" />
                  </svg>
                </div>
                <div>
                  <h3 className="text-lg font-semibold text-foreground">暂无限速规则</h3>
                  <p className="text-default-500 text-sm mt-1">还没有创建任何限速规则，点击上方按钮开始创建</p>
                </div>
              </div>
            </CardBody>
          </Card>
        )}

        <Card className="panel-shell">
          <CardHeader className="pb-2">
            <div>
              <p className="text-xs uppercase tracking-[0.14em] panel-muted">Tunnel Policy</p>
              <h2 className="text-lg font-semibold mt-1">用户隧道策略（限速管理入口）</h2>
            </div>
          </CardHeader>
          <CardBody className="space-y-4">
            <div className="flex flex-col lg:flex-row gap-3 lg:items-center">
              <Select
                label="选择用户"
                selectedKeys={selectedUserId ? [selectedUserId.toString()] : []}
                onSelectionChange={(keys) => {
                  const selectedKey = Array.from(keys)[0] as string | undefined;
                  setSelectedUserId(selectedKey ? Number(selectedKey) : null);
                }}
                className="w-full lg:w-80"
                variant="bordered"
              >
                {users.map((item) => (
                  <SelectItem key={item.id}>{item.user}</SelectItem>
                ))}
              </Select>
              <Button
                color="primary"
                variant="flat"
                onPress={() => selectedUserId && loadUserTunnelPolicies(selectedUserId)}
                isDisabled={!selectedUserId}
              >
                刷新策略
              </Button>
            </div>

            {userPolicyLoading ? (
              <div className="py-6 flex items-center gap-3 text-default-500">
                <Spinner size="sm" />
                正在加载用户隧道策略...
              </div>
            ) : userTunnelPolicies.length === 0 ? (
              <div className="py-6 text-default-500 text-sm">当前用户暂无隧道策略</div>
            ) : (
              <div className={PANEL_CARD_GRID_CLASS}>
                {userTunnelPolicies.map((item) => (
                  <Card key={item.id} className="panel-shell panel-card-hover">
                    <CardHeader className="pb-2">
                      <div className="flex items-center justify-between gap-3 w-full">
                        <div className="min-w-0">
                          <p className="font-semibold text-foreground truncate text-sm">{item.tunnelName}</p>
                          <p className="text-xs text-default-500">策略ID: {item.id}</p>
                        </div>
                        <Chip color={item.status === 1 ? 'success' : 'danger'} variant="flat" size="sm" className="text-xs">
                          {item.status === 1 ? '启用' : '禁用'}
                        </Chip>
                      </div>
                    </CardHeader>
                    <CardBody className="pt-0 pb-3 space-y-3">
                      <div className="grid grid-cols-2 gap-2 text-sm">
                        <div>配额: {item.flow} GB</div>
                        <div>数量: {item.num}</div>
                        <div>限速: {item.speedLimitName || '不限速'}</div>
                        <div>重置日: {item.flowResetTime === 0 ? '不重置' : `每月${item.flowResetTime}号`}</div>
                      </div>
                      <Button size="sm" color="primary" variant="flat" onPress={() => openPolicyModal(item)}>
                        编辑用户隧道/入口/出口策略
                      </Button>
                    </CardBody>
                  </Card>
                ))}
              </div>
            )}
          </CardBody>
        </Card>

        {/* 新增/编辑模态框 */}
        <Modal 
          isOpen={modalOpen}
          onOpenChange={setModalOpen}
          size="2xl"
        scrollBehavior="outside"
        backdrop="blur"
        placement="center"
        >
          <ModalContent>
            {(onClose) => (
              <>
                <ModalHeader className="flex flex-col gap-1">
                  <h2 className="text-xl font-bold">
                    {isEdit ? '编辑限速规则' : '新增限速规则'}
                  </h2>
                  <p className="text-small text-default-500">
                    {isEdit ? '修改现有限速规则的配置信息' : '创建新的限速规则并绑定到隧道'}
                  </p>
                </ModalHeader>
                <ModalBody>
                  <div className="space-y-4">
                    <Input
                      label="规则名称"
                      placeholder="请输入限速规则名称"
                      value={form.name}
                      onChange={(e) => setForm(prev => ({ ...prev, name: e.target.value }))}
                      isInvalid={!!errors.name}
                      errorMessage={errors.name}
                      variant="bordered"
                    />
                    
                    <Input
                      label="速度限制"
                      placeholder="请输入速度限制"
                      type="number"
                      value={form.speed.toString()}
                      onChange={(e) => setForm(prev => ({ ...prev, speed: parseInt(e.target.value) || 0 }))}
                      isInvalid={!!errors.speed}
                      errorMessage={errors.speed}
                      variant="bordered"
                      endContent={
                        <div className="pointer-events-none flex items-center">
                          <span className="text-default-400 text-small">Mbps</span>
                        </div>
                      }
                    />
                    
                    <Select
                      label="绑定隧道"
                      placeholder="请选择要绑定的隧道"
                      selectedKeys={form.tunnelId ? [form.tunnelId.toString()] : []}
                      onSelectionChange={(keys) => {
                        const selectedKey = Array.from(keys)[0] as string;
                        if (selectedKey) {
                          const selectedTunnel = tunnels.find(tunnel => tunnel.id === parseInt(selectedKey));
                          setForm(prev => ({ 
                            ...prev, 
                            tunnelId: parseInt(selectedKey),
                            tunnelName: selectedTunnel?.name || ''
                          }));
                        } else {
                          setForm(prev => ({ 
                            ...prev, 
                            tunnelId: null,
                            tunnelName: ''
                          }));
                        }
                      }}
                      isInvalid={!!errors.tunnelId}
                      errorMessage={errors.tunnelId}
                      variant="bordered"
                      isDisabled={isEdit}
                      description={isEdit ? "编辑时无法修改绑定隧道" : undefined}
                    >
                      {tunnels.map((tunnel) => (
                        <SelectItem key={tunnel.id}>
                          {tunnel.name}
                        </SelectItem>
                      ))}
                    </Select>
                  </div>
                </ModalBody>
                <ModalFooter>
                  <Button variant="light" onPress={onClose}>
                    取消
                  </Button>
                  <Button 
                    color="primary" 
                    onPress={handleSubmit}
                    isLoading={submitLoading}
                  >
                    {isEdit ? '保存修改' : '创建规则'}
                  </Button>
                </ModalFooter>
              </>
            )}
          </ModalContent>
        </Modal>

        <Modal
          isOpen={policyModalOpen}
          onOpenChange={setPolicyModalOpen}
          size="4xl"
          scrollBehavior="outside"
          backdrop="blur"
          placement="center"
        >
          <ModalContent>
            {(onClose) => (
              <>
                <ModalHeader className="flex flex-col gap-1">
                  <h2 className="text-xl font-bold">编辑用户隧道策略</h2>
                  <p className="text-small text-default-500">
                    {editingPolicy ? `${editingPolicy.tunnelName}（策略ID: ${editingPolicy.id}）` : ''}
                  </p>
                </ModalHeader>
                <ModalBody>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                    <Input
                      label="流量配额 (GB)"
                      type="number"
                      value={editingPolicyForm.flow.toString()}
                      onChange={(e) => setEditingPolicyForm((prev) => ({ ...prev, flow: Number(e.target.value || 0) }))}
                      variant="bordered"
                    />
                    <Input
                      label="转发数量"
                      type="number"
                      value={editingPolicyForm.num.toString()}
                      onChange={(e) => setEditingPolicyForm((prev) => ({ ...prev, num: Number(e.target.value || 0) }))}
                      variant="bordered"
                    />
                    <Input
                      label="流量重置日"
                      type="number"
                      value={editingPolicyForm.flowResetTime.toString()}
                      onChange={(e) => setEditingPolicyForm((prev) => ({ ...prev, flowResetTime: Number(e.target.value || 0) }))}
                      variant="bordered"
                    />
                    <Input
                      label="过期时间"
                      type="datetime-local"
                      value={editingPolicyForm.expTime}
                      onChange={(e) => setEditingPolicyForm((prev) => ({ ...prev, expTime: e.target.value }))}
                      variant="bordered"
                    />
                    <Select
                      label="限速规则"
                      selectedKeys={editingPolicyForm.speedId ? [String(editingPolicyForm.speedId)] : ['none']}
                      onSelectionChange={(keys) => {
                        const selectedKey = Array.from(keys)[0] as string | undefined;
                        setEditingPolicyForm((prev) => ({
                          ...prev,
                          speedId: selectedKey && selectedKey !== 'none' ? Number(selectedKey) : null
                        }));
                      }}
                      variant="bordered"
                      items={[
                        { key: 'none', label: '不限速' },
                        ...rules.map((rule) => ({ key: String(rule.id), label: rule.name }))
                      ]}
                    >
                      {(item) => <SelectItem key={item.key}>{item.label}</SelectItem>}
                    </Select>
                    <Select
                      label="策略状态"
                      selectedKeys={[String(editingPolicyForm.status)]}
                      onSelectionChange={(keys) => {
                        const selectedKey = Array.from(keys)[0] as string | undefined;
                        setEditingPolicyForm((prev) => ({ ...prev, status: selectedKey ? Number(selectedKey) : 1 }));
                      }}
                      variant="bordered"
                    >
                      <SelectItem key="1">启用</SelectItem>
                      <SelectItem key="0">禁用</SelectItem>
                    </Select>
                  </div>

                  <div className="mt-2">
                    <h3 className="text-base font-semibold">用户级入口限速/配额策略</h3>
                    {entryPolicyLoading ? (
                      <div className="mt-3 flex items-center gap-2 text-default-500">
                        <Spinner size="sm" />
                        正在加载入口策略...
                      </div>
                    ) : entryPolicies.length === 0 ? (
                      <p className="mt-3 text-sm text-default-500">暂无可编辑入口策略</p>
                    ) : (
                      <div className="mt-3 space-y-2">
                        {entryPolicies.map((item) => (
                          <Card key={item.id} className="border border-slate-200/80 dark:border-slate-700/70">
                            <CardBody className="space-y-2">
                              <div className="flex items-center justify-between gap-2">
                                <p className="font-medium">{item.entryNodeName || `节点 ${item.entryNodeId}`}</p>
                                <Chip size="sm" color={item.status === 1 ? 'success' : 'danger'} variant="flat">
                                  {item.status === 1 ? '启用' : '禁用'}
                                </Chip>
                              </div>
                              <div className="grid grid-cols-1 md:grid-cols-4 gap-2">
                                <Input
                                  label="入口限速(Mbps)"
                                  type="number"
                                  value={item.speedLimitMbps == null ? '' : String(item.speedLimitMbps)}
                                  placeholder="留空表示不限速"
                                  onChange={(e) => {
                                    const value = e.target.value.trim();
                                    updateEntryPolicyDraft(item.id, { speedLimitMbps: value === '' ? null : Number(value) });
                                  }}
                                  variant="bordered"
                                />
                                <Input
                                  label="入口配额(GB)"
                                  type="number"
                                  value={item.flowQuotaGb == null ? '' : String(item.flowQuotaGb)}
                                  placeholder="留空表示不限额"
                                  onChange={(e) => {
                                    const value = e.target.value.trim();
                                    updateEntryPolicyDraft(item.id, { flowQuotaGb: value === '' ? null : Number(value) });
                                  }}
                                  variant="bordered"
                                />
                                <Input
                                  label="已用流量(字节)"
                                  value={String(item.usedFlow || 0)}
                                  isReadOnly
                                  variant="bordered"
                                />
                                <Select
                                  label="策略状态"
                                  selectedKeys={[String(item.status)]}
                                  onSelectionChange={(keys) => {
                                    const selectedKey = Array.from(keys)[0] as string | undefined;
                                    updateEntryPolicyDraft(item.id, { status: selectedKey ? Number(selectedKey) : 1 });
                                  }}
                                  variant="bordered"
                                >
                                  <SelectItem key="1">启用</SelectItem>
                                  <SelectItem key="0">禁用</SelectItem>
                                </Select>
                              </div>
                              <div className="flex items-center justify-end">
                                <Button
                                  size="sm"
                                  color="primary"
                                  variant="flat"
                                  isLoading={entryPolicySavingId === item.id}
                                  onPress={() => handleEntryPolicySave(item)}
                                >
                                  保存该入口策略
                                </Button>
                              </div>
                            </CardBody>
                          </Card>
                        ))}
                      </div>
                    )}
                  </div>

                  <div className="mt-4">
                    <h3 className="text-base font-semibold">用户级出口配额策略</h3>
                    {exitPolicyLoading ? (
                      <div className="mt-3 flex items-center gap-2 text-default-500">
                        <Spinner size="sm" />
                        正在加载出口配额策略...
                      </div>
                    ) : exitPolicies.length === 0 ? (
                      <p className="mt-3 text-sm text-default-500">暂无可编辑出口策略</p>
                    ) : (
                      <div className="mt-3 space-y-2">
                        {exitPolicies.map((item) => (
                          <Card key={item.id} className="border border-slate-200/80 dark:border-slate-700/70">
                            <CardBody className="space-y-2">
                              <div className="flex items-center justify-between gap-2">
                                <p className="font-medium">{item.exitNodeName || `节点 ${item.exitNodeId}`}</p>
                                <Chip size="sm" color={item.healthStatus === 1 ? 'success' : 'danger'} variant="flat">
                                  {item.healthStatus === 1 ? '健康' : '异常'}
                                </Chip>
                              </div>
                              <div className="grid grid-cols-1 md:grid-cols-3 gap-2">
                                <Input
                                  label="出口配额(GB)"
                                  type="number"
                                  value={item.flowQuotaGb == null ? '' : String(item.flowQuotaGb)}
                                  placeholder="留空表示不限额"
                                  onChange={(e) => {
                                    const value = e.target.value.trim();
                                    updateExitPolicyDraft(item.id, { flowQuotaGb: value === '' ? null : Number(value) });
                                  }}
                                  variant="bordered"
                                />
                                <Input
                                  label="已用流量(字节)"
                                  value={String(item.usedFlow || 0)}
                                  isReadOnly
                                  variant="bordered"
                                />
                                <Select
                                  label="策略状态"
                                  selectedKeys={[String(item.status)]}
                                  onSelectionChange={(keys) => {
                                    const selectedKey = Array.from(keys)[0] as string | undefined;
                                    updateExitPolicyDraft(item.id, { status: selectedKey ? Number(selectedKey) : 1 });
                                  }}
                                  variant="bordered"
                                >
                                  <SelectItem key="1">启用</SelectItem>
                                  <SelectItem key="0">禁用</SelectItem>
                                </Select>
                              </div>
                              <div className="flex items-center justify-end">
                                <Button
                                  size="sm"
                                  color="primary"
                                  variant="flat"
                                  isLoading={exitPolicySavingId === item.id}
                                  onPress={() => handleExitPolicySave(item)}
                                >
                                  保存该出口策略
                                </Button>
                              </div>
                            </CardBody>
                          </Card>
                        ))}
                      </div>
                    )}
                  </div>
                </ModalBody>
                <ModalFooter>
                  <Button variant="light" onPress={onClose}>
                    取消
                  </Button>
                  <Button color="primary" onPress={handlePolicySubmit} isLoading={policySubmitting}>
                    保存用户隧道策略
                  </Button>
                </ModalFooter>
              </>
            )}
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
                  <h2 className="text-lg font-bold text-danger">确认删除</h2>
                </ModalHeader>
                <ModalBody>
                  <p className="text-default-600">
                    确定要删除限速规则 <span className="font-semibold text-foreground">"{ruleToDelete?.name}"</span> 吗？
                  </p>
                  <p className="text-small text-default-500 mt-2">
                    此操作无法撤销，删除后该规则将永久消失。
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
                    确认删除
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
                  <h2 className="text-lg font-bold text-danger">确认批量删除</h2>
                </ModalHeader>
                <ModalBody>
                  <p className="text-default-600">
                    确定要删除已选择的 <span className="font-semibold text-foreground">{batchSelection.selectedCount}</span> 条限速规则吗？
                  </p>
                  <p className="text-small text-default-500 mt-2">
                    此操作无法撤销，删除后对应规则将永久消失。
                  </p>
                  {batchSelection.lastResult && batchSelection.failures.length > 0 && (
                    <div className="mt-3 space-y-2">
                      <p className="text-small text-warning">
                        已成功删除 {batchSelection.lastResult.successCount} 条，失败 {batchSelection.lastResult.failureCount} 条
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
                    {batchSelection.failures.length > 0 ? '关闭' : '取消'}
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
                    {batchSelection.failures.length > 0 ? '已完成' : '确认删除'}
                  </Button>
                </ModalFooter>
              </>
            )}
          </ModalContent>
        </Modal>
      </div>
    
  );
} 