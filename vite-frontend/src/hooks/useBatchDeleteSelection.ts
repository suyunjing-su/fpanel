import { useEffect, useMemo, useState } from 'react';
import toast from 'react-hot-toast';
import type { BatchDeleteFailure, BatchDeleteResult } from '@/api';

interface BatchDeleteApiResponse {
  code: number;
  msg?: string;
  data?: BatchDeleteResult;
}

interface UseBatchDeleteSelectionOptions<T extends { id: number }> {
  items: T[];
  entityLabel: string;
  batchDeleteApi: (ids: number[]) => Promise<BatchDeleteApiResponse>;
  reloadData: () => Promise<void>;
}

export function useBatchDeleteSelection<T extends { id: number }>(options: UseBatchDeleteSelectionOptions<T>) {
  const { items, entityLabel, batchDeleteApi, reloadData } = options;

  const [selectedIds, setSelectedIds] = useState<number[]>([]);
  const [modalOpen, setModalOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [failures, setFailures] = useState<BatchDeleteFailure[]>([]);
  const [lastResult, setLastResult] = useState<{ successCount: number; failureCount: number } | null>(null);

  useEffect(() => {
    const itemIdSet = new Set(items.map(item => item.id));
    setSelectedIds(prev => prev.filter(id => itemIdSet.has(id)));
  }, [items]);

  const selectedCount = selectedIds.length;

  const isAllSelected = useMemo(() => {
    return items.length > 0 && selectedIds.length === items.length;
  }, [items.length, selectedIds.length]);

  const toggleItemSelection = (id: number) => {
    setSelectedIds(prev => (
      prev.includes(id) ? prev.filter(itemId => itemId !== id) : [...prev, id]
    ));
  };

  const toggleSelectAll = () => {
    if (isAllSelected) {
      setSelectedIds([]);
      return;
    }
    setSelectedIds(items.map(item => item.id));
  };

  const clearSelection = () => {
    setSelectedIds([]);
  };

  const openBatchDeleteModal = () => {
    if (selectedCount === 0) {
      toast.error(`请先选择要删除的${entityLabel}`);
      return;
    }
    setFailures([]);
    setLastResult(null);
    setModalOpen(true);
  };

  const executeBatchDelete = async (targetIds: number[]) => {
    if (targetIds.length === 0) {
      return;
    }

    setDeleting(true);
    try {
      const response = await batchDeleteApi(targetIds);
      if (response.code !== 0) {
        toast.error(response.msg || '批量删除失败');
        return;
      }

      const result: BatchDeleteResult = response.data || {
        totalCount: targetIds.length,
        successCount: targetIds.length,
        failureCount: 0,
        failures: []
      };

      const nextFailures = result.failures || [];
      setFailures(nextFailures);
      setLastResult({
        successCount: result.successCount || 0,
        failureCount: result.failureCount || nextFailures.length
      });

      if ((result.successCount || 0) > 0) {
        toast.success(`成功删除 ${result.successCount} 个${entityLabel}`);
      }
      if ((result.failureCount || 0) > 0) {
        toast.error(`有 ${result.failureCount} 个${entityLabel}删除失败`);
      }

      await reloadData();

      if ((result.failureCount || 0) === 0) {
        setSelectedIds([]);
        setModalOpen(false);
        return;
      }

      setSelectedIds(nextFailures.map(item => item.id));
    } catch (error) {
      toast.error('批量删除失败');
    } finally {
      setDeleting(false);
    }
  };

  const confirmBatchDelete = async () => {
    await executeBatchDelete(selectedIds);
  };

  const retryFailedDeletes = async () => {
    if (failures.length === 0) {
      return;
    }
    await executeBatchDelete(failures.map(item => item.id));
  };

  const closeModal = () => {
    setModalOpen(false);
    setFailures([]);
    setLastResult(null);
  };

  return {
    selectedIds,
    selectedCount,
    isAllSelected,
    modalOpen,
    deleting,
    failures,
    lastResult,
    toggleItemSelection,
    toggleSelectAll,
    clearSelection,
    openBatchDeleteModal,
    confirmBatchDelete,
    retryFailedDeletes,
    closeModal,
    setModalOpen
  };
}
