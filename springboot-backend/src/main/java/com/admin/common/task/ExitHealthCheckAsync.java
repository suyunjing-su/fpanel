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
import java.util.HashSet;
import java.util.List;
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

        long now = System.currentTimeMillis();
        Set<Long> changedTunnelIds = new HashSet<>();

        for (ChainTunnel exit : exits) {
            if (exit.getId() == null || exit.getTunnelId() == null || exit.getNodeId() == null) {
                continue;
            }

            HealthResult healthResult = evaluateExit(exit.getNodeId());
            Integer nextStatus = healthResult.healthy ? 1 : 0;

            boolean changed = !Objects.equals(exit.getHealthStatus(), nextStatus)
                    || !Objects.equals(exit.getLastLatencyMs(), healthResult.latencyMs);
            if (changed) {
                changedTunnelIds.add(exit.getTunnelId());
            }

            UpdateWrapper<ChainTunnel> updateWrapper = new UpdateWrapper<>();
            updateWrapper.eq("id", exit.getId())
                    .set("health_status", nextStatus)
                    .set("last_latency_ms", healthResult.latencyMs)
                    .set("health_checked_time", now);
            chainTunnelService.update(null, updateWrapper);
        }

        if (!changedTunnelIds.isEmpty()) {
            forcePullTunnelConfigs(changedTunnelIds);
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

    private void forcePullTunnelConfigs(Set<Long> tunnelIds) {
        Set<Long> affectedNodeIds = new HashSet<>();
        List<ChainTunnel> tunnelNodes = chainTunnelService.list(new QueryWrapper<ChainTunnel>().in("tunnel_id", tunnelIds));
        for (ChainTunnel chainTunnel : tunnelNodes) {
            if (chainTunnel.getNodeId() != null) {
                affectedNodeIds.add(chainTunnel.getNodeId());
            }
        }

        for (Long nodeId : affectedNodeIds) {
            try {
                GostUtil.ForcePullFullConfig(nodeId);
            } catch (Exception ex) {
                log.warn("force pull config failed for node {}", nodeId, ex);
            }
        }
    }

    private record HealthResult(boolean healthy, Long latencyMs) {
    }
}
