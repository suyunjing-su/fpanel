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

    // 用于同步相同用户和隧道的流量更新操作
    private static final ConcurrentHashMap<String, Object> USER_LOCKS = new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<String, Object> TUNNEL_LOCKS = new ConcurrentHashMap<>();
    private static final ConcurrentHashMap<String, Object> FORWARD_LOCKS = new ConcurrentHashMap<>();

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

        // 2. 尝试解密数据
        String decryptedData = decryptIfNeeded(rawData, secret);

        // 3. 解析为FlowDto列表
        JSONArray flowDataList = JSONObject.parseArray(decryptedData);
        log.info("节点上报流量数据{}", flowDataList);
        for (int i = 0; i < flowDataList.size(); i++) {
            String jsonObject = flowDataList.getJSONObject(i).toJSONString();
            FlowDto flowDto = JSONObject.parseObject(jsonObject, FlowDto.class);
            if (!Objects.equals(flowDto.getN(), "web_api")) {
                processFlowData(flowDto);
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
    private void processFlowData(FlowDto flowDataList) {
        String[] serviceIds = parseServiceName(flowDataList.getN());
        String forwardId = serviceIds[0];
        String userId = serviceIds[1];
        String userTunnelId = serviceIds[2];

        Forward forward = forwardService.getById(forwardId);
        if (forward != null){
            Tunnel tunnel = tunnelService.getById(forward.getTunnelId());

            //  处理流量倍率及单双向计算
            BigDecimal trafficRatio = tunnel.getTrafficRatio();
            BigDecimal originalD = BigDecimal.valueOf(flowDataList.getD());
            BigDecimal originalU = BigDecimal.valueOf(flowDataList.getU());
            BigDecimal newD = originalD.multiply(trafficRatio);
            BigDecimal newU = originalU.multiply(trafficRatio);
            flowDataList.setD(newD.longValue() * tunnel.getFlow());
            flowDataList.setU(newU.longValue() * tunnel.getFlow());
        }

        // 先更新所有流量统计 - 确保流量数据的一致性
        updateForwardFlow(forwardId, flowDataList);
        updateUserFlow(userId, flowDataList);
        updateUserTunnelFlow(userTunnelId, flowDataList);

        // 7. 检查和服务暂停操作
        String name = buildServiceName(forwardId, userId, userTunnelId);
        if (!Objects.equals(userTunnelId, DEFAULT_USER_TUNNEL_ID)) { // 非管理员的转发需要检测流量限制
            checkUserRelatedLimits(userId, name);
            checkUserTunnelRelatedLimits(userTunnelId, name, userId);
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
            updateWrapper.setSql("in_flow = in_flow + " + flowStats.getD());
            updateWrapper.setSql("out_flow = out_flow + " + flowStats.getU());

            forwardService.update(null, updateWrapper);
        }
    }

    private void updateUserFlow(String userId, FlowDto flowStats) {
        // 对相同用户的流量更新进行同步，避免并发覆盖
        synchronized (getUserLock(userId)) {
            UpdateWrapper<User> updateWrapper = new UpdateWrapper<>();
            updateWrapper.eq("id", userId);

            updateWrapper.setSql("in_flow = in_flow + " + flowStats.getD());
            updateWrapper.setSql("out_flow = out_flow + " + flowStats.getU());

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
            updateWrapper.setSql("in_flow = in_flow + " + flowStats.getD());
            updateWrapper.setSql("out_flow = out_flow + " + flowStats.getU());
            userTunnelService.update(null, updateWrapper);
        }
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

            List<ChainTunnel> nextHops = getNextHops(chainTunnel, chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", chainTunnel.getTunnelId())));
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

            int userTunnelId = userTunnel == null ? 0 : userTunnel.getId();
            String baseServiceName = forward.getId() + "_" + forward.getUserId() + "_" + userTunnelId;
            if (userTunnel != null && userTunnel.getSpeedId() != null) {
                limiterIds.add(userTunnel.getSpeedId().longValue());
            }

            services.add(buildForwardService(baseServiceName, "tcp", node, forward, forwardPort, tunnel, userTunnel));
            services.add(buildForwardService(baseServiceName, "udp", node, forward, forwardPort, tunnel, userTunnel));
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
                                           UserTunnel userTunnel) {
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

        if (userTunnel != null && userTunnel.getSpeedId() != null) {
            service.put("limiter", userTunnel.getSpeedId().toString());
        }

        JSONObject handler = new JSONObject();
        handler.put("type", protocol);
        if (tunnel.getType() == 2) {
            handler.put("chain", resolveForwardEntryChainName(forward.getTunnelId().longValue(), protocol));
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

    private String resolveForwardEntryChainName(Long tunnelId, String trafficProtocol) {
        List<ChainTunnel> all = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnelId));
        List<ChainTunnel> firstHop = all.stream()
                .filter(item -> Objects.equals(item.getChainType(), 2) && Objects.equals(item.getInx(), 1))
                .toList();
        if (firstHop.isEmpty()) {
            firstHop = all.stream().filter(item -> Objects.equals(item.getChainType(), 3)).toList();
        }

        if (firstHop.isEmpty()) {
            return "chains_" + tunnelId;
        }
        return GostUtil.buildChainName(tunnelId, firstHop.getFirst().getProtocol(), trafficProtocol);
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
