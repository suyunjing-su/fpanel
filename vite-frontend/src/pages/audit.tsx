import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Button } from "@heroui/button";
import { Card, CardBody } from "@heroui/card";
import { Chip } from "@heroui/chip";
import { Spinner } from "@heroui/spinner";
import {
  Table,
  TableBody,
  TableCell,
  TableColumn,
  TableHeader,
  TableRow,
} from "@heroui/table";
import toast from "react-hot-toast";

import { getAuditLogs } from "@/api";
import type { AuditEvent } from "@/types";
import { isAdmin } from "@/utils/auth";

const PAGE_SIZE = 50;

const actionLabels: Record<string, string> = {
  post: "提交",
  put: "更新",
  patch: "更新",
  delete: "删除",
};

const formatTime = (timestamp: number) =>
  new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    hour12: false,
  }).format(new Date(timestamp));

export default function AuditPage() {
  const navigate = useNavigate();
  const [items, setItems] = useState<AuditEvent[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const pageCount = useMemo(
    () => Math.max(1, Math.ceil(total / PAGE_SIZE)),
    [total],
  );

  const loadEvents = useCallback(async () => {
    setLoading(true);
    try {
      const response = await getAuditLogs({ page, pageSize: PAGE_SIZE });
      if (response.code !== 0 || !response.data) {
        toast.error(response.msg || "审计日志加载失败");
        return;
      }
      setItems(response.data.items);
      setTotal(response.data.total);
    } catch (error) {
      console.error("审计日志加载失败:", error);
      toast.error("审计日志加载失败");
    } finally {
      setLoading(false);
    }
  }, [page]);

  useEffect(() => {
    if (!isAdmin()) {
      toast.error("权限不足，只有管理员可以访问此页面");
      navigate("/dashboard", { replace: true });
      return;
    }
    void loadEvents();
  }, [loadEvents, navigate]);

  return (
    <div className="px-3 lg:px-6 py-4 lg:py-6 flex flex-col h-full min-w-0 overflow-hidden gap-4">
      <Card className="panel-shell overflow-hidden">
        <CardBody className="p-4 lg:p-5">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
            <div className="min-w-0">
              <p className="text-xs uppercase tracking-[0.14em] panel-muted">
                Control Plane Audit
              </p>
              <h1 className="text-xl lg:text-2xl font-semibold mt-1">
                审计日志
              </h1>
              <p className="text-sm panel-muted mt-1 break-words">
                追踪控制面变更结果、操作者与请求来源
                <span className="hidden sm:inline">，不保存请求正文</span>
              </p>
            </div>
            <div className="flex items-center gap-3">
              <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 bg-white/70 dark:bg-slate-900/60 px-3 py-2 min-w-24">
                <p className="text-xs panel-muted">记录总数</p>
                <p className="text-lg font-semibold mt-0.5">{total}</p>
              </div>
              <Button
                color="primary"
                variant="flat"
                onPress={() => void loadEvents()}
                isLoading={loading}
              >
                刷新
              </Button>
            </div>
          </div>
        </CardBody>
      </Card>

      <Card className="panel-shell flex-1 min-h-0 hidden md:block">
        <CardBody className="p-3 overflow-hidden">
          <Table
            aria-label="控制面审计日志"
            removeWrapper
            classNames={{
              base: "h-full overflow-auto",
              th: "bg-slate-50 dark:bg-slate-900 text-slate-600 dark:text-slate-300 whitespace-nowrap",
              td: "py-3 align-top",
            }}
          >
            <TableHeader>
              <TableColumn>时间</TableColumn>
              <TableColumn>操作者</TableColumn>
              <TableColumn>动作与资源</TableColumn>
              <TableColumn>结果</TableColumn>
              <TableColumn>请求 ID</TableColumn>
              <TableColumn>来源</TableColumn>
              <TableColumn>摘要</TableColumn>
            </TableHeader>
            <TableBody
              items={items}
              isLoading={loading}
              loadingContent={<Spinner label="正在加载审计日志" />}
              emptyContent="暂无变更审计记录"
            >
              {(event) => (
                <TableRow key={event.id}>
                  <TableCell>
                    <span className="text-sm whitespace-nowrap">
                      {formatTime(event.createdAt)}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-sm">
                      {event.actorId ?? "系统/未知"}
                    </span>
                  </TableCell>
                  <TableCell>
                    <div className="flex flex-col gap-1 min-w-40">
                      <span className="text-sm font-medium">
                        {event.resourceType}
                      </span>
                      <span className="text-xs panel-muted uppercase">
                        {actionLabels[event.action] || event.action}
                      </span>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Chip
                      size="sm"
                      variant="flat"
                      color={event.outcome === "success" ? "success" : "danger"}
                    >
                      {event.outcome === "success" ? "成功" : "失败"}
                    </Chip>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs break-all max-w-52 inline-block">
                      {event.requestId || "-"}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs whitespace-nowrap">
                      {event.remoteAddr || "-"}
                    </span>
                  </TableCell>
                  <TableCell>
                    <span className="font-mono text-xs whitespace-nowrap">
                      {event.detail || "-"}
                    </span>
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </CardBody>
      </Card>

      <div className="md:hidden flex-1 min-h-0 overflow-y-auto space-y-3">
        {loading ? (
          <div className="h-40 flex items-center justify-center">
            <Spinner label="正在加载审计日志" />
          </div>
        ) : items.length === 0 ? (
          <Card className="panel-shell">
            <CardBody className="h-40 flex items-center justify-center panel-muted">
              暂无变更审计记录
            </CardBody>
          </Card>
        ) : (
          items.map((event) => (
            <Card key={event.id} className="panel-shell">
              <CardBody className="p-4 gap-3">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <p className="font-medium break-all">
                      {event.resourceType}
                    </p>
                    <p className="text-xs panel-muted mt-1">
                      {formatTime(event.createdAt)} ·{" "}
                      {actionLabels[event.action] || event.action}
                    </p>
                  </div>
                  <Chip
                    size="sm"
                    variant="flat"
                    color={event.outcome === "success" ? "success" : "danger"}
                  >
                    {event.outcome === "success" ? "成功" : "失败"}
                  </Chip>
                </div>
                <div className="grid grid-cols-[4.5rem_minmax(0,1fr)] gap-x-2 gap-y-1.5 text-xs">
                  <span className="panel-muted">操作者</span>
                  <span className="font-mono break-all">
                    {event.actorId ?? "系统/未知"}
                  </span>
                  <span className="panel-muted">请求 ID</span>
                  <span className="font-mono break-all">
                    {event.requestId || "-"}
                  </span>
                  <span className="panel-muted">来源</span>
                  <span className="font-mono break-all">
                    {event.remoteAddr || "-"}
                  </span>
                  <span className="panel-muted">摘要</span>
                  <span className="font-mono break-all">
                    {event.detail || "-"}
                  </span>
                </div>
              </CardBody>
            </Card>
          ))
        )}
      </div>

      <div className="flex items-center justify-between gap-3 px-1">
        <p className="text-xs panel-muted shrink-0">
          第 {page} / {pageCount} 页
          <span className="hidden sm:inline">，每页 {PAGE_SIZE} 条</span>
        </p>
        <div className="flex gap-2 shrink-0">
          <Button
            size="sm"
            variant="flat"
            className="hidden sm:inline-flex"
            isDisabled={page <= 1 || loading}
            onPress={() => setPage((value) => value - 1)}
          >
            上一页
          </Button>
          <Button
            size="sm"
            variant="flat"
            className={page < pageCount ? undefined : "hidden sm:inline-flex"}
            isDisabled={page >= pageCount || loading}
            onPress={() => setPage((value) => value + 1)}
          >
            下一页
          </Button>
        </div>
      </div>
    </div>
  );
}
