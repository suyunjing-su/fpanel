package com.admin.controller;

import com.admin.common.aop.LogAnnotation;
import com.admin.common.dto.FlowDto;
import com.admin.common.dto.GostConfigDto;
import com.admin.common.task.CheckGostConfigAsync;
import com.admin.common.utils.AESCrypto;
import com.admin.common.utils.GostUtil;
import com.admin.entity.*;
import com.admin.service.ChainTunnelService;
import com.admin.service.ForwardPortService;
import com.admin.service.SpeedLimitService;
import com.admin.service.UserTunnelEntryPolicyService;
import com.admin.service.UserTunnelExitPolicyService;
import com.alibaba.fastjson.JSON;
import com.alibaba.fastjson.JSONArray;
import com.alibaba.fastjson.JSONObject;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import org.springframework.context.annotation.Lazy;
import org.springframework.util.StringUtils;
import org.springframework.web.bind.annotation.*;
import lombok.extern.slf4j.Slf4j;

import javax.annotation.Resource;
import javax.servlet.http.HttpServletRequest;
import java.math.BigDecimal;
import java.net.InetAddress;
import java.util.Date;
import java.util.HashMap;
import java.util.HashSet;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

/**
 * 流量上报控制器
 * 处理节点上报的流量数据，更新用户和隧道的流量统计
 * <p>
 * 主要功能：
 * 1. 接收并处理节点上报的流量数据
 * 2. 更新转发、用户和隧道的流量统计
 * 3. 检查用户总流量限制，超限时暂停所有服务
 * 4. 检查隧道流量限制，超限时暂停对应服务
 * 5. 检查用户到期时间，到期时暂停所有服务
 * 6. 检查隧道权限到期时间，到期时暂停对应服务
 * 7. 检查用户状态，状态不为1时暂停所有服务
 * 8. 检查转发状态，状态不为1时暂停对应转发
 * 9. 检查用户隧道权限状态，状态不为1时暂停对应转发
 * <p>
 * 并发安全解决方案：
 * 1. 使用UpdateWrapper进行数据库层面的原子更新操作，避免读取-修改-写入的竞态条件
 * 2. 使用synchronized锁确保同一用户/隧道的流量更新串行执行
 * 3. 这样可以避免相同用户相同隧道不同转发同时上报时流量统计丢失的问题
 */
@RestController
@RequestMapping("/flow")
@CrossOrigin
@Slf4j
public class FlowController extends BaseController {

    // 常量定义
    private static final String SUCCESS_RESPONSE = "ok";
    private static final String FORBIDDEN_RESPONSE = "forbidden";
    private static final String DEFAULT_USER_TUNNEL_ID = "0";
    private static final long BYTES_TO_GB = 1024L * 1024L * 1024L;
    private static final long EXIT_MAX_LATENCY_MS = 20L;
    private static final long CONFIG_REFRESH_THROTTLE_MS = 30_000L;

    private enum TunnelConfigRefreshScope {
        TUNNEL_NODES,
        ENTRY_NODES,
        ENTRY_NODE_TARGET
    }

    // 用于同步相同用户和隧道的流量更新操作
    private static final ConcurrentHashMap<String, Object> USER_LOCKS = new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<String, Object> TUNNEL_LOCKS = new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<String, Object> FORWARD_LOCKS = new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<Long, Long> TUNNEL_CONFIG_REFRESH_AT = new ConcurrentHashMap<>();

    // 缓存加密器实例，避免重复创建
    private static final ConcurrentHashMap<String, AESCrypto> CRYPTO_CACHE = new ConcurrentHashMap<>();

    @Resource
    CheckGostConfigAsync checkGostConfigAsync;

    @Resource
    @Lazy
    ChainTunnelService chainTunnelService;

    @Resource
    ForwardPortService forwardPortService;

    @Resource
    SpeedLimitService speedLimitService;

    @Resource
    UserTunnelEntryPolicyService userTunnelEntryPolicyService;

    @Resource
    UserTunnelExitPolicyService userTunnelExitPolicyService;

    /**
     * 加密消息包装器
     */
    public static class EncryptedMessage {
        private boolean encrypted;
        private String data;
        private Long timestamp;

        // getters and setters
        public boolean isEncrypted() {
            return encrypted;
        }

        public void setEncrypted(boolean encrypted) {
            this.encrypted = encrypted;
        }

        public String getData() {
            return data;
        }

        public void setData(String data) {
            this.data = data;
        }

        public Long getTimestamp() {
            return timestamp;
        }

        public void setTimestamp(Long timestamp) {
            this.timestamp = timestamp;
        }
    }

    @PostMapping("/config")
    @LogAnnotation
    public String config(@RequestBody String rawData,
                         HttpServletRequest request) {
        if (!isSecureTransport(request)) {
            log.warn("拒绝非安全配置上报请求，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        String secret = resolveNodeSecret(request);
        if (!StringUtils.hasText(secret)) {
            log.warn("拒绝无鉴权配置上报请求，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        Node node = nodeService.getOne(new QueryWrapper<Node>().eq("secret", secret));
        if (node == null) return SUCCESS_RESPONSE;

        try {
            // 尝试解密数据
            String decryptedData = decryptIfNeeded(rawData, secret);

            // 解析为GostConfigDto
            GostConfigDto gostConfigDto = JSON.parseObject(decryptedData, GostConfigDto.class);
            checkGostConfigAsync.cleanNodeConfigs(node.getId().toString(), gostConfigDto);

            log.info("🔓 节点 {} 配置数据接收成功{}", node.getId(), isEncryptedMessage(rawData) ? "（已解密）" : "");

        } catch (Exception e) {
            log.error("处理节点 {} 配置数据失败: {}", node.getId(), e.getMessage());
        }

        return SUCCESS_RESPONSE;
    }

    @RequestMapping("/test")
    @LogAnnotation
    public String test() {
        return "test";
    }

    /**
     * 处理流量数据上报
     *
     * @param rawData 原始数据（可能是加密的）
     * @param secret  节点密钥
     * @return 处理结果
     */
    @RequestMapping("/upload")
    @LogAnnotation
    public String uploadFlowData(@RequestBody String rawData,
                                 HttpServletRequest request) {
        if (!isSecureTransport(request)) {
            log.warn("拒绝非安全流量上报请求，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        String secret = resolveNodeSecret(request);
        if (!StringUtils.hasText(secret)) {
            log.warn("拒绝无鉴权流量上报请求，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        // 1. 验证节点权限
        if (!isValidNode(secret)) {
            return SUCCESS_RESPONSE;
        }

        Node node = nodeService.getOne(new QueryWrapper<Node>().eq("secret", secret));
        if (node == null) {
            return SUCCESS_RESPONSE;
        }

        // 2. 尝试解密数据
        String decryptedData = decryptIfNeeded(rawData, secret);

        // 3. 解析为FlowDto列表
        JSONArray flowDataList = JSONObject.parseArray(decryptedData);
        log.info("节点上报流量数据{}", flowDataList);
        for (int i = 0; i < flowDataList.size(); i++) {
            String jsonObject = flowDataList.getJSONObject(i).toJSONString();
            FlowDto flowDto = JSONObject.parseObject(jsonObject, FlowDto.class);
            if (!Objects.equals(flowDto.getN(), "web_api")) {
                processFlowData(flowDto, node);
            }
        }
        return SUCCESS_RESPONSE;

    }

    @GetMapping("/config/all")
    @LogAnnotation
    public String getAllConfig(HttpServletRequest request) {
        if (!isSecureTransport(request)) {
            log.warn("拒绝非安全全量配置请求，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        String secret = resolveNodeSecret(request);
        if (!StringUtils.hasText(secret)) {
            log.warn("拒绝无鉴权全量配置请求，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        Node node = nodeService.getOne(new QueryWrapper<Node>().eq("secret", secret));
        if (node == null) {
            log.warn("全量配置请求鉴权失败，IP: {}", request.getRemoteAddr());
            return FORBIDDEN_RESPONSE;
        }

        JSONObject fullConfig = buildNodeFullConfig(node);
        return fullConfig.toJSONString();
    }

    /**
     * 检测消息是否为加密格式
     */
    private boolean isEncryptedMessage(String data) {
        try {
            JSONObject json = JSON.parseObject(data);
            return json.getBooleanValue("encrypted");
        } catch (Exception e) {
            return false;
        }
    }

    /**
     * 根据需要解密数据
     */
    private String decryptIfNeeded(String rawData, String secret) {
        if (rawData == null || rawData.trim().isEmpty()) {
            throw new IllegalArgumentException("数据不能为空");
        }

        try {
            // 尝试解析为加密消息格式
            EncryptedMessage encryptedMessage = JSON.parseObject(rawData, EncryptedMessage.class);

            if (encryptedMessage.isEncrypted() && encryptedMessage.getData() != null) {
                // 获取或创建加密器
                AESCrypto crypto = getOrCreateCrypto(secret);
                if (crypto == null) {
                    log.info("⚠️ 收到加密消息但无法创建解密器，使用原始数据");
                    return rawData;
                }

                // 解密数据
                String decryptedData = crypto.decryptString(encryptedMessage.getData());
                return decryptedData;
            }
        } catch (Exception e) {
            // 解析失败，可能是非加密格式，直接返回原始数据
            log.info("数据未加密或解密失败，使用原始数据: {}", e.getMessage());
        }

        return rawData;
    }

    /**
     * 获取或创建加密器实例
     */
    private AESCrypto getOrCreateCrypto(String secret) {
        return CRYPTO_CACHE.computeIfAbsent(secret, AESCrypto::create);
    }

    /**
     * 处理流量数据的核心逻辑
     */
    private void processFlowData(FlowDto flowDataList, Node reporterNode) {
        if (flowDataList == null || !StringUtils.hasText(flowDataList.getN())) {
            return;
        }

        String serviceName = flowDataList.getN();
        Long relayTunnelId = GostUtil.parseTunnelIdFromChainServiceName(serviceName);
        if (relayTunnelId != null) {
            processExitRelayFlow(relayTunnelId, reporterNode, flowDataList);
            return;
        }

        if (!isForwardServiceName(serviceName)) {
            return;
        }

        String[] serviceIds = parseServiceName(serviceName);
        if (serviceIds.length < 3) {
            return;
        }
        String forwardId = serviceIds[0];
        String userId = serviceIds[1];
        String userTunnelId = serviceIds[2];

        Forward forward = forwardService.getById(forwardId);
        if (forward != null){
            Tunnel tunnel = tunnelService.getById(forward.getTunnelId());
            if (tunnel == null) {
                return;
            }

            //  处理流量倍率及单双向计算
            BigDecimal trafficRatio = tunnel.getTrafficRatio();
            BigDecimal originalD = BigDecimal.valueOf(flowDataList.getD());
            BigDecimal originalU = BigDecimal.valueOf(flowDataList.getU());
            BigDecimal newD = originalD.multiply(trafficRatio);
            BigDecimal newU = originalU.multiply(trafficRatio);
            flowDataList.setD(newD.longValue() * tunnel.getFlow());
            flowDataList.setU(newU.longValue() * tunnel.getFlow());

            if (reporterNode != null) {
                updateChainNodeFlow(forward.getTunnelId().longValue(), reporterNode.getId(), 1, flowDataList);
                if (isChainNodeQuotaExceeded(forward.getTunnelId().longValue(), reporterNode.getId(), 1)) {
                    triggerTunnelEntryConfigRefresh(forward.getTunnelId().longValue(), reporterNode.getId());
                }
            }
        }

        // 先更新所有流量统计 - 确保流量数据的一致性
        updateForwardFlow(forwardId, flowDataList);
        updateUserFlow(userId, flowDataList);
        updateUserTunnelFlow(userTunnelId, flowDataList);
        if (!Objects.equals(userTunnelId, DEFAULT_USER_TUNNEL_ID) && forward != null && reporterNode != null) {
            updateUserEntryPolicyUsage(userTunnelId, forward.getTunnelId(), reporterNode.getId(), flowDataList);
        }
        if (!Objects.equals(userTunnelId, DEFAULT_USER_TUNNEL_ID) && forward != null) {
            updateUserExitPolicyUsage(userTunnelId, forward.getTunnelId(), flowDataList);
        }

        // 7. 检查和服务暂停操作
        String name = buildServiceName(forwardId, userId, userTunnelId);
        if (!Objects.equals(userTunnelId, DEFAULT_USER_TUNNEL_ID)) { // 非管理员的转发需要检测流量限制
            checkUserRelatedLimits(userId, name);
            checkUserTunnelRelatedLimits(userTunnelId, name, userId);
        }

    }

    private void processExitRelayFlow(Long tunnelId, Node reporterNode, FlowDto flowStats) {
        if (tunnelId == null || reporterNode == null || flowStats == null) {
            return;
        }
        updateChainNodeFlow(tunnelId, reporterNode.getId(), 3, flowStats);
        if (isChainNodeQuotaExceeded(tunnelId, reporterNode.getId(), 3)) {
            triggerTunnelEntryConfigRefresh(tunnelId);
        }
    }

    private void checkUserRelatedLimits(String userId, String name) {

        // 重新查询用户以获取最新的流量数据
        User updatedUser = userService.getById(userId);
        if (updatedUser == null) return;

        // 检查用户总流量限制
        long userFlowLimit = updatedUser.getFlow() * BYTES_TO_GB;
        long userCurrentFlow = updatedUser.getInFlow() + updatedUser.getOutFlow();
        if (userFlowLimit < userCurrentFlow) {
            pauseAllUserServices(userId, name);
            return;
        }

        // 检查用户到期时间
        if (updatedUser.getExpTime() != null && updatedUser.getExpTime() <= new Date().getTime()) {
            pauseAllUserServices(userId, name);
            return;
        }

        // 检查用户状态
        if (updatedUser.getStatus() != 1) {
            pauseAllUserServices(userId, name);
        }
    }

    public void pauseAllUserServices(String userId, String name) {
        List<Forward> forwardList = forwardService.list(new QueryWrapper<Forward>().eq("user_id", userId));
        pauseService(forwardList, name);
    }

    public void checkUserTunnelRelatedLimits(String userTunnelId, String name, String userId) {

        UserTunnel userTunnel = userTunnelService.getById(userTunnelId);
        if (userTunnel == null) return;
        long flow = userTunnel.getInFlow() + userTunnel.getOutFlow();
        if (flow >= userTunnel.getFlow() * BYTES_TO_GB) {
            pauseSpecificForward(userTunnel.getTunnelId(), name, userId);
            return;
        }

        if (userTunnel.getExpTime() != null && userTunnel.getExpTime() <= System.currentTimeMillis()) {
            pauseSpecificForward(userTunnel.getTunnelId(), name, userId);
            return;
        }

        if (userTunnel.getStatus() != 1) {
            pauseSpecificForward(userTunnel.getTunnelId(), name, userId);
        }


    }

    private void pauseSpecificForward(Integer tunnelId, String name, String userId) {
        List<Forward> forwardList = forwardService.list(new QueryWrapper<Forward>().eq("tunnel_id", tunnelId).eq("user_id", userId));
        pauseService(forwardList, name);
    }

    public void pauseService(List<Forward> forwardList, String name) {
        for (Forward forward : forwardList) {
            List<ChainTunnel> chainTunnels = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", forward.getTunnelId()).eq("chain_type", 1));
            for (ChainTunnel chainTunnel : chainTunnels) {
                GostUtil.PauseAndResumeService(chainTunnel.getNodeId(), name, "PauseService");
            }
            forward.setStatus(0);
            forwardService.updateById(forward);
        }
    }

    private void updateForwardFlow(String forwardId, FlowDto flowStats) {
        // 对相同转发的流量更新进行同步，避免并发覆盖
        synchronized (getForwardLock(forwardId)) {
            UpdateWrapper<Forward> updateWrapper = new UpdateWrapper<>();
            updateWrapper.eq("id", forwardId);
            updateWrapper.setSql("in_flow = in_flow + " + flowStats.getD() + ", out_flow = out_flow + " + flowStats.getU());

            forwardService.update(null, updateWrapper);
        }
    }

    private void updateUserFlow(String userId, FlowDto flowStats) {
        // 对相同用户的流量更新进行同步，避免并发覆盖
        synchronized (getUserLock(userId)) {
            UpdateWrapper<User> updateWrapper = new UpdateWrapper<>();
            updateWrapper.eq("id", userId);

            updateWrapper.setSql("in_flow = in_flow + " + flowStats.getD() + ", out_flow = out_flow + " + flowStats.getU());

            userService.update(null, updateWrapper);
        }
    }

    private void updateUserTunnelFlow(String userTunnelId, FlowDto flowStats) {
        if (Objects.equals(userTunnelId, DEFAULT_USER_TUNNEL_ID)) {
            return; // 默认隧道不需要更新，返回成功
        }

        // 对相同用户隧道的流量更新进行同步，避免并发覆盖
        synchronized (getTunnelLock(userTunnelId)) {
            UpdateWrapper<UserTunnel> updateWrapper = new UpdateWrapper<>();
            updateWrapper.eq("id", userTunnelId);
            updateWrapper.setSql("in_flow = in_flow + " + flowStats.getD() + ", out_flow = out_flow + " + flowStats.getU());
            userTunnelService.update(null, updateWrapper);
        }
    }

    private void updateChainNodeFlow(Long tunnelId, Long nodeId, Integer chainType, FlowDto flowStats) {
        if (tunnelId == null || nodeId == null || chainType == null || flowStats == null) {
            return;
        }
        UpdateWrapper<ChainTunnel> updateWrapper = new UpdateWrapper<>();
        updateWrapper.eq("tunnel_id", tunnelId)
                .eq("node_id", nodeId)
                .eq("chain_type", chainType)
                .setSql("in_flow = in_flow + " + flowStats.getD() + ", out_flow = out_flow + " + flowStats.getU());
        chainTunnelService.update(null, updateWrapper);
    }

    private boolean isChainNodeQuotaExceeded(Long tunnelId, Long nodeId, Integer chainType) {
        ChainTunnel chainTunnel = chainTunnelService.getOne(new QueryWrapper<ChainTunnel>()
                .eq("tunnel_id", tunnelId)
                .eq("node_id", nodeId)
                .eq("chain_type", chainType));
        if (chainTunnel == null || chainTunnel.getFlowQuotaGb() == null || chainTunnel.getFlowQuotaGb() <= 0) {
            return false;
        }
        long usedFlow = safeLong(chainTunnel.getInFlow()) + safeLong(chainTunnel.getOutFlow());
        return usedFlow >= chainTunnel.getFlowQuotaGb() * BYTES_TO_GB;
    }

    private void triggerTunnelConfigRefresh(Long tunnelId) {
        triggerTunnelConfigRefresh(tunnelId, TunnelConfigRefreshScope.TUNNEL_NODES, null);
    }

    private void triggerTunnelEntryConfigRefresh(Long tunnelId) {
        triggerTunnelConfigRefresh(tunnelId, TunnelConfigRefreshScope.ENTRY_NODES, null);
    }

    private void triggerTunnelEntryConfigRefresh(Long tunnelId, Long entryNodeId) {
        triggerTunnelConfigRefresh(tunnelId, TunnelConfigRefreshScope.ENTRY_NODE_TARGET, entryNodeId);
    }

    private void triggerTunnelConfigRefresh(Long tunnelId, TunnelConfigRefreshScope scope, Long targetNodeId) {
        if (tunnelId == null) {
            return;
        }
        long now = System.currentTimeMillis();
        Long lastRefresh = TUNNEL_CONFIG_REFRESH_AT.get(tunnelId);
        if (lastRefresh != null && now - lastRefresh < CONFIG_REFRESH_THROTTLE_MS) {
            return;
        }
        TUNNEL_CONFIG_REFRESH_AT.put(tunnelId, now);

        List<ChainTunnel> chainTunnels = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnelId));
        Set<Long> nodeIds = resolveRefreshNodeIds(chainTunnels, scope, targetNodeId);
        if (nodeIds.isEmpty() && !Objects.equals(scope, TunnelConfigRefreshScope.TUNNEL_NODES)) {
            nodeIds = resolveRefreshNodeIds(chainTunnels, TunnelConfigRefreshScope.TUNNEL_NODES, null);
        }
        for (Long nodeId : nodeIds) {
            try {
                GostUtil.ForcePullFullConfig(nodeId);
            } catch (Exception ex) {
                log.warn("force pull config failed for tunnel {} node {}", tunnelId, nodeId, ex);
            }
        }
    }

    private Set<Long> resolveRefreshNodeIds(List<ChainTunnel> chainTunnels,
                                            TunnelConfigRefreshScope scope,
                                            Long targetNodeId) {
        Set<Long> nodeIds = new LinkedHashSet<>();
        if (chainTunnels == null || chainTunnels.isEmpty()) {
            return nodeIds;
        }

        for (ChainTunnel chainTunnel : chainTunnels) {
            Long nodeId = chainTunnel.getNodeId();
            if (nodeId == null) {
                continue;
            }

            if (Objects.equals(scope, TunnelConfigRefreshScope.ENTRY_NODE_TARGET)) {
                if (Objects.equals(chainTunnel.getChainType(), 1) && Objects.equals(nodeId, targetNodeId)) {
                    nodeIds.add(nodeId);
                }
                continue;
            }

            if (Objects.equals(scope, TunnelConfigRefreshScope.ENTRY_NODES)) {
                if (Objects.equals(chainTunnel.getChainType(), 1)) {
                    nodeIds.add(nodeId);
                }
                continue;
            }

            nodeIds.add(nodeId);
        }

        return nodeIds;
    }

    private Object getUserLock(String userId) {
        return USER_LOCKS.computeIfAbsent(userId, k -> new Object());
    }

    private Object getTunnelLock(String userTunnelId) {
        return TUNNEL_LOCKS.computeIfAbsent(userTunnelId, k -> new Object());
    }

    private Object getForwardLock(String forwardId) {
        return FORWARD_LOCKS.computeIfAbsent(forwardId, k -> new Object());
    }

    private String resolveNodeSecret(HttpServletRequest request) {
        String fromHeader = extractBearerToken(request.getHeader("Authorization"));
        return StringUtils.hasText(fromHeader) ? fromHeader : null;
    }

    private String extractBearerToken(String authorization) {
        if (!StringUtils.hasText(authorization)) {
            return null;
        }
        String value = authorization.trim();
        if (value.regionMatches(true, 0, "Bearer ", 0, 7)) {
            value = value.substring(7).trim();
        }
        return StringUtils.hasText(value) ? value : null;
    }

    private boolean isSecureTransport(HttpServletRequest request) {
        return true;
    }

    private boolean isTrustedProxySource(HttpServletRequest request) {
        String remoteAddr = request.getRemoteAddr();
        if (!StringUtils.hasText(remoteAddr)) {
            return false;
        }
        try {
            InetAddress ip = InetAddress.getByName(remoteAddr);
            return ip.isLoopbackAddress() || ip.isSiteLocalAddress();
        } catch (Exception ignored) {
            return false;
        }
    }

    // Build full node config from dashboard DB state to avoid stale local gost.json on agent restart.
    private JSONObject buildNodeFullConfig(Node node) {
        JSONObject config = new JSONObject();
        JSONArray services = new JSONArray();
        JSONArray chains = new JSONArray();
        JSONArray limiters = new JSONArray();

        List<ChainTunnel> nodeChainTunnels = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("node_id", node.getId()));
        Set<Long> tunnelIds = new HashSet<>();
        for (ChainTunnel chainTunnel : nodeChainTunnels) {
            if (chainTunnel.getTunnelId() != null) {
                tunnelIds.add(chainTunnel.getTunnelId());
            }
        }

        List<ForwardPort> forwardPorts = forwardPortService.list(new QueryWrapper<ForwardPort>().eq("node_id", node.getId()));
        for (ForwardPort forwardPort : forwardPorts) {
            Forward forward = forwardService.getById(forwardPort.getForwardId());
            if (forward != null && forward.getTunnelId() != null) {
                tunnelIds.add(forward.getTunnelId().longValue());
            }
        }

        Map<Long, Tunnel> tunnelMap = new HashMap<>();
        if (!tunnelIds.isEmpty()) {
            List<Tunnel> tunnelList = tunnelService.list(new QueryWrapper<Tunnel>().in("id", tunnelIds));
            for (Tunnel tunnel : tunnelList) {
                tunnelMap.put(tunnel.getId(), tunnel);
            }
        }

        Set<Long> relatedNodeIds = new HashSet<>();
        if (!tunnelIds.isEmpty()) {
            List<ChainTunnel> relatedChainTunnels = chainTunnelService.list(new QueryWrapper<ChainTunnel>().in("tunnel_id", tunnelIds));
            for (ChainTunnel chainTunnel : relatedChainTunnels) {
                if (chainTunnel.getNodeId() != null) {
                    relatedNodeIds.add(chainTunnel.getNodeId());
                }
            }
        }
        relatedNodeIds.add(node.getId());

        Map<Long, Node> nodeMap = new HashMap<>();
        if (!relatedNodeIds.isEmpty()) {
            List<Node> relatedNodes = nodeService.list(new QueryWrapper<Node>().in("id", relatedNodeIds));
            for (Node relatedNode : relatedNodes) {
                nodeMap.put(relatedNode.getId(), relatedNode);
            }
        }

        Set<String> chainNames = new HashSet<>();
        for (ChainTunnel chainTunnel : nodeChainTunnels) {
            if (chainTunnel.getTunnelId() == null) {
                continue;
            }
            Tunnel tunnel = tunnelMap.get(chainTunnel.getTunnelId());
            if (tunnel == null || tunnel.getType() != 2) {
                continue;
            }
            if (!(Objects.equals(chainTunnel.getChainType(), 1) || Objects.equals(chainTunnel.getChainType(), 2))) {
                continue;
            }

            List<ChainTunnel> tunnelChainItems = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", chainTunnel.getTunnelId()));
            List<ChainTunnel> nextHops = getNextHops(chainTunnel, tunnelChainItems);
            nextHops = filterUnavailableExitHops(nextHops);
            if (nextHops.isEmpty()) {
                continue;
            }

            for (String trafficProtocol : resolveTrafficProtocols(nextHops)) {
                String chainName = GostUtil.buildChainName(chainTunnel.getTunnelId(), nextHops.getFirst().getProtocol(), trafficProtocol);
                if (chainNames.contains(chainName)) {
                    continue;
                }

                JSONObject chain = new JSONObject();
                chain.put("name", chainName);

                JSONObject hop = new JSONObject();
                hop.put("name", "hop_" + chainTunnel.getTunnelId());
                if (StringUtils.hasText(node.getInterfaceName())) {
                    hop.put("interface", node.getInterfaceName());
                }

                JSONObject selector = new JSONObject();
                selector.put("strategy", nextHops.getFirst().getStrategy());
                selector.put("maxFails", 1);
                selector.put("failTimeout", 600000000000L);
                hop.put("selector", selector);

                JSONArray nodes = new JSONArray();
                for (ChainTunnel nextHop : nextHops) {
                    Node nextNode = nodeMap.get(nextHop.getNodeId());
                    if (nextNode == null || nextHop.getPort() == null) {
                        continue;
                    }
                    JSONObject nodeItem = new JSONObject();
                    nodeItem.put("name", "node_" + (nextHop.getInx() == null ? 0 : nextHop.getInx()));
                    Integer listenPort = GostUtil.resolveChainListenPort(nextHop.getPort(), nextHop.getProtocol(), trafficProtocol);
                    nodeItem.put("addr", GostUtil.processServerAddress(nextNode.getServerIp() + ":" + listenPort));

                    JSONObject connector = new JSONObject();
                    connector.put("type", "relay");
                    nodeItem.put("connector", connector);

                    JSONObject dialer = GostUtil.createChainDialer(nextHop.getProtocol(), trafficProtocol);
                    nodeItem.put("dialer", dialer);
                    nodes.add(nodeItem);
                }

                if (nodes.isEmpty()) {
                    continue;
                }
                hop.put("nodes", nodes);
                JSONArray hops = new JSONArray();
                hops.add(hop);
                chain.put("hops", hops);
                chains.add(chain);
                chainNames.add(chainName);
            }
        }

        Set<String> serviceNames = new HashSet<>();
        for (ChainTunnel chainTunnel : nodeChainTunnels) {
            if (chainTunnel.getTunnelId() == null || chainTunnel.getPort() == null) {
                continue;
            }
            Tunnel tunnel = tunnelMap.get(chainTunnel.getTunnelId());
            if (tunnel == null || tunnel.getType() != 2) {
                continue;
            }
            if (!(Objects.equals(chainTunnel.getChainType(), 2) || Objects.equals(chainTunnel.getChainType(), 3))) {
                continue;
            }

            for (String trafficProtocol : resolveTrafficProtocols(List.of(chainTunnel))) {
                String serviceName = GostUtil.buildChainServiceName(
                        chainTunnel.getTunnelId(),
                        chainTunnel.getProtocol(),
                        trafficProtocol
                );
                if (serviceNames.contains(serviceName)) {
                    continue;
                }

                JSONObject service = new JSONObject();
                service.put("name", serviceName);
                String normalizedProtocol = GostUtil.normalizeChainProtocol(chainTunnel.getProtocol());
                String transportType = GostUtil.resolveChainTransportType(normalizedProtocol, trafficProtocol);
                String listenAddr = GostUtil.isUdpTransport(transportType) ? node.getUdpListenAddr() : node.getTcpListenAddr();
                Integer listenPort = GostUtil.resolveChainListenPort(chainTunnel.getPort(), normalizedProtocol, trafficProtocol);
                service.put("addr", listenAddr + ":" + listenPort);

                if (Objects.equals(chainTunnel.getChainType(), 3) && StringUtils.hasText(node.getInterfaceName())) {
                    JSONObject metadata = new JSONObject();
                    metadata.put("interface", node.getInterfaceName());
                    service.put("metadata", metadata);
                }

                JSONObject handler = new JSONObject();
                handler.put("type", "relay");
                if (Objects.equals(chainTunnel.getChainType(), 2)) {
                    handler.put("retries", 1);
                    handler.put("chain", GostUtil.buildChainName(chainTunnel.getTunnelId(), chainTunnel.getProtocol(), trafficProtocol));
                }
                service.put("handler", handler);

                JSONObject listener = GostUtil.createChainListener(chainTunnel.getProtocol(), trafficProtocol);
                service.put("listener", listener);
                services.add(service);
                serviceNames.add(serviceName);
            }
        }

        Set<Long> limiterIds = new LinkedHashSet<>();
        Map<String, Integer> dynamicLimiterSpeeds = new HashMap<>();
        Map<Integer, List<UserTunnelEntryPolicy>> userEntryPolicyCache = new HashMap<>();
        for (ForwardPort forwardPort : forwardPorts) {
            Forward forward = forwardService.getById(forwardPort.getForwardId());
            if (forward == null) {
                continue;
            }
            Tunnel tunnel = tunnelMap.get(forward.getTunnelId().longValue());
            if (tunnel == null) {
                continue;
            }

            UserTunnel userTunnel = userTunnelService.getOne(new QueryWrapper<UserTunnel>()
                    .eq("user_id", forward.getUserId())
                    .eq("tunnel_id", forward.getTunnelId()));

            ChainTunnel entryPolicy = chainTunnelService.getOne(new QueryWrapper<ChainTunnel>()
                    .eq("tunnel_id", forward.getTunnelId())
                    .eq("chain_type", 1)
                    .eq("node_id", node.getId()));
            if (entryPolicy != null && isChainNodeQuotaReached(entryPolicy)) {
                continue;
            }

            UserTunnelEntryPolicy userEntryPolicy = resolveUserEntryPolicy(userTunnel, forward.getTunnelId(), node.getId(), userEntryPolicyCache);
            if (userEntryPolicy != null) {
                if (!Objects.equals(userEntryPolicy.getStatus(), 1)) {
                    continue;
                }
                if (isUserEntryPolicyQuotaReached(userEntryPolicy)) {
                    continue;
                }
            }

            String overrideLimiter = null;
            if (userEntryPolicy != null && userEntryPolicy.getSpeedLimitMbps() != null && userEntryPolicy.getSpeedLimitMbps() > 0) {
                overrideLimiter = buildUserEntryLimiterName(userEntryPolicy.getUserTunnelId(), forward.getTunnelId().longValue(), node.getId());
                dynamicLimiterSpeeds.put(overrideLimiter, userEntryPolicy.getSpeedLimitMbps());
            } else if (entryPolicy != null && entryPolicy.getSpeedLimitMbps() != null && entryPolicy.getSpeedLimitMbps() > 0) {
                overrideLimiter = buildEntryLimiterName(forward.getTunnelId().longValue(), node.getId());
                dynamicLimiterSpeeds.put(overrideLimiter, entryPolicy.getSpeedLimitMbps());
            }

            int userTunnelId = userTunnel == null ? 0 : userTunnel.getId();
            String baseServiceName = forward.getId() + "_" + forward.getUserId() + "_" + userTunnelId;
            if (userTunnel != null && userTunnel.getSpeedId() != null) {
                limiterIds.add(userTunnel.getSpeedId().longValue());
            }

            Set<Long> allowedExitNodeIds = resolveAllowedExitNodeIdsForUserTunnel(userTunnel, forward.getTunnelId());
            if (allowedExitNodeIds != null && allowedExitNodeIds.isEmpty()) {
                continue;
            }

            JSONObject tcpService = buildForwardService(baseServiceName, "tcp", node, forward, forwardPort, tunnel, userTunnel, overrideLimiter, allowedExitNodeIds);
            if (tcpService != null) {
                services.add(tcpService);
            }
            JSONObject udpService = buildForwardService(baseServiceName, "udp", node, forward, forwardPort, tunnel, userTunnel, overrideLimiter, allowedExitNodeIds);
            if (udpService != null) {
                services.add(udpService);
            }
        }

        if (!limiterIds.isEmpty()) {
            List<SpeedLimit> speedLimits = speedLimitService.list(new QueryWrapper<SpeedLimit>().in("id", limiterIds));
            for (SpeedLimit speedLimit : speedLimits) {
                JSONObject limiter = new JSONObject();
                limiter.put("name", speedLimit.getId().toString());
                JSONArray limits = new JSONArray();
                String speed = convertBitsToMBps(speedLimit.getSpeed());
                limits.add("$ " + speed + "MB " + speed + "MB");
                limiter.put("limits", limits);
                limiters.add(limiter);
            }
        }

        for (Map.Entry<String, Integer> limiterEntry : dynamicLimiterSpeeds.entrySet()) {
            JSONObject limiter = new JSONObject();
            limiter.put("name", limiterEntry.getKey());
            JSONArray limits = new JSONArray();
            String speed = convertBitsToMBps(limiterEntry.getValue());
            limits.add("$ " + speed + "MB " + speed + "MB");
            limiter.put("limits", limits);
            limiters.add(limiter);
        }

        config.put("services", services);
        config.put("chains", chains);
        config.put("limiters", limiters);
        return config;
    }

    private List<ChainTunnel> getNextHops(ChainTunnel current, List<ChainTunnel> all) {
        if (Objects.equals(current.getChainType(), 1)) {
            int nextInx = 1;
            List<ChainTunnel> firstHop = all.stream()
                    .filter(item -> Objects.equals(item.getChainType(), 2) && Objects.equals(item.getInx(), nextInx))
                    .toList();
            if (!firstHop.isEmpty()) {
                return firstHop;
            }
            return all.stream().filter(item -> Objects.equals(item.getChainType(), 3)).toList();
        }

        if (Objects.equals(current.getChainType(), 2)) {
            int nextInx = (current.getInx() == null ? 0 : current.getInx()) + 1;
            List<ChainTunnel> nextHop = all.stream()
                    .filter(item -> Objects.equals(item.getChainType(), 2) && Objects.equals(item.getInx(), nextInx))
                    .toList();
            if (!nextHop.isEmpty()) {
                return nextHop;
            }
            return all.stream().filter(item -> Objects.equals(item.getChainType(), 3)).toList();
        }

        return List.of();
    }

    private JSONObject buildForwardService(String baseServiceName,
                                           String protocol,
                                           Node node,
                                           Forward forward,
                                           ForwardPort forwardPort,
                                           Tunnel tunnel,
                                           UserTunnel userTunnel,
                                           String overrideLimiter,
                                           Set<Long> allowedExitNodeIds) {
        JSONObject service = new JSONObject();
        service.put("name", baseServiceName + "_" + protocol);

        if (Objects.equals(protocol, "tcp")) {
            service.put("addr", node.getTcpListenAddr() + ":" + forwardPort.getPort());
        } else {
            service.put("addr", node.getUdpListenAddr() + ":" + forwardPort.getPort());
        }

        if (tunnel.getType() == 1 && StringUtils.hasText(node.getInterfaceName())) {
            JSONObject metadata = new JSONObject();
            metadata.put("interface", node.getInterfaceName());
            service.put("metadata", metadata);
        }

        if (StringUtils.hasText(overrideLimiter)) {
            service.put("limiter", overrideLimiter);
        } else if (userTunnel != null && userTunnel.getSpeedId() != null) {
            service.put("limiter", userTunnel.getSpeedId().toString());
        }

        JSONObject handler = new JSONObject();
        handler.put("type", protocol);
        if (tunnel.getType() == 2) {
            handler.put("retries", 1);
            String entryChainName = resolveForwardEntryChainName(forward.getTunnelId().longValue(), protocol, allowedExitNodeIds);
            if (!StringUtils.hasText(entryChainName)) {
                return null;
            }
            handler.put("chain", entryChainName);
        }
        service.put("handler", handler);

        JSONObject listener = new JSONObject();
        listener.put("type", protocol);
        if (Objects.equals(protocol, "udp")) {
            JSONObject metadata = new JSONObject();
            metadata.put("keepAlive", true);
            listener.put("metadata", metadata);
        }
        service.put("listener", listener);

        JSONObject forwarder = new JSONObject();
        JSONArray nodes = new JSONArray();
        String[] split = forward.getRemoteAddr().split(",");
        int num = 1;
        for (String addr : split) {
            String remote = addr == null ? "" : addr.trim();
            if (!StringUtils.hasText(remote)) {
                continue;
            }
            JSONObject nodeItem = new JSONObject();
            nodeItem.put("name", "node_" + num);
            nodeItem.put("addr", remote);
            nodes.add(nodeItem);
            num++;
        }
        forwarder.put("nodes", nodes);
        forwarder.put("probePeriod", "10s");
        forwarder.put("probeTimeout", "3s");

        JSONObject selector = new JSONObject();
        selector.put("strategy", StringUtils.hasText(forward.getStrategy()) ? forward.getStrategy() : "fifo");
        selector.put("maxFails", 1);
        selector.put("failTimeout", "600s");
        forwarder.put("selector", selector);
        service.put("forwarder", forwarder);

        return service;
    }

    private List<String> resolveTrafficProtocols(List<ChainTunnel> nextHops) {
        if (nextHops == null || nextHops.isEmpty()) {
            return List.of(GostUtil.PROTOCOL_TCP);
        }
        String protocol = nextHops.getFirst().getProtocol();
        if (GostUtil.isHybridUdpProtocol(protocol)) {
            return List.of(GostUtil.PROTOCOL_TCP, GostUtil.PROTOCOL_UDP);
        }
        return List.of(GostUtil.PROTOCOL_TCP);
    }

    private boolean isForwardServiceName(String serviceName) {
        if (!StringUtils.hasText(serviceName)) {
            return false;
        }
        String[] parts = serviceName.split("_");
        if (parts.length < 3) {
            return false;
        }
        return isNumeric(parts[0]) && isNumeric(parts[1]) && isNumeric(parts[2]);
    }

    private boolean isNumeric(String value) {
        if (!StringUtils.hasText(value)) {
            return false;
        }
        for (int i = 0; i < value.length(); i++) {
            if (!Character.isDigit(value.charAt(i))) {
                return false;
            }
        }
        return true;
    }

    private List<ChainTunnel> filterUnavailableExitHops(List<ChainTunnel> nextHops) {
        if (nextHops == null || nextHops.isEmpty()) {
            return List.of();
        }
        if (!Objects.equals(nextHops.getFirst().getChainType(), 3)) {
            return nextHops;
        }
        return nextHops.stream().filter(this::isExitNodeSelectable).toList();
    }

    private boolean isExitNodeSelectable(ChainTunnel chainTunnel) {
        if (chainTunnel == null) {
            return false;
        }
        if (isChainNodeQuotaReached(chainTunnel)) {
            return false;
        }
        if (Objects.equals(chainTunnel.getBandwidthOverloaded(), 1)) {
            return false;
        }
        if (chainTunnel.getHealthStatus() != null && chainTunnel.getHealthStatus() != 1) {
            return false;
        }
        return chainTunnel.getLastLatencyMs() == null || chainTunnel.getLastLatencyMs() <= EXIT_MAX_LATENCY_MS;
    }

    private boolean isChainNodeQuotaReached(ChainTunnel chainTunnel) {
        if (chainTunnel == null || chainTunnel.getFlowQuotaGb() == null || chainTunnel.getFlowQuotaGb() <= 0) {
            return false;
        }
        long usedFlow = safeLong(chainTunnel.getInFlow()) + safeLong(chainTunnel.getOutFlow());
        return usedFlow >= chainTunnel.getFlowQuotaGb() * BYTES_TO_GB;
    }

    private String buildEntryLimiterName(Long tunnelId, Long nodeId) {
        return "entry_" + tunnelId + "_" + nodeId;
    }

    private String buildUserEntryLimiterName(Integer userTunnelId, Long tunnelId, Long nodeId) {
        return "user_entry_" + userTunnelId + "_" + tunnelId + "_" + nodeId;
    }

    private long safeLong(Long value) {
        return value == null ? 0L : value;
    }

    private UserTunnelEntryPolicy resolveUserEntryPolicy(UserTunnel userTunnel,
                                                         Integer tunnelId,
                                                         Long entryNodeId,
                                                         Map<Integer, List<UserTunnelEntryPolicy>> cache) {
        if (userTunnel == null || userTunnel.getId() == null || tunnelId == null || entryNodeId == null) {
            return null;
        }
        List<UserTunnelEntryPolicy> policies = cache.computeIfAbsent(
                userTunnel.getId(),
                id -> userTunnelEntryPolicyService.syncAndListByUserTunnelId(id)
        );
        for (UserTunnelEntryPolicy policy : policies) {
            if (Objects.equals(policy.getTunnelId(), tunnelId) && Objects.equals(policy.getEntryNodeId(), entryNodeId)) {
                return policy;
            }
        }
        return null;
    }

    private boolean isUserEntryPolicyQuotaReached(UserTunnelEntryPolicy policy) {
        if (policy == null || policy.getFlowQuotaGb() == null || policy.getFlowQuotaGb() <= 0) {
            return false;
        }
        return safeLong(policy.getUsedFlow()) >= policy.getFlowQuotaGb() * BYTES_TO_GB;
    }

    private String resolveForwardEntryChainName(Long tunnelId, String trafficProtocol, Set<Long> allowedExitNodeIds) {
        List<ChainTunnel> all = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnelId));
        List<ChainTunnel> firstHop = all.stream()
                .filter(item -> Objects.equals(item.getChainType(), 2) && Objects.equals(item.getInx(), 1))
                .toList();
        if (firstHop.isEmpty()) {
            firstHop = filterUnavailableExitHops(all.stream().filter(item -> Objects.equals(item.getChainType(), 3)).toList());
            if (allowedExitNodeIds != null) {
                firstHop = firstHop.stream()
                        .filter(item -> item.getNodeId() != null && allowedExitNodeIds.contains(item.getNodeId()))
                        .toList();
            }
        }

        if (firstHop.isEmpty()) {
            return null;
        }
        return GostUtil.buildChainName(tunnelId, firstHop.getFirst().getProtocol(), trafficProtocol);
    }

    private Set<Long> resolveAllowedExitNodeIdsForUserTunnel(UserTunnel userTunnel, Integer tunnelId) {
        if (userTunnel == null || userTunnel.getId() == null || tunnelId == null) {
            return null;
        }
        int chainHopCount = chainTunnelService.count(new QueryWrapper<ChainTunnel>()
                .eq("tunnel_id", tunnelId)
                .eq("chain_type", 2));
        if (chainHopCount > 0) {
            return null;
        }

        List<UserTunnelExitPolicy> policies = userTunnelExitPolicyService.syncAndListByUserTunnelId(userTunnel.getId());
        if (policies.isEmpty()) {
            return null;
        }

        Set<Long> allowedExitNodeIds = new LinkedHashSet<>();
        for (UserTunnelExitPolicy policy : policies) {
            if (policy.getExitNodeId() == null || !Objects.equals(policy.getStatus(), 1)) {
                continue;
            }
            if (isUserExitPolicyQuotaReached(policy)) {
                continue;
            }
            if (!isExitHealthy(tunnelId.longValue(), policy.getExitNodeId())) {
                continue;
            }
            allowedExitNodeIds.add(policy.getExitNodeId());
        }
        return allowedExitNodeIds;
    }

    private boolean isExitHealthy(Long tunnelId, Long exitNodeId) {
        ChainTunnel exit = chainTunnelService.getOne(new QueryWrapper<ChainTunnel>()
                .eq("tunnel_id", tunnelId)
                .eq("chain_type", 3)
                .eq("node_id", exitNodeId));
        return isExitNodeSelectable(exit);
    }

    private boolean isUserExitPolicyQuotaReached(UserTunnelExitPolicy policy) {
        if (policy == null || policy.getFlowQuotaGb() == null || policy.getFlowQuotaGb() <= 0) {
            return false;
        }
        return safeLong(policy.getUsedFlow()) >= policy.getFlowQuotaGb() * BYTES_TO_GB;
    }

    private void updateUserEntryPolicyUsage(String userTunnelId, Integer tunnelId, Long entryNodeId, FlowDto flowStats) {
        if (!StringUtils.hasText(userTunnelId) || tunnelId == null || entryNodeId == null || flowStats == null) {
            return;
        }

        Integer userTunnelPk;
        try {
            userTunnelPk = Integer.parseInt(userTunnelId);
        } catch (NumberFormatException ignored) {
            return;
        }

        List<UserTunnelEntryPolicy> policies = userTunnelEntryPolicyService.syncAndListByUserTunnelId(userTunnelPk);
        if (policies.isEmpty()) {
            return;
        }

        UserTunnelEntryPolicy matchedPolicy = null;
        for (UserTunnelEntryPolicy policy : policies) {
            if (!Objects.equals(policy.getTunnelId(), tunnelId) || !Objects.equals(policy.getEntryNodeId(), entryNodeId)) {
                continue;
            }
            matchedPolicy = policy;
            break;
        }
        if (matchedPolicy == null || matchedPolicy.getId() == null) {
            return;
        }

        long delta = Math.max(0L, flowStats.getD()) + Math.max(0L, flowStats.getU());
        if (delta <= 0) {
            return;
        }

        long before = safeLong(matchedPolicy.getUsedFlow());
        userTunnelEntryPolicyService.addUsedFlow(matchedPolicy.getId(), delta);
        if (matchedPolicy.getFlowQuotaGb() != null && matchedPolicy.getFlowQuotaGb() > 0) {
            long limit = matchedPolicy.getFlowQuotaGb() * BYTES_TO_GB;
            if (before < limit && before + delta >= limit) {
                triggerTunnelEntryConfigRefresh(tunnelId.longValue(), entryNodeId);
            }
        }
    }

    private void updateUserExitPolicyUsage(String userTunnelId, Integer tunnelId, FlowDto flowStats) {
        if (!StringUtils.hasText(userTunnelId) || tunnelId == null || flowStats == null) {
            return;
        }
        int chainHopCount = chainTunnelService.count(new QueryWrapper<ChainTunnel>()
                .eq("tunnel_id", tunnelId)
                .eq("chain_type", 2));
        if (chainHopCount > 0) {
            return;
        }

        Integer userTunnelPk;
        try {
            userTunnelPk = Integer.parseInt(userTunnelId);
        } catch (NumberFormatException ignored) {
            return;
        }

        List<UserTunnelExitPolicy> policies = userTunnelExitPolicyService.syncAndListByUserTunnelId(userTunnelPk);
        if (policies.isEmpty()) {
            return;
        }

        UserTunnelExitPolicy activePolicy = null;
        for (UserTunnelExitPolicy policy : policies) {
            if (!Objects.equals(policy.getStatus(), 1)) {
                continue;
            }
            if (isUserExitPolicyQuotaReached(policy)) {
                continue;
            }
            if (!isExitHealthy(tunnelId.longValue(), policy.getExitNodeId())) {
                continue;
            }
            activePolicy = policy;
            break;
        }
        if (activePolicy == null || activePolicy.getId() == null) {
            return;
        }

        long delta = Math.max(0L, flowStats.getD()) + Math.max(0L, flowStats.getU());
        if (delta <= 0) {
            return;
        }
        long before = safeLong(activePolicy.getUsedFlow());
        userTunnelExitPolicyService.addUsedFlow(activePolicy.getId(), delta);
        if (activePolicy.getFlowQuotaGb() != null && activePolicy.getFlowQuotaGb() > 0) {
            long limit = activePolicy.getFlowQuotaGb() * BYTES_TO_GB;
            if (before < limit && before + delta >= limit) {
                triggerTunnelEntryConfigRefresh(tunnelId.longValue());
            }
        }
    }

    private String convertBitsToMBps(Integer speedInBits) {
        if (speedInBits == null) {
            return "0.0";
        }
        double mbs = speedInBits / 8.0;
        return String.format("%.1f", mbs);
    }

    private boolean isValidNode(String secret) {
        int nodeCount = nodeService.count(new QueryWrapper<Node>().eq("secret", secret));
        return nodeCount > 0;
    }

    private String[] parseServiceName(String serviceName) {
        return serviceName.split("_");
    }

    private String buildServiceName(String forwardId, String userId, String userTunnelId) {
        return forwardId + "_" + userId + "_" + userTunnelId;
    }
}
