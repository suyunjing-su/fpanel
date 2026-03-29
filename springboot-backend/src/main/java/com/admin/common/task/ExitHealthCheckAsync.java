package com.admin.common.task;

import com.admin.common.dto.GostDto;
import com.admin.common.utils.GostUtil;
import com.admin.common.utils.WebSocketServer;
import com.admin.entity.ChainTunnel;
import com.admin.entity.Node;
import com.admin.service.ChainTunnelService;
import com.admin.service.NodeService;
import com.alibaba.fastjson.JSONObject;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import lombok.extern.slf4j.Slf4j;
import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

import javax.annotation.Resource;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;

@Slf4j
@Component
public class ExitHealthCheckAsync {

    private static final int EXIT_CHAIN_TYPE = 3;
    private static final long MAX_ALLOWED_LATENCY_MS = 20L;
    private static final String HEALTH_CHECK_TARGET = "www.google.com";
    private static final int HEALTH_CHECK_PORT = 443;
    private static final int HEALTH_CHECK_COUNT = 1;
    private static final int HEALTH_CHECK_TIMEOUT_MS = 2000;

    @Resource
    private ChainTunnelService chainTunnelService;

    @Resource
    private NodeService nodeService;

    @Scheduled(fixedDelay = 60000, initialDelay = 15000)
    public void checkExitHealth() {
        List<ChainTunnel> exits = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("chain_type", EXIT_CHAIN_TYPE));
        if (exits.isEmpty()) {
            return;
        }

        Map<Long, List<ChainTunnel>> exitsByTunnel = new HashMap<>();
        for (ChainTunnel exit : exits) {
            if (exit.getTunnelId() == null) {
                continue;
            }
            exitsByTunnel.computeIfAbsent(exit.getTunnelId(), k -> new ArrayList<>()).add(exit);
        }

        long now = System.currentTimeMillis();
        Set<Long> changedTunnelIds = new HashSet<>();

        for (Map.Entry<Long, List<ChainTunnel>> tunnelEntry : exitsByTunnel.entrySet()) {
            List<ChainTunnel> tunnelExits = tunnelEntry.getValue();
            if (tunnelExits == null || tunnelExits.size() <= 1) {
                continue;
            }

            for (ChainTunnel exit : tunnelExits) {
                if (exit.getId() == null || exit.getNodeId() == null) {
                    continue;
                }

                boolean beforeSelectable = isSelectableExit(exit.getHealthStatus(), exit.getLastLatencyMs());

                HealthResult healthResult = evaluateExit(exit.getNodeId());
                Integer nextStatus = healthResult.healthy ? 1 : 0;
                Long nextLatency = healthResult.latencyMs;
                boolean afterSelectable = isSelectableExit(nextStatus, nextLatency);
                if (beforeSelectable != afterSelectable) {
                    changedTunnelIds.add(tunnelEntry.getKey());
                }

                UpdateWrapper<ChainTunnel> updateWrapper = new UpdateWrapper<>();
                updateWrapper.eq("id", exit.getId())
                        .set("health_status", nextStatus)
                        .set("last_latency_ms", nextLatency)
                        .set("health_checked_time", now);
                chainTunnelService.update(null, updateWrapper);
            }
        }

        if (!changedTunnelIds.isEmpty()) {
            forcePullTunnelEntryConfigs(changedTunnelIds);
        }
    }

    private HealthResult evaluateExit(Long nodeId) {
        Node node = nodeService.getById(nodeId);
        if (node == null || node.getStatus() == null || node.getStatus() != 1) {
            return new HealthResult(false, null);
        }

        try {
            JSONObject payload = new JSONObject();
            payload.put("ip", HEALTH_CHECK_TARGET);
            payload.put("port", HEALTH_CHECK_PORT);
            payload.put("count", HEALTH_CHECK_COUNT);
            payload.put("timeout", HEALTH_CHECK_TIMEOUT_MS);

            GostDto response = WebSocketServer.send_msg(nodeId, payload, "TcpPing");
            if (response == null || !"OK".equals(response.getMsg()) || !(response.getData() instanceof JSONObject data)) {
                return new HealthResult(false, null);
            }

            boolean success = data.getBooleanValue("success");
            if (!success) {
                return new HealthResult(false, null);
            }

            long latency = Math.round(data.getDoubleValue("averageTime"));
            if (latency > MAX_ALLOWED_LATENCY_MS) {
                return new HealthResult(false, latency);
            }

            return new HealthResult(true, latency);
        } catch (Exception ex) {
            log.warn("exit health check failed for node {}", nodeId, ex);
            return new HealthResult(false, null);
        }
    }

    private boolean isSelectableExit(Integer healthStatus, Long latencyMs) {
        if (!Objects.equals(healthStatus, 1)) {
            return false;
        }
        return latencyMs == null || latencyMs <= MAX_ALLOWED_LATENCY_MS;
    }

    private void forcePullTunnelEntryConfigs(Set<Long> tunnelIds) {
        if (tunnelIds == null || tunnelIds.isEmpty()) {
            return;
        }

        Set<Long> affectedNodeIds = new LinkedHashSet<>();
        List<ChainTunnel> entryNodes = chainTunnelService.list(new QueryWrapper<ChainTunnel>()
                .in("tunnel_id", tunnelIds)
                .eq("chain_type", 1));
        for (ChainTunnel chainTunnel : entryNodes) {
            if (chainTunnel.getNodeId() != null) {
                affectedNodeIds.add(chainTunnel.getNodeId());
            }
        }

        if (affectedNodeIds.isEmpty()) {
            return;
        }

        for (Long nodeId : affectedNodeIds) {
            try {
                GostUtil.ForcePullFullConfig(nodeId);
            } catch (Exception ex) {
                log.warn("force pull entry config failed for node {}", nodeId, ex);
            }
        }
    }

    private record HealthResult(boolean healthy, Long latencyMs) {
    }
}
