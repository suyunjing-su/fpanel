package com.admin.common.task;

import com.admin.common.utils.GostUtil;
import com.admin.entity.ChainTunnel;
import com.admin.entity.Node;
import com.admin.service.ChainTunnelService;
import com.admin.service.NodeService;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import lombok.extern.slf4j.Slf4j;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

import javax.annotation.Resource;
import java.util.Comparator;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;
import java.util.stream.Collectors;

@Slf4j
@Component
public class ExitBandwidthCheckAsync {

    private static final int EXIT_CHAIN_TYPE = 3;
    private static final double BANDWIDTH_USAGE_THRESHOLD = 0.80D;
    private static final double MIN_EXTERNAL_USAGE_MBPS = 0.10D;
    private static final long MIN_SAMPLING_INTERVAL_MS = 1000L;

    private final ConcurrentHashMap<Long, FlowSnapshot> flowSnapshotCache = new ConcurrentHashMap<>();

    @Resource
    private ChainTunnelService chainTunnelService;

    @Resource
    private NodeService nodeService;

    @Scheduled(fixedDelay = 15000, initialDelay = 30000)
    public void checkExitBandwidth() {
        List<ChainTunnel> chainTunnels = chainTunnelService.list(
                new QueryWrapper<ChainTunnel>().isNotNull("node_id").orderByAsc("id")
        );
        if (chainTunnels.isEmpty()) {
            flowSnapshotCache.clear();
            return;
        }

        long now = System.currentTimeMillis();
        Set<Long> activeIds = new HashSet<>();
        Map<Long, Double> chainTunnelMbps = new HashMap<>();
        Map<Long, Double> nodeTotalMbps = new HashMap<>();
        Set<Long> rateReadyNodeIds = new HashSet<>();

        for (ChainTunnel chainTunnel : chainTunnels) {
            if (chainTunnel.getId() == null || chainTunnel.getNodeId() == null) {
                continue;
            }
            activeIds.add(chainTunnel.getId());

            long totalBytes = safeLong(chainTunnel.getInFlow()) + safeLong(chainTunnel.getOutFlow());
            FlowSnapshot previous = flowSnapshotCache.put(chainTunnel.getId(), new FlowSnapshot(totalBytes, now));
            if (previous == null) {
                continue;
            }

            long durationMs = now - previous.timestampMs;
            if (durationMs < MIN_SAMPLING_INTERVAL_MS) {
                continue;
            }

            long deltaBytes = totalBytes - previous.totalBytes;
            if (deltaBytes < 0) {
                deltaBytes = 0;
            }

            double mbps = bytesToMbps(deltaBytes, durationMs);
            chainTunnelMbps.put(chainTunnel.getId(), mbps);
            nodeTotalMbps.merge(chainTunnel.getNodeId(), mbps, Double::sum);
            rateReadyNodeIds.add(chainTunnel.getNodeId());
        }

        flowSnapshotCache.keySet().removeIf(id -> !activeIds.contains(id));

        List<ChainTunnel> exits = chainTunnels.stream()
                .filter(item -> Objects.equals(item.getChainType(), EXIT_CHAIN_TYPE))
                .filter(item -> item.getTunnelId() != null && item.getNodeId() != null && item.getId() != null)
                .sorted(Comparator.comparing(ChainTunnel::getTunnelId).thenComparing(ChainTunnel::getId))
                .toList();
        if (exits.isEmpty()) {
            return;
        }

        Map<Long, Node> nodeMap = loadNodeMap(exits);
        Map<Long, List<ChainTunnel>> exitsByTunnel = exits.stream()
                .collect(Collectors.groupingBy(ChainTunnel::getTunnelId, LinkedHashMap::new, Collectors.toList()));

        Set<Long> changedTunnelIds = new HashSet<>();
        for (Map.Entry<Long, List<ChainTunnel>> entry : exitsByTunnel.entrySet()) {
            Long tunnelId = entry.getKey();
            List<ChainTunnel> tunnelExits = entry.getValue();
            if (tunnelId == null || tunnelExits == null || tunnelExits.isEmpty()) {
                continue;
            }

            boolean primaryBackup = isPrimaryBackupTunnel(tunnelExits);
            if (!primaryBackup) {
                for (ChainTunnel exit : tunnelExits) {
                    if (updateBandwidthOverloaded(exit, 0)) {
                        changedTunnelIds.add(tunnelId);
                    }
                }
                continue;
            }

            for (ChainTunnel exit : tunnelExits) {
                if (!rateReadyNodeIds.contains(exit.getNodeId())) {
                    continue;
                }

                Node node = nodeMap.get(exit.getNodeId());
                double nodeUsageMbps = nodeTotalMbps.getOrDefault(exit.getNodeId(), 0D);
                double currentTunnelUsageMbps = chainTunnelMbps.getOrDefault(exit.getId(), 0D);

                int overloaded = shouldMarkOverloaded(node, nodeUsageMbps, currentTunnelUsageMbps) ? 1 : 0;
                if (updateBandwidthOverloaded(exit, overloaded)) {
                    changedTunnelIds.add(tunnelId);
                }
            }
        }

        if (!changedTunnelIds.isEmpty()) {
            forcePullTunnelEntryConfigs(changedTunnelIds);
        }
    }

    private Map<Long, Node> loadNodeMap(List<ChainTunnel> exits) {
        Set<Long> nodeIds = exits.stream()
                .map(ChainTunnel::getNodeId)
                .filter(Objects::nonNull)
                .collect(Collectors.toCollection(LinkedHashSet::new));
        if (nodeIds.isEmpty()) {
            return Map.of();
        }

        List<Node> nodes = nodeService.list(new QueryWrapper<Node>().in("id", nodeIds));
        return nodes.stream().collect(Collectors.toMap(Node::getId, item -> item, (left, right) -> left));
    }

    private boolean isPrimaryBackupTunnel(List<ChainTunnel> exits) {
        if (exits == null || exits.size() <= 1) {
            return false;
        }
        String strategy = exits.get(0).getStrategy();
        return "fifo".equalsIgnoreCase(strategy == null ? "" : strategy.trim());
    }

    private boolean shouldMarkOverloaded(Node node, double nodeUsageMbps, double currentTunnelUsageMbps) {
        if (node == null || node.getMaxBandwidthMbps() == null || node.getMaxBandwidthMbps() <= 0) {
            return false;
        }

        double usageRatio = nodeUsageMbps / node.getMaxBandwidthMbps();
        double externalUsageMbps = Math.max(0D, nodeUsageMbps - currentTunnelUsageMbps);
        return usageRatio >= BANDWIDTH_USAGE_THRESHOLD && externalUsageMbps > MIN_EXTERNAL_USAGE_MBPS;
    }

    private boolean updateBandwidthOverloaded(ChainTunnel exit, int nextValue) {
        int currentValue = exit.getBandwidthOverloaded() == null ? 0 : exit.getBandwidthOverloaded();
        if (currentValue == nextValue || exit.getId() == null) {
            return false;
        }

        UpdateWrapper<ChainTunnel> updateWrapper = new UpdateWrapper<>();
        updateWrapper.eq("id", exit.getId())
                .set("bandwidth_overloaded", nextValue);
        boolean updated = chainTunnelService.update(null, updateWrapper);
        if (updated) {
            exit.setBandwidthOverloaded(nextValue);
        }
        return updated;
    }

    private void forcePullTunnelEntryConfigs(Set<Long> tunnelIds) {
        Set<Long> entryNodeIds = new LinkedHashSet<>();
        List<ChainTunnel> entryNodes = chainTunnelService.list(new QueryWrapper<ChainTunnel>()
                .in("tunnel_id", tunnelIds)
                .eq("chain_type", 1));

        for (ChainTunnel entry : entryNodes) {
            if (entry.getNodeId() != null) {
                entryNodeIds.add(entry.getNodeId());
            }
        }

        for (Long nodeId : entryNodeIds) {
            try {
                GostUtil.ForcePullFullConfig(nodeId);
            } catch (Exception ex) {
                log.warn("force pull entry config failed for node {}", nodeId, ex);
            }
        }
    }

    private double bytesToMbps(long bytes, long durationMs) {
        if (durationMs <= 0) {
            return 0D;
        }
        return (bytes * 8D * 1000D) / (durationMs * 1_000_000D);
    }

    private long safeLong(Long value) {
        return value == null ? 0L : value;
    }

    private record FlowSnapshot(long totalBytes, long timestampMs) {
    }
}
