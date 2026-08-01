import { useEffect, useRef, useState } from "react";
import { Button } from "@heroui/button";
import { Card, CardBody, CardHeader } from "@heroui/card";
import {
  Modal,
  ModalBody,
  ModalContent,
  ModalFooter,
  ModalHeader,
  useDisclosure,
} from "@heroui/modal";
import { Divider } from "@heroui/divider";
import { Spinner } from "@heroui/spinner";
import toast from "react-hot-toast";
import { useNavigate } from "react-router-dom";

import {
  downloadDatabaseBackup,
  downloadSupportBundle,
  listMaintenanceRunEvents,
  restoreDatabaseBackup,
  type MaintenanceRunEvent,
} from "@/api";
import { isAdmin } from "@/utils/auth";

const saveDownload = (blob: Blob, filename: string) => {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
};

const formatBytes = (size: number) => {
  if (size < 1024 * 1024) {
    return `${Math.max(1, Math.round(size / 1024))} KB`;
  }
  return `${(size / 1024 / 1024).toFixed(1)} MB`;
};

export default function OperationsPage() {
  const navigate = useNavigate();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { isOpen, onOpen, onOpenChange } = useDisclosure();
  const [selectedFile, setSelectedFile] = useState<File | null>(null);
  const [downloadLoading, setDownloadLoading] = useState<
    "backup" | "support" | null
  >(null);
  const [restoreLoading, setRestoreLoading] = useState(false);
  const [maintenanceLoading, setMaintenanceLoading] = useState(false);
  const [maintenanceEvents, setMaintenanceEvents] = useState<MaintenanceRunEvent[]>([]);

  const loadMaintenanceEvents = async () => {
    setMaintenanceLoading(true);
    try {
      const response = await listMaintenanceRunEvents();
      if (response.code !== 0) {
        toast.error(response.msg || "获取维护运行历史失败");
        return;
      }
      setMaintenanceEvents(response.data || []);
    } catch (error) {
      console.error("获取维护运行历史失败:", error);
      toast.error("获取维护运行历史失败");
    } finally {
      setMaintenanceLoading(false);
    }
  };

  const formatTime = (timestamp: number) =>
    new Date(timestamp).toLocaleString("zh-CN", { hour12: false });

  const maintenanceJobLabel = (job: string) =>
    job === "monthly_traffic_reset" ? "月度流量重置" : "小时流量统计";

  const maintenanceDuration = (event: MaintenanceRunEvent) =>
    `${Math.max(0, event.completedAt - event.startedAt)} ms`;

  useEffect(() => {
    if (!isAdmin()) {
      toast.error("权限不足，只有管理员可以访问此页面");
      navigate("/dashboard", { replace: true });
      return;
    }
    void loadMaintenanceEvents();
  }, [navigate]);

  const handleDownload = async (kind: "backup" | "support") => {
    setDownloadLoading(kind);
    try {
      const response =
        kind === "backup"
          ? await downloadDatabaseBackup()
          : await downloadSupportBundle();
      if (!response.ok || !response.blob) {
        toast.error(response.msg || "下载失败");
        return;
      }
      saveDownload(response.blob, response.filename || `${kind}.bin`);
      toast.success(kind === "backup" ? "数据库备份已下载" : "支持包已下载");
    } catch (error) {
      console.error("下载运维文件失败:", error);
      toast.error("下载失败，请稍后重试");
    } finally {
      setDownloadLoading(null);
    }
  };

  const handleFileChange = (event: React.ChangeEvent<HTMLInputElement>) => {
    const file = event.target.files?.[0] || null;
    setSelectedFile(file);
    if (file) {
      onOpen();
    }
    event.target.value = "";
  };

  const handleRestore = async () => {
    if (!selectedFile) {
      return;
    }
    setRestoreLoading(true);
    try {
      const response = await restoreDatabaseBackup(selectedFile);
      if (response.code !== 0) {
        toast.error(response.msg || "恢复失败");
        return;
      }
      onOpenChange();
      setSelectedFile(null);
      toast.success("数据库已验证，控制面正在重启，请稍候重新登录");
    } catch (error) {
      console.error("恢复数据库失败:", error);
      toast.error("恢复失败，请稍后重试");
    } finally {
      setRestoreLoading(false);
    }
  };

  return (
    <div className="px-3 lg:px-6 py-8">
      <div className="max-w-5xl mx-auto space-y-6">
        <div>
          <p className="text-xs uppercase tracking-[0.14em] panel-muted">
            Operations
          </p>
          <h1 className="text-2xl font-semibold mt-1">运维与恢复</h1>
          <p className="text-sm panel-muted mt-2">
            导出一致性数据库备份、生成脱敏支持包，或从经过校验的备份恢复控制面。
          </p>
        </div>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
          <Card className="panel-shell panel-card-hover">
            <CardHeader className="flex flex-col items-start gap-1 px-5 pt-5">
              <h2 className="text-lg font-semibold">数据库备份</h2>
              <p className="text-sm panel-muted">
                使用 SQLite
                在线备份生成一致性数据库文件，不会读取业务表到浏览器内存。
              </p>
            </CardHeader>
            <Divider />
            <CardBody className="px-5 py-5 flex flex-row items-center justify-between gap-4">
              <p className="text-xs panel-muted">
                建议在升级或大规模变更前下载。
              </p>
              <Button
                color="primary"
                onPress={() => handleDownload("backup")}
                isLoading={downloadLoading === "backup"}
                isDisabled={downloadLoading !== null || restoreLoading}
              >
                下载备份
              </Button>
            </CardBody>
          </Card>

          <Card className="panel-shell panel-card-hover">
            <CardHeader className="flex flex-col items-start gap-1 px-5 pt-5">
              <h2 className="text-lg font-semibold">脱敏支持包</h2>
              <p className="text-sm panel-muted">
                包含运行时、迁移版本、完整性状态与核心表计数，不包含密码、密钥或业务内容。
              </p>
            </CardHeader>
            <Divider />
            <CardBody className="px-5 py-5 flex flex-row items-center justify-between gap-4">
              <p className="text-xs panel-muted">
                用于提交故障信息时提供环境上下文。
              </p>
              <Button
                variant="flat"
                color="primary"
                onPress={() => handleDownload("support")}
                isLoading={downloadLoading === "support"}
                isDisabled={downloadLoading !== null || restoreLoading}
              >
                下载支持包
              </Button>
            </CardBody>
          </Card>
        </div>

        <Card className="panel-shell">
          <CardHeader className="flex flex-row items-start justify-between gap-4 px-5 pt-5">
            <div>
              <h2 className="text-lg font-semibold">维护运行历史</h2>
              <p className="text-sm panel-muted mt-1">
                查看月度流量重置与小时统计的最近执行结果，失败会保留错误摘要以便诊断。
              </p>
            </div>
            <Button
              size="sm"
              variant="flat"
              onPress={() => void loadMaintenanceEvents()}
              isLoading={maintenanceLoading}
              isDisabled={restoreLoading}
            >
              刷新
            </Button>
          </CardHeader>
          <Divider />
          <CardBody className="px-5 py-5">
            {maintenanceEvents.length === 0 ? (
              <p className="text-sm panel-muted">尚无已完成的维护作业。</p>
            ) : (
              <div className="space-y-3">
                {maintenanceEvents.map((event) => (
                  <div
                    key={event.id}
                    className={`rounded-xl border px-4 py-3 ${event.status === "failed"
                      ? "border-danger-200 bg-danger-50/60 dark:border-danger-800 dark:bg-danger-950/20"
                      : "border-success-200 bg-success-50/60 dark:border-success-800 dark:bg-success-950/20"}`}
                  >
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${event.status === "failed"
                          ? "bg-danger-100 text-danger-700 dark:bg-danger-900/60 dark:text-danger-300"
                          : "bg-success-100 text-success-700 dark:bg-success-900/60 dark:text-success-300"}`}>
                          {event.status === "failed" ? "失败" : "成功"}
                        </span>
                        <span className="text-sm font-medium">{maintenanceJobLabel(event.job)}</span>
                      </div>
                      <span className="text-xs panel-muted">{maintenanceDuration(event)}</span>
                    </div>
                    <p className="mt-2 break-words text-sm text-default-700 dark:text-default-300">
                      {event.detail || "未提供详情"}
                    </p>
                    <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1 text-xs panel-muted">
                      <span>周期：{event.periodKey}</span>
                      <span>完成：{formatTime(event.completedAt)}</span>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </CardBody>
        </Card>

        <Card className="panel-shell border-danger-200 dark:border-danger-800">
          <CardHeader className="flex flex-col items-start gap-1 px-5 pt-5">
            <h2 className="text-lg font-semibold text-danger">恢复数据库</h2>
            <p className="text-sm panel-muted">
              备份会先流式上传并执行 SQLite 完整性与 Flux schema
              校验，成功后控制面会优雅重启。
            </p>
          </CardHeader>
          <Divider />
          <CardBody className="px-5 py-5 space-y-4">
            <div className="rounded-xl bg-danger-50 dark:bg-danger-500/10 px-4 py-3 text-sm text-danger-700 dark:text-danger-300">
              恢复会替换当前数据库及其 WAL/SHM 文件。请确认备份来自可信的 Flux
              Panel 实例，且已准备好重新登录。
            </div>
            <input
              ref={fileInputRef}
              type="file"
              accept=".db,application/vnd.sqlite3,application/octet-stream"
              className="hidden"
              onChange={handleFileChange}
            />
            <div className="flex flex-wrap items-center gap-3">
              <Button
                color="danger"
                variant="flat"
                onPress={() => fileInputRef.current?.click()}
                isDisabled={downloadLoading !== null || restoreLoading}
              >
                选择备份文件
              </Button>
              {selectedFile && (
                <span className="text-sm text-default-600">
                  {selectedFile.name} · {formatBytes(selectedFile.size)}
                </span>
              )}
            </div>
          </CardBody>
        </Card>
      </div>

      <Modal isOpen={isOpen} onOpenChange={onOpenChange} backdrop="blur">
        <ModalContent>
          <ModalHeader>确认恢复数据库</ModalHeader>
          <ModalBody>
            <div className="space-y-3">
              <p>
                即将使用 <strong>{selectedFile?.name}</strong> 替换当前数据库。
              </p>
              <p className="text-sm text-danger-600 dark:text-danger-400">
                此操作不可撤销。控制面会在上传并校验成功后重启，当前会话将失效。
              </p>
            </div>
          </ModalBody>
          <ModalFooter>
            <Button
              variant="light"
              onPress={onOpenChange}
              isDisabled={restoreLoading}
            >
              取消
            </Button>
            <Button
              color="danger"
              onPress={handleRestore}
              isLoading={restoreLoading}
            >
              确认恢复并重启
            </Button>
          </ModalFooter>
        </ModalContent>
      </Modal>

      {downloadLoading && (
        <div className="fixed bottom-6 right-6 z-20">
          <Card className="panel-shell">
            <CardBody className="flex flex-row items-center gap-2 px-4 py-3">
              <Spinner size="sm" />
              <span className="text-sm">正在生成文件…</span>
            </CardBody>
          </Card>
        </div>
      )}
    </div>
  );
}
