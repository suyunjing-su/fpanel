package com.admin.service.impl;

import com.admin.common.dto.*;

import com.admin.common.lang.R;
import com.admin.common.utils.GostUtil;
import com.admin.common.utils.JwtUtil;
import com.admin.common.utils.WebSocketServer;
import com.admin.entity.*;
import com.admin.mapper.TunnelMapper;
import com.admin.mapper.UserTunnelMapper;
import com.admin.service.*;
import com.alibaba.fastjson.JSONArray;
import com.alibaba.fastjson.JSONObject;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import lombok.Data;
import org.apache.commons.lang3.StringUtils;
import org.springframework.beans.BeanUtils;
import org.springframework.stereotype.Service;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.transaction.support.TransactionSynchronization;
import org.springframework.transaction.support.TransactionSynchronizationManager;

import javax.annotation.Resource;
import java.util.*;
import java.util.stream.Collectors;

/**
 *
 * @author QAQ
 * @since 2025-06-03
 */
@Service
public class TunnelServiceImpl extends ServiceImpl<TunnelMapper, Tunnel> implements TunnelService {

    private static final Set<String> SUPPORTED_CHAIN_PROTOCOLS = Set.of(
            GostUtil.PROTOCOL_TCP,
            GostUtil.PROTOCOL_UDP_QUIC,
            GostUtil.PROTOCOL_UDP_KCP,
            GostUtil.PROTOCOL_MPTCP,
            "tls",
            "wss",
            "mtls",
            "mwss",
            "mtcp"
    );

    private static final Set<String> SUPPORTED_CHAIN_STRATEGIES = Set.of(
            "fifo",
            "round",
            "rand"
    );


    @Resource
    UserTunnelMapper userTunnelMapper;

    @Resource
    NodeService nodeService;

    @Resource
    ForwardService forwardService;

    @Resource
    UserTunnelService userTunnelService;

    @Resource
    ChainTunnelService chainTunnelService;

    @Resource
    ForwardPortService forwardPortService;


    @Override
    public R createTunnel(TunnelDto tunnelDto) {

        int count = this.count(new QueryWrapper<Tunnel>().eq("name", tunnelDto.getName()));
        if (count > 0) return R.err("隧道名称重复");
        if (tunnelDto.getType() == 2 && tunnelDto.getOutNodeId() == null) return R.err("出口不能为空");


        List<ChainTunnel> chainTunnels = new ArrayList<>();
        Map<Long, Node> nodes = new HashMap<>();

        List<Long> node_ids = new ArrayList<>();
        for (ChainTunnel in_node : tunnelDto.getInNodeId()) {
            if (!isValidFlowQuota(in_node.getFlowQuotaGb())) return R.err("入口流量配额不能小于0");
            if (!isValidSpeedLimit(in_node.getSpeedLimitMbps())) return R.err("入口限速不能小于0");
            node_ids.add(in_node.getNodeId());
            in_node.setFlowQuotaGb(normalizeNullableLong(in_node.getFlowQuotaGb()));
            in_node.setSpeedLimitMbps(normalizeNullableInteger(in_node.getSpeedLimitMbps()));
            in_node.setInFlow(0L);
            in_node.setOutFlow(0L);
            in_node.setHealthStatus(1);
            in_node.setLastLatencyMs(null);
            in_node.setHealthCheckedTime(null);
            chainTunnels.add(in_node);

            Node node = nodeService.getById(in_node.getNodeId());
            if (node == null) return R.err("节点不存在");
            nodes.put(node.getId(), node);
        }

        if (tunnelDto.getType() == 2) {
            // 处理转发链节点，为每一跳设置inx
            int inx = 1;
            for (List<ChainTunnel> chainNode : tunnelDto.getChainNodes()) {
                for (ChainTunnel chain_node : chainNode) {
                    String protocol = normalizeAndValidateChainProtocol(chain_node);
                    if (protocol == null) return R.err("隧道协议不支持: " + chain_node.getProtocol());
                    node_ids.add(chain_node.getNodeId());
                    Node node = nodeService.getById(chain_node.getNodeId());
                    if (node == null) return R.err("节点不存在");
                    nodes.put(node.getId(), node);
                    Integer nodePort = getNodePort(chain_node.getNodeId(), protocol);
                    chain_node.setPort(nodePort);
                    chain_node.setInx(inx); // 设置转发链序号
                    chain_node.setProtocol(protocol);
                    chainTunnels.add(chain_node);
                }
                inx++; // 每一跳递增
            }
            for (ChainTunnel out_node : tunnelDto.getOutNodeId()) {
                String protocol = normalizeAndValidateChainProtocol(out_node);
                if (protocol == null) return R.err("隧道协议不支持: " + out_node.getProtocol());
                if (!isValidFlowQuota(out_node.getFlowQuotaGb())) return R.err("出口流量配额不能小于0");
                if (!isValidSpeedLimit(out_node.getSpeedLimitMbps())) return R.err("出口限速不能小于0");
                node_ids.add(out_node.getNodeId());
                Node node = nodeService.getById(out_node.getNodeId());
                if (node == null) return R.err("节点不存在");
                nodes.put(node.getId(), node);
                Integer nodePort = getNodePort(out_node.getNodeId(), protocol);
                out_node.setPort(nodePort);
                out_node.setProtocol(protocol);
                out_node.setFlowQuotaGb(normalizeNullableLong(out_node.getFlowQuotaGb()));
                out_node.setSpeedLimitMbps(normalizeNullableInteger(out_node.getSpeedLimitMbps()));
                out_node.setInFlow(0L);
                out_node.setOutFlow(0L);
                if (out_node.getHealthStatus() == null) {
                    out_node.setHealthStatus(1);
                }
                out_node.setLastLatencyMs(null);
                out_node.setHealthCheckedTime(null);
                chainTunnels.add(out_node);
            }

        }
        Set<Long> set = new HashSet<>(node_ids);
        boolean hasDuplicate = set.size() != node_ids.size();
        if (hasDuplicate) return R.err("节点重复");

        List<Node> list = nodeService.list(new QueryWrapper<Node>().in("id", node_ids));
        if (list.size() != node_ids.size()) return R.err("部分节点不存在");
        for (Node node : list) {
            if (node.getStatus() != 1) return R.err("部分节点不在线");
        }


        Tunnel tunnel = new Tunnel();
        BeanUtils.copyProperties(tunnelDto, tunnel);
        tunnel.setStatus(1);
        long currentTime = System.currentTimeMillis();
        tunnel.setCreatedTime(currentTime);
        tunnel.setUpdatedTime(currentTime);
        if (StringUtils.isEmpty(tunnel.getInIp())){
            StringBuilder in_ip = new StringBuilder();
            for (ChainTunnel chainTunnel : tunnelDto.getInNodeId()) {
                Node node = nodes.get(chainTunnel.getNodeId());
                in_ip.append(node.getServerIp()).append(",");
            }
            in_ip.deleteCharAt(in_ip.length() - 1);
            tunnel.setInIp(in_ip.toString());
        }

        this.save(tunnel);
        for (ChainTunnel chainTunnel : chainTunnels) {
            chainTunnel.setTunnelId(tunnel.getId());
        }
        chainTunnelService.saveBatch(chainTunnels);

        List<JSONObject> chain_success = new ArrayList<>();
        List<JSONObject> service_success = new ArrayList<>();



        if (tunnel.getType() == 2) {

            for (ChainTunnel in_node : tunnelDto.getInNodeId()) {
                // 创建Chain， 指向chainNode的第一跳。如果chainNode为空就是指向出口
                if (tunnelDto.getChainNodes().isEmpty()) { // 指向出口
                    List<String> trafficProtocols = resolveTrafficProtocols(tunnelDto.getOutNodeId());
                    for (String trafficProtocol : trafficProtocols) {
                        GostDto gostDto = GostUtil.AddChains(in_node.getNodeId(), tunnelDto.getOutNodeId(), nodes, trafficProtocol);
                        if (Objects.equals(gostDto.getMsg(), "OK")) {
                            JSONObject data = new JSONObject();
                            data.put("node_id", in_node.getNodeId());
                            data.put("name", GostUtil.buildChainName(tunnel.getId(), tunnelDto.getOutNodeId().getFirst().getProtocol(), trafficProtocol));
                            chain_success.add(data);
                        } else {
                            this.removeById(tunnel.getId());
                            chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnel.getId()));
                            for (JSONObject chainSuccess : chain_success) {
                                GostDto deleteChains = GostUtil.DeleteChains(chainSuccess.getLong("node_id"), chainSuccess.getString("name"));
                                System.out.println(deleteChains);
                            }
                            return R.err(gostDto.getMsg());
                        }
                    }

                } else {
                    List<ChainTunnel> firstHop = tunnelDto.getChainNodes().getFirst();
                    List<String> trafficProtocols = resolveTrafficProtocols(firstHop);
                    for (String trafficProtocol : trafficProtocols) {
                        GostDto gostDto = GostUtil.AddChains(in_node.getNodeId(), firstHop, nodes, trafficProtocol);// 指向第一跳
                        if (Objects.equals(gostDto.getMsg(), "OK")){
                            JSONObject data = new JSONObject();
                            data.put("node_id", in_node.getNodeId());
                            data.put("name", GostUtil.buildChainName(tunnel.getId(), firstHop.getFirst().getProtocol(), trafficProtocol));
                            chain_success.add(data);
                        }else {
                            this.removeById(tunnel.getId());
                            chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnel.getId()));
                            for (JSONObject chainSuccess : chain_success) {
                                GostDto deleteChains = GostUtil.DeleteChains(chainSuccess.getLong("node_id"), chainSuccess.getString("name"));
                                System.out.println(deleteChains);
                            }
                            return R.err(gostDto.getMsg());
                        }
                    }
                }
            }

            for (int i = 0; i < tunnelDto.getChainNodes().size(); i++) {
                //  创建Chain和Service。每一条的Chain都是指向下一跳。最后一跳指向出口， Service是监听端口
                List<ChainTunnel> chainTunnels1 = tunnelDto.getChainNodes().get(i);
                for (ChainTunnel chainTunnel : chainTunnels1) {
                    int inx = i+1;
                    if (inx >= tunnelDto.getChainNodes().size()) { // 指向出口
                        List<String> trafficProtocols = resolveTrafficProtocols(tunnelDto.getOutNodeId());
                        for (String trafficProtocol : trafficProtocols) {
                            GostDto gostDto = GostUtil.AddChains(chainTunnel.getNodeId(), tunnelDto.getOutNodeId(), nodes, trafficProtocol);
                            if (Objects.equals(gostDto.getMsg(), "OK")){
                                JSONObject data = new JSONObject();
                                data.put("node_id", chainTunnel.getNodeId());
                                data.put("name", GostUtil.buildChainName(tunnel.getId(), tunnelDto.getOutNodeId().getFirst().getProtocol(), trafficProtocol));
                                chain_success.add(data);
                            }else {
                                this.removeById(tunnel.getId());
                                chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnel.getId()));
                                for (JSONObject chainSuccess : chain_success) {
                                    GostDto deleteChains = GostUtil.DeleteChains(chainSuccess.getLong("node_id"), chainSuccess.getString("name"));
                                    System.out.println(deleteChains);
                                }
                                return R.err(gostDto.getMsg());
                            }
                        }
                    } else {
                        List<ChainTunnel> nextHop = tunnelDto.getChainNodes().get(inx);
                        List<String> trafficProtocols = resolveTrafficProtocols(nextHop);
                        for (String trafficProtocol : trafficProtocols) {
                            GostDto gostDto = GostUtil.AddChains(chainTunnel.getNodeId(), nextHop, nodes, trafficProtocol);
                            if (Objects.equals(gostDto.getMsg(), "OK")){
                                JSONObject data = new JSONObject();
                                data.put("node_id", chainTunnel.getNodeId());
                                data.put("name", GostUtil.buildChainName(tunnel.getId(), nextHop.getFirst().getProtocol(), trafficProtocol));
                                chain_success.add(data);
                            }else {
                                this.removeById(tunnel.getId());
                                chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnel.getId()));
                                for (JSONObject chainSuccess : chain_success) {
                                    GostDto deleteChains = GostUtil.DeleteChains(chainSuccess.getLong("node_id"), chainSuccess.getString("name"));
                                    System.out.println(deleteChains);
                                }
                                return R.err(gostDto.getMsg());
                            }
                        }
                    }

                    List<String> serviceTrafficProtocols = resolveTrafficProtocols(List.of(chainTunnel));
                    for (String trafficProtocol : serviceTrafficProtocols) {
                        GostDto gostDto = GostUtil.AddChainService(chainTunnel.getNodeId(), chainTunnel, nodes, trafficProtocol);
                        if (Objects.equals(gostDto.getMsg(), "OK")){
                            JSONObject data = new JSONObject();
                            data.put("node_id", chainTunnel.getNodeId());
                            data.put("name", GostUtil.buildChainServiceName(tunnel.getId(), chainTunnel.getProtocol(), trafficProtocol));
                            service_success.add(data);
                        }else {
                            this.removeById(tunnel.getId());
                            chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnel.getId()));
                            for (JSONObject serviceSuccess : service_success) {
                                JSONArray jsonArray = new JSONArray();
                                jsonArray.add(serviceSuccess.getString("name"));
                                GostDto deleteService = GostUtil.DeleteService(serviceSuccess.getLong("node_id"), jsonArray);
                                System.out.println(deleteService);
                            }
                            return R.err(gostDto.getMsg());
                        }
                    }
                }

            }


            for (ChainTunnel out_node : tunnelDto.getOutNodeId()) {
                List<String> trafficProtocols = resolveTrafficProtocols(List.of(out_node));
                for (String trafficProtocol : trafficProtocols) {
                    GostDto gostDto = GostUtil.AddChainService(out_node.getNodeId(), out_node, nodes, trafficProtocol);
                    if (Objects.equals(gostDto.getMsg(), "OK")){
                        JSONObject data = new JSONObject();
                        data.put("node_id", out_node.getNodeId());
                        data.put("name", GostUtil.buildChainServiceName(tunnel.getId(), out_node.getProtocol(), trafficProtocol));
                        service_success.add(data);
                    }else {
                        this.removeById(tunnel.getId());
                        chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnel.getId()));
                        for (JSONObject serviceSuccess : service_success) {
                            JSONArray jsonArray = new JSONArray();
                            jsonArray.add(serviceSuccess.getString("name"));
                            GostDto deleteService = GostUtil.DeleteService(serviceSuccess.getLong("node_id"), jsonArray);
                            System.out.println(deleteService);
                        }
                        return R.err(gostDto.getMsg());
                    }
                }
            }

        }
        return R.ok();
    }


    @Override
    public R getAllTunnels() {
        List<Tunnel> tunnelList = this.list();
        
        // 查询所有隧道的ChainTunnel信息
        List<Long> tunnelIds = tunnelList.stream()
                .map(Tunnel::getId)
                .collect(Collectors.toList());
        
        if (tunnelIds.isEmpty()) {
            return R.ok(new ArrayList<TunnelDetailDto>());
        }
        
        // 批量查询所有ChainTunnel记录
        List<ChainTunnel> allChainTunnels = chainTunnelService.list(
                new QueryWrapper<ChainTunnel>().in("tunnel_id", tunnelIds)
        );
        
        // 按tunnelId分组
        Map<Long, List<ChainTunnel>> chainTunnelMap = allChainTunnels.stream()
                .collect(Collectors.groupingBy(ChainTunnel::getTunnelId));
        
        // 转换为TunnelDetailDto列表
        List<TunnelDetailDto> detailDtoList = tunnelList.stream()
                .map(tunnel -> {
                    TunnelDetailDto detailDto = new TunnelDetailDto();
                    BeanUtils.copyProperties(tunnel, detailDto);
                    
                    List<ChainTunnel> chainTunnels = chainTunnelMap.getOrDefault(tunnel.getId(), new ArrayList<>());
                    
                    // 按chainType分类节点
                    // 入口节点 (chainType = 1)
                    List<ChainTunnel> inNodes = chainTunnels.stream()
                            .filter(ct -> ct.getChainType() != null && ct.getChainType() == 1)
                            .collect(Collectors.toList());
                    detailDto.setInNodeId(inNodes);

                    detailDto.setInIp(tunnel.getInIp());
                    
                    // 转发链节点 (chainType = 2) - 按inx分组
                    Map<Integer, List<ChainTunnel>> chainNodesMap = chainTunnels.stream()
                            .filter(ct -> ct.getChainType() != null && ct.getChainType() == 2)
                            .collect(Collectors.groupingBy(
                                    ct -> ct.getInx() != null ? ct.getInx() : 0,
                                    Collectors.toList()
                            ));
                    
                    // 将Map转换为按inx排序的二维列表
                    List<List<ChainTunnel>> chainNodesList = chainNodesMap.entrySet().stream()
                            .sorted(Map.Entry.comparingByKey())
                            .map(Map.Entry::getValue)
                            .collect(Collectors.toList());
                    detailDto.setChainNodes(chainNodesList);
                    
                    // 出口节点 (chainType = 3)
                    List<ChainTunnel> outNodes = chainTunnels.stream()
                            .filter(ct -> ct.getChainType() != null && ct.getChainType() == 3)
                            .collect(Collectors.toList());
                    detailDto.setOutNodeId(outNodes);
                    
                    return detailDto;
                })
                .collect(Collectors.toList());
        
        return R.ok(detailDtoList);
    }


    @Override
    @Transactional
    public R updateTunnel(TunnelUpdateDto tunnelUpdateDto) {
        Tunnel existingTunnel = this.getById(tunnelUpdateDto.getId());
        if (existingTunnel == null) return R.err("隧道不存在");

        int duplicateCount = this.count(new QueryWrapper<Tunnel>()
                .eq("name", tunnelUpdateDto.getName())
                .ne("id", tunnelUpdateDto.getId()));
        if (duplicateCount > 0) return R.err("隧道名称重复");

        List<ChainTunnel> updatedChainTunnels = new ArrayList<>();
        List<Long> allNodeIds = new ArrayList<>();
        Map<Long, Node> nodes = new HashMap<>();

        List<ChainTunnel> inNodes = Optional.ofNullable(tunnelUpdateDto.getInNodeId()).orElse(Collections.emptyList());
        if (inNodes.isEmpty()) return R.err("请至少选择一个入口节点");
        for (ChainTunnel inNode : inNodes) {
            if (inNode == null || inNode.getNodeId() == null) {
                return R.err("入口节点数据错误");
            }
            if (!isValidFlowQuota(inNode.getFlowQuotaGb())) return R.err("入口流量配额不能小于0");
            if (!isValidSpeedLimit(inNode.getSpeedLimitMbps())) return R.err("入口限速不能小于0");
            allNodeIds.add(inNode.getNodeId());
            ChainTunnel entry = new ChainTunnel();
            entry.setChainType(1);
            entry.setNodeId(inNode.getNodeId());
            entry.setFlowQuotaGb(normalizeNullableLong(inNode.getFlowQuotaGb()));
            entry.setSpeedLimitMbps(normalizeNullableInteger(inNode.getSpeedLimitMbps()));
            entry.setInFlow(0L);
            entry.setOutFlow(0L);
            entry.setHealthStatus(1);
            updatedChainTunnels.add(entry);
        }

        if (existingTunnel.getType() == 2) {
            List<List<ChainTunnel>> chainNodes = Optional.ofNullable(tunnelUpdateDto.getChainNodes()).orElse(Collections.emptyList());
            int inx = 1;
            for (List<ChainTunnel> chainGroup : chainNodes) {
                if (chainGroup == null || chainGroup.isEmpty()) {
                    inx++;
                    continue;
                }
                for (ChainTunnel chainNode : chainGroup) {
                    if (chainNode == null || chainNode.getNodeId() == null) {
                        return R.err("转发链节点数据错误");
                    }
                    String protocol = normalizeAndValidateChainProtocol(chainNode);
                    if (protocol == null) return R.err("隧道协议不支持: " + chainNode.getProtocol());
                    String strategy = normalizeAndValidateChainStrategy(chainNode.getStrategy());
                    if (strategy == null) return R.err("负载策略不支持: " + chainNode.getStrategy());

                    allNodeIds.add(chainNode.getNodeId());

                    ChainTunnel hopNode = new ChainTunnel();
                    hopNode.setChainType(2);
                    hopNode.setNodeId(chainNode.getNodeId());
                    hopNode.setInx(inx);
                    hopNode.setProtocol(protocol);
                    hopNode.setStrategy(strategy);
                    hopNode.setInFlow(0L);
                    hopNode.setOutFlow(0L);
                    hopNode.setHealthStatus(1);
                    updatedChainTunnels.add(hopNode);
                }
                inx++;
            }

            List<ChainTunnel> outNodes = Optional.ofNullable(tunnelUpdateDto.getOutNodeId()).orElse(Collections.emptyList());
            if (outNodes.isEmpty()) return R.err("请至少选择一个出口节点");
            for (ChainTunnel outNode : outNodes) {
                if (outNode == null || outNode.getNodeId() == null) {
                    return R.err("出口节点数据错误");
                }
                String protocol = normalizeAndValidateChainProtocol(outNode);
                if (protocol == null) return R.err("隧道协议不支持: " + outNode.getProtocol());
                String strategy = normalizeAndValidateChainStrategy(outNode.getStrategy());
                if (strategy == null) return R.err("负载策略不支持: " + outNode.getStrategy());
                if (!isValidFlowQuota(outNode.getFlowQuotaGb())) return R.err("出口流量配额不能小于0");
                if (!isValidSpeedLimit(outNode.getSpeedLimitMbps())) return R.err("出口限速不能小于0");

                allNodeIds.add(outNode.getNodeId());

                ChainTunnel exitNode = new ChainTunnel();
                exitNode.setChainType(3);
                exitNode.setNodeId(outNode.getNodeId());
                exitNode.setProtocol(protocol);
                exitNode.setStrategy(strategy);
                exitNode.setFlowQuotaGb(normalizeNullableLong(outNode.getFlowQuotaGb()));
                exitNode.setSpeedLimitMbps(normalizeNullableInteger(outNode.getSpeedLimitMbps()));
                exitNode.setInFlow(0L);
                exitNode.setOutFlow(0L);
                exitNode.setHealthStatus(1);
                updatedChainTunnels.add(exitNode);
            }
        }

        Set<Long> uniqueNodeIds = new HashSet<>(allNodeIds);
        if (uniqueNodeIds.size() != allNodeIds.size()) return R.err("节点重复");

        List<Node> nodeList = nodeService.list(new QueryWrapper<Node>().in("id", uniqueNodeIds));
        if (nodeList.size() != uniqueNodeIds.size()) return R.err("部分节点不存在");
        for (Node node : nodeList) {
            if (node.getStatus() != 1) return R.err("部分节点不在线");
            nodes.put(node.getId(), node);
        }

        List<ChainTunnel> oldChainTunnels = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", existingTunnel.getId()));
        Set<Long> affectedNodeIds = oldChainTunnels.stream()
                .map(ChainTunnel::getNodeId)
                .filter(Objects::nonNull)
                .collect(Collectors.toSet());
        affectedNodeIds.addAll(uniqueNodeIds);
        Map<Long, String> affectedNodeNameMap = nodeService.list(new QueryWrapper<Node>().in("id", affectedNodeIds)).stream()
            .collect(Collectors.toMap(Node::getId, Node::getName, (left, right) -> left));

        chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", existingTunnel.getId()));
        for (ChainTunnel chainTunnel : updatedChainTunnels) {
            chainTunnel.setTunnelId(existingTunnel.getId());
            if (Objects.equals(chainTunnel.getChainType(), 2) || Objects.equals(chainTunnel.getChainType(), 3)) {
                Integer nodePort = getNodePort(chainTunnel.getNodeId(), chainTunnel.getProtocol());
                chainTunnel.setPort(nodePort);
            }
        }
        if (!updatedChainTunnels.isEmpty()) {
            chainTunnelService.saveBatch(updatedChainTunnels);
        }

        Tunnel tunnel = new Tunnel();
        tunnel.setId(tunnelUpdateDto.getId());
        tunnel.setName(tunnelUpdateDto.getName());
        tunnel.setFlow(tunnelUpdateDto.getFlow());
        tunnel.setTrafficRatio(tunnelUpdateDto.getTrafficRatio());
        tunnel.setInIp(tunnelUpdateDto.getInIp());
        tunnel.setUpdatedTime(System.currentTimeMillis());

        if (StringUtils.isEmpty(tunnel.getInIp())){
            StringBuilder in_ip = new StringBuilder();
            List<ChainTunnel> chainTunnels = updatedChainTunnels.stream()
                    .filter(item -> Objects.equals(item.getChainType(), 1))
                    .toList();
            for (ChainTunnel chainTunnel : chainTunnels) {
                Node node = nodes.get(chainTunnel.getNodeId());
                if (node == null) return R.err("隧道节点数据错误，部分节点不存在");
                in_ip.append(node.getServerIp()).append(",");
            }
            in_ip.deleteCharAt(in_ip.length() - 1);
            tunnel.setInIp(in_ip.toString());
        }

        this.updateById(tunnel);
        Map<String, Object> syncResult = registerForcePullFullConfigAfterCommit(affectedNodeIds, affectedNodeNameMap);
        Map<String, Object> data = new HashMap<>();
        data.put("syncResult", syncResult);
        return R.ok(data);
    }

    private String normalizeAndValidateChainStrategy(String strategy) {
        if (StringUtils.isBlank(strategy)) {
            return "round";
        }
        String lower = strategy.trim().toLowerCase();
        if (!SUPPORTED_CHAIN_STRATEGIES.contains(lower)) {
            return null;
        }
        return lower;
    }

    private boolean isValidFlowQuota(Long value) {
        return value == null || value >= 0;
    }

    private boolean isValidSpeedLimit(Integer value) {
        return value == null || value >= 0;
    }

    private Long normalizeNullableLong(Long value) {
        if (value == null || value <= 0) {
            return null;
        }
        return value;
    }

    private Integer normalizeNullableInteger(Integer value) {
        if (value == null || value <= 0) {
            return null;
        }
        return value;
    }

    private Map<String, Object> registerForcePullFullConfigAfterCommit(Set<Long> affectedNodeIds, Map<Long, String> affectedNodeNameMap) {
        List<Map<String, Object>> nodeResults = new ArrayList<>();
        Map<String, Object> summary = buildSyncSummary(nodeResults);
        if (affectedNodeIds == null || affectedNodeIds.isEmpty()) {
            return summary;
        }

        Set<Long> nodeIds = new HashSet<>(affectedNodeIds);
        Runnable syncRunner = () -> {
            nodeResults.clear();
            nodeResults.addAll(forcePullFullConfig(nodeIds, affectedNodeNameMap));
            summary.putAll(buildSyncSummary(nodeResults));
        };

        if (TransactionSynchronizationManager.isActualTransactionActive()) {
            TransactionSynchronizationManager.registerSynchronization(new TransactionSynchronization() {
                @Override
                public void afterCommit() {
                    syncRunner.run();
                }
            });
        } else {
            syncRunner.run();
        }
        return summary;
    }

    private List<Map<String, Object>> forcePullFullConfig(Set<Long> nodeIds, Map<Long, String> affectedNodeNameMap) {
        List<Map<String, Object>> results = new ArrayList<>();
        for (Long nodeId : nodeIds) {
            if (nodeId == null) {
                continue;
            }
            Map<String, Object> item = new HashMap<>();
            item.put("nodeId", nodeId);
            item.put("nodeName", Optional.ofNullable(affectedNodeNameMap.get(nodeId)).orElse(String.valueOf(nodeId)));
            try {
                GostDto gostDto = GostUtil.ForcePullFullConfig(nodeId);
                String msg = gostDto == null ? "节点无响应" : gostDto.getMsg();
                item.put("success", Objects.equals(msg, "OK"));
                item.put("message", msg);
            } catch (Exception ex) {
                item.put("success", false);
                item.put("message", ex.getMessage());
            }
            results.add(item);
        }
        return results;
    }

    private Map<String, Object> buildSyncSummary(List<Map<String, Object>> nodeResults) {
        int total = nodeResults.size();
        int successCount = 0;
        for (Map<String, Object> nodeResult : nodeResults) {
            if (Boolean.TRUE.equals(nodeResult.get("success"))) {
                successCount++;
            }
        }
        Map<String, Object> summary = new HashMap<>();
        summary.put("totalCount", total);
        summary.put("successCount", successCount);
        summary.put("failureCount", total - successCount);
        summary.put("results", nodeResults);
        return summary;
    }


    @Override
    @Transactional
    public R deleteTunnel(Long id) {
        Tunnel tunnel = this.getById(id);
        if (tunnel == null) return R.err("隧道不存在");
        List<Forward> forwardList = forwardService.list(new QueryWrapper<Forward>().eq("tunnel_id", id));
        for (Forward forward : forwardList) {
            R result = forwardService.deleteForward(forward.getId());
            if (result == null || result.getCode() != 0) {
                String msg = result == null ? "删除转发失败" : result.getMsg();
                return R.err("删除隧道失败，转发清理异常: " + msg);
            }
        }
        userTunnelService.remove(new QueryWrapper<UserTunnel>().eq("tunnel_id", id));
        this.removeById(id);

        List<ChainTunnel> chainTunnels = chainTunnelService.list(new QueryWrapper<ChainTunnel>().eq("tunnel_id", id));
        for (ChainTunnel chainTunnel : chainTunnels) {
            if (chainTunnel.getChainType() == 1){ // 入口
                GostUtil.DeleteChains(chainTunnel.getNodeId(), "chains_" + chainTunnel.getTunnelId());
                GostUtil.DeleteChains(chainTunnel.getNodeId(), "chains_" + chainTunnel.getTunnelId() + "_tcp");
                GostUtil.DeleteChains(chainTunnel.getNodeId(), "chains_" + chainTunnel.getTunnelId() + "_udp");
            }
            else if (chainTunnel.getChainType() == 2){ // 链
                GostUtil.DeleteChains(chainTunnel.getNodeId(), "chains_" + chainTunnel.getTunnelId());
                GostUtil.DeleteChains(chainTunnel.getNodeId(), "chains_" + chainTunnel.getTunnelId() + "_tcp");
                GostUtil.DeleteChains(chainTunnel.getNodeId(), "chains_" + chainTunnel.getTunnelId() + "_udp");
                JSONArray services = new JSONArray();
                for (String trafficProtocol : resolveTrafficProtocols(List.of(chainTunnel))) {
                    services.add(GostUtil.buildChainServiceName(chainTunnel.getTunnelId(), chainTunnel.getProtocol(), trafficProtocol));
                }
                GostUtil.DeleteService(chainTunnel.getNodeId(), services);
            }
            else { // 出口
                JSONArray services = new JSONArray();
                for (String trafficProtocol : resolveTrafficProtocols(List.of(chainTunnel))) {
                    services.add(GostUtil.buildChainServiceName(chainTunnel.getTunnelId(), chainTunnel.getProtocol(), trafficProtocol));
                }
                GostUtil.DeleteService(chainTunnel.getNodeId(), services);
            }
        }
        chainTunnelService.remove(new QueryWrapper<ChainTunnel>().eq("tunnel_id", id));
        return R.ok();
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

    private String normalizeAndValidateChainProtocol(ChainTunnel chainTunnel) {
        String source = chainTunnel.getProtocol();
        if (StringUtils.isNotBlank(source)) {
            String lower = source.trim().toLowerCase();
            if (!SUPPORTED_CHAIN_PROTOCOLS.contains(lower)) {
                return null;
            }
        }
        return GostUtil.normalizeChainProtocol(source);
    }


    @Override
    public R userTunnel() {
        List<Tunnel> tunnelEntities;
        Integer roleId = JwtUtil.getRoleIdFromToken();
        Integer userId = JwtUtil.getUserIdFromToken();
        if (roleId == 0) {
            tunnelEntities = this.list(new QueryWrapper<Tunnel>().eq("status", 1));
        } else {
            tunnelEntities = java.util.Collections.emptyList(); // 返回空列表
            List<UserTunnel> userTunnels = userTunnelMapper.selectList(
                    new QueryWrapper<UserTunnel>().eq("user_id", userId)
            );
            if (!userTunnels.isEmpty()) {
                List<Integer> tunnelIds = userTunnels.stream()
                        .map(UserTunnel::getTunnelId)
                        .collect(Collectors.toList());
                tunnelEntities = this.list(new QueryWrapper<Tunnel>()
                        .in("id", tunnelIds)
                        .eq("status", 1));
            }

        }
        return R.ok(tunnelEntities);
    }


    @Override
    public R diagnoseTunnel(Long tunnelId) {
        Tunnel tunnel = this.getById(tunnelId);
        if (tunnel == null) {
            return R.err("隧道不存在");
        }

        List<ChainTunnel> chainTunnels = chainTunnelService.list(
                new QueryWrapper<ChainTunnel>().eq("tunnel_id", tunnelId)
        );

        if (chainTunnels.isEmpty()) {
            return R.err("隧道配置不完整");
        }

        List<ChainTunnel> inNodes = chainTunnels.stream()
                .filter(ct -> ct.getChainType() == 1)
                .toList();

        Map<Integer, List<ChainTunnel>> chainNodesMap = chainTunnels.stream()
                .filter(ct -> ct.getChainType() == 2)
                .collect(Collectors.groupingBy(
                        ct -> ct.getInx() != null ? ct.getInx() : 0,
                        Collectors.toList()
                ));

        List<List<ChainTunnel>> chainNodesList = chainNodesMap.entrySet().stream()
                .sorted(Map.Entry.comparingByKey())
                .map(Map.Entry::getValue)
                .toList();

        List<ChainTunnel> outNodes = chainTunnels.stream()
                .filter(ct -> ct.getChainType() == 3)
                .toList();

        List<DiagnosisResult> results = new ArrayList<>();

        if (tunnel.getType() == 1) {
            for (ChainTunnel inNode : inNodes) {
                Node node = nodeService.getById(inNode.getNodeId());
                if (node != null) {
                    DiagnosisResult result = performTcpPingDiagnosisWithConnectionCheck(
                            node, "www.google.com", 443, "入口(" + node.getName() + ")->外网"
                    );
                    result.setFromChainType(1); // 入口
                    results.add(result);
                }
            }
        } else if (tunnel.getType() == 2) {
            for (ChainTunnel inNode : inNodes) {
                Node fromNode = nodeService.getById(inNode.getNodeId());
                
                if (fromNode != null) {
                    if (!chainNodesList.isEmpty()) {
                        for (ChainTunnel firstChainNode : chainNodesList.getFirst()) {
                            Node toNode = nodeService.getById(firstChainNode.getNodeId());
                            if (toNode != null) {
                                addEntryDiagnosisResults(results, fromNode, toNode, firstChainNode, 2, firstChainNode.getInx());
                            }
                        }
                    } else if (!outNodes.isEmpty()) {
                        for (ChainTunnel outNode : outNodes) {
                            Node toNode = nodeService.getById(outNode.getNodeId());
                            if (toNode != null) {
                                addEntryDiagnosisResults(results, fromNode, toNode, outNode, 3, null);
                            }
                        }
                    }
                }
            }

            for (int i = 0; i < chainNodesList.size(); i++) {
                List<ChainTunnel> currentHop = chainNodesList.get(i);
                
                for (ChainTunnel currentNode : currentHop) {
                    Node fromNode = nodeService.getById(currentNode.getNodeId());
                    
                    if (fromNode != null) {
                        if (i + 1 < chainNodesList.size()) {
                            for (ChainTunnel nextNode : chainNodesList.get(i + 1)) {
                                Node toNode = nodeService.getById(nextNode.getNodeId());
                                if (toNode != null) {
                                    addHopDiagnosisResults(
                                            results,
                                            fromNode,
                                            toNode,
                                            nextNode,
                                            2,
                                            currentNode.getInx(),
                                            2,
                                            nextNode.getInx(),
                                            "第" + (i + 1) + "跳(" + fromNode.getName() + ")->第" + (i + 2) + "跳(" + toNode.getName() + ")"
                                    );
                                }
                            }
                        } else if (!outNodes.isEmpty()) {
                            for (ChainTunnel outNode : outNodes) {
                                Node toNode = nodeService.getById(outNode.getNodeId());
                                if (toNode != null) {
                                    addHopDiagnosisResults(
                                            results,
                                            fromNode,
                                            toNode,
                                            outNode,
                                            2,
                                            currentNode.getInx(),
                                            3,
                                            null,
                                            "第" + (i + 1) + "跳(" + fromNode.getName() + ")->出口(" + toNode.getName() + ")"
                                    );
                                }
                            }
                        }
                    }
                }
            }
            for (ChainTunnel outNode : outNodes) {
                Node node = nodeService.getById(outNode.getNodeId());
                if (node != null) {
                    DiagnosisResult result = performTcpPingDiagnosisWithConnectionCheck(
                            node, "www.google.com", 443, "出口(" + node.getName() + ")->外网"
                    );
                    result.setFromChainType(3);
                    results.add(result);
                }
            }
        }

        Map<String, Object> diagnosisReport = new HashMap<>();
        diagnosisReport.put("tunnelId", tunnelId);
        diagnosisReport.put("tunnelName", tunnel.getName());
        diagnosisReport.put("tunnelType", tunnel.getType() == 1 ? "端口转发" : "隧道转发");
        diagnosisReport.put("results", results);
        diagnosisReport.put("timestamp", System.currentTimeMillis());

        return R.ok(diagnosisReport);
    }

    public Integer getNodePort(Long nodeId) {
        return getNodePort(nodeId, GostUtil.PROTOCOL_TCP);
    }

    public Integer getNodePort(Long nodeId, String protocol) {

        Node node = nodeService.getById(nodeId);
        if (node == null){
            throw new RuntimeException("节点不存在");
        }

        // 1. 查询隧道转发链占用的端口
        List<ChainTunnel> chainTunnels = chainTunnelService.list(
                new QueryWrapper<ChainTunnel>().eq("node_id", nodeId)
        );
        Set<Integer> usedPorts = chainTunnels.stream()
                .map(ChainTunnel::getPort)
                .filter(Objects::nonNull)
                .collect(Collectors.toSet());
        for (ChainTunnel chainTunnel : chainTunnels) {
            if (chainTunnel.getPort() == null) {
                continue;
            }
            if (GostUtil.isHybridUdpProtocol(chainTunnel.getProtocol())) {
                Integer udpPort = GostUtil.resolveChainListenPort(chainTunnel.getPort(), chainTunnel.getProtocol(), GostUtil.PROTOCOL_UDP);
                if (udpPort != null) {
                    usedPorts.add(udpPort);
                }
            }
        }


        List<ForwardPort> list = forwardPortService.list(new QueryWrapper<ForwardPort>().eq("node_id", nodeId));
        Set<Integer> forwardUsedPorts = new HashSet<>();
        for (ForwardPort forwardPort : list) {
            forwardUsedPorts.add(forwardPort.getPort());
        }
        usedPorts.addAll(forwardUsedPorts);

        // 3. 从可用端口范围中筛选未被占用的端口
        List<Integer> parsedPorts = parsePorts(node.getPort());
        List<Integer> availablePorts = parsedPorts.stream()
                .filter(p -> !usedPorts.contains(p))
                .toList();

        if (availablePorts.isEmpty()) {
            throw new RuntimeException("节点端口已满，无可用端口");
        }

        String normalizedProtocol = GostUtil.normalizeChainProtocol(protocol);
        if (GostUtil.isHybridUdpProtocol(normalizedProtocol)) {
            for (Integer candidate : availablePorts) {
                Integer udpPort = GostUtil.resolveChainListenPort(candidate, normalizedProtocol, GostUtil.PROTOCOL_UDP);
                if (udpPort == null || Objects.equals(udpPort, candidate)) {
                    continue;
                }
                if (parsedPorts.contains(udpPort) && !usedPorts.contains(udpPort)) {
                    return candidate;
                }
            }
        }
        return availablePorts.getFirst();
    }

    private void addEntryDiagnosisResults(List<DiagnosisResult> results,
                                          Node fromNode,
                                          Node toNode,
                                          ChainTunnel targetTunnel,
                                          int toChainType,
                                          Integer toInx) {
        if (targetTunnel.getPort() == null) {
            return;
        }

        String baseDesc = "入口(" + fromNode.getName() + ")->" +
                (toChainType == 3 ? "出口(" + toNode.getName() + ")" : "第1跳(" + toNode.getName() + ")");

        addHopDiagnosisResults(results, fromNode, toNode, targetTunnel, 1, null, toChainType, toInx, baseDesc);
    }

    private void addHopDiagnosisResults(List<DiagnosisResult> results,
                                        Node fromNode,
                                        Node toNode,
                                        ChainTunnel targetTunnel,
                                        int fromChainType,
                                        Integer fromInx,
                                        int toChainType,
                                        Integer toInx,
                                        String baseDesc) {
        if (targetTunnel.getPort() == null) {
            return;
        }

        String protocol = GostUtil.normalizeChainProtocol(targetTunnel.getProtocol());
        if (GostUtil.isHybridUdpProtocol(protocol)) {
            Integer udpPort = GostUtil.resolveChainListenPort(targetTunnel.getPort(), protocol, GostUtil.PROTOCOL_UDP);
            if (udpPort == null) {
                return;
            }
            DiagnosisResult udpResult = performTransportPingDiagnosisWithConnectionCheck(
                    fromNode,
                    toNode.getServerIp(),
                    udpPort,
                    baseDesc + " [UDPPing]",
                    "UdpPing",
                    "UDP连接成功"
            );
                    udpResult.setFromChainType(fromChainType);
                    udpResult.setFromInx(fromInx);
            udpResult.setToChainType(toChainType);
            udpResult.setToInx(toInx);
            results.add(udpResult);

            String commandType = Objects.equals(protocol, GostUtil.PROTOCOL_UDP_QUIC) ? "QuicPing" : "KcpPing";
            String successMsg = Objects.equals(protocol, GostUtil.PROTOCOL_UDP_QUIC) ? "QUIC连接成功" : "KCP连接成功";
            DiagnosisResult mixedResult = performTransportPingDiagnosisWithConnectionCheck(
                    fromNode,
                    toNode.getServerIp(),
                    targetTunnel.getPort(),
                    baseDesc + (Objects.equals(protocol, GostUtil.PROTOCOL_UDP_QUIC) ? " [QUICPing]" : " [KCPPing]"),
                    commandType,
                    successMsg
            );
            mixedResult.setFromChainType(fromChainType);
            mixedResult.setFromInx(fromInx);
            mixedResult.setToChainType(toChainType);
            mixedResult.setToInx(toInx);
            results.add(mixedResult);
            return;
        }

        DiagnosisResult result = performTcpPingDiagnosisWithConnectionCheck(
                fromNode, toNode.getServerIp(), targetTunnel.getPort(), baseDesc
        );
        result.setFromChainType(fromChainType);
        result.setFromInx(fromInx);
        result.setToChainType(toChainType);
        result.setToInx(toInx);
        results.add(result);
    }

    private DiagnosisResult performTransportPingDiagnosisWithConnectionCheck(Node node,
                                                                             String targetIp,
                                                                             int port,
                                                                             String description,
                                                                             String commandType,
                                                                             String successMessage) {
        DiagnosisResult result = new DiagnosisResult();
        result.setNodeId(node.getId());
        result.setNodeName(node.getName());
        result.setTargetIp(targetIp);
        result.setTargetPort(port);
        result.setDescription(description);
        result.setTimestamp(System.currentTimeMillis());

        try {
            JSONObject pingData = new JSONObject();
            pingData.put("ip", targetIp);
            pingData.put("port", port);
            pingData.put("count", 4);
            pingData.put("timeout", 5000);

            GostDto gostResult = WebSocketServer.send_msg(node.getId(), pingData, commandType);
            if (gostResult != null && "OK".equals(gostResult.getMsg())) {
                if (gostResult.getData() != null) {
                    JSONObject pingResponse = (JSONObject) gostResult.getData();
                    boolean success = pingResponse.getBooleanValue("success");
                    result.setSuccess(success);
                    if (success) {
                        result.setMessage(successMessage);
                        result.setAverageTime(pingResponse.getDoubleValue("averageTime"));
                        result.setPacketLoss(pingResponse.getDoubleValue("packetLoss"));
                    } else {
                        result.setMessage(pingResponse.getString("errorMessage"));
                        result.setAverageTime(-1.0);
                        result.setPacketLoss(100.0);
                    }
                } else {
                    result.setSuccess(true);
                    result.setMessage(successMessage);
                    result.setAverageTime(0.0);
                    result.setPacketLoss(0.0);
                }
                return result;
            }

            result.setSuccess(false);
            result.setMessage(gostResult != null ? gostResult.getMsg() : "节点无响应");
            result.setAverageTime(-1.0);
            result.setPacketLoss(100.0);
            return result;
        } catch (Exception e) {
            result.setSuccess(false);
            result.setMessage("连接检查异常: " + e.getMessage());
            result.setAverageTime(-1.0);
            result.setPacketLoss(100.0);
            return result;
        }
    }

    public static List<Integer> parsePorts(String input) {
        Set<Integer> set = new HashSet<>();
        String[] parts = input.split(",");
        for (String part : parts) {
            part = part.trim();
            if (part.contains("-")) {
                String[] range = part.split("-");
                int start = Integer.parseInt(range[0]);
                int end = Integer.parseInt(range[1]);
                for (int i = start; i <= end; i++) {
                    set.add(i);
                }
            } else {
                set.add(Integer.parseInt(part));
            }
        }
        return set.stream().sorted().collect(Collectors.toList());
    }

    private void isError(GostDto gostDto){

    }

    private DiagnosisResult performTcpPingDiagnosis(Node node, String targetIp, int port, String description) {
        try {
            // 构建TCP ping请求数据
            JSONObject tcpPingData = new JSONObject();
            tcpPingData.put("ip", targetIp);
            tcpPingData.put("port", port);
            tcpPingData.put("count", 4);
            tcpPingData.put("timeout", 5000); // 5秒超时

            // 发送TCP ping命令到节点
            GostDto gostResult = WebSocketServer.send_msg(node.getId(), tcpPingData, "TcpPing");

            DiagnosisResult result = new DiagnosisResult();
            result.setNodeId(node.getId());
            result.setNodeName(node.getName());
            result.setTargetIp(targetIp);
            result.setTargetPort(port);
            result.setDescription(description);
            result.setTimestamp(System.currentTimeMillis());

            if (gostResult != null && "OK".equals(gostResult.getMsg())) {
                // 尝试解析TCP ping响应数据
                try {
                    if (gostResult.getData() != null) {
                        JSONObject tcpPingResponse = (JSONObject) gostResult.getData();
                        boolean success = tcpPingResponse.getBooleanValue("success");

                        result.setSuccess(success);
                        if (success) {
                            result.setMessage("TCP连接成功");
                            result.setAverageTime(tcpPingResponse.getDoubleValue("averageTime"));
                            result.setPacketLoss(tcpPingResponse.getDoubleValue("packetLoss"));
                        } else {
                            result.setMessage(tcpPingResponse.getString("errorMessage"));
                            result.setAverageTime(-1.0);
                            result.setPacketLoss(100.0);
                        }
                    } else {
                        // 没有详细数据，使用默认值
                        result.setSuccess(true);
                        result.setMessage("TCP连接成功");
                        result.setAverageTime(0.0);
                        result.setPacketLoss(0.0);
                    }
                } catch (Exception e) {
                    // 解析响应数据失败，但TCP ping命令本身成功了
                    result.setSuccess(true);
                    result.setMessage("TCP连接成功，但无法解析详细数据");
                    result.setAverageTime(0.0);
                    result.setPacketLoss(0.0);
                }
            } else {
                result.setSuccess(false);
                result.setMessage(gostResult != null ? gostResult.getMsg() : "节点无响应");
                result.setAverageTime(-1.0);
                result.setPacketLoss(100.0);
            }

            return result;
        } catch (Exception e) {
            DiagnosisResult result = new DiagnosisResult();
            result.setNodeId(node.getId());
            result.setNodeName(node.getName());
            result.setTargetIp(targetIp);
            result.setTargetPort(port);
            result.setDescription(description);
            result.setSuccess(false);
            result.setMessage("诊断执行异常: " + e.getMessage());
            result.setTimestamp(System.currentTimeMillis());
            result.setAverageTime(-1.0);
            result.setPacketLoss(100.0);
            return result;
        }
    }

    private DiagnosisResult performTcpPingDiagnosisWithConnectionCheck(Node node, String targetIp, int port, String description) {
        DiagnosisResult result = new DiagnosisResult();
        result.setNodeId(node.getId());
        result.setNodeName(node.getName());
        result.setTargetIp(targetIp);
        result.setTargetPort(port);
        result.setDescription(description);
        result.setTimestamp(System.currentTimeMillis());

        try {
            return performTcpPingDiagnosis(node, targetIp, port, description);
        } catch (Exception e) {
            result.setSuccess(false);
            result.setMessage("连接检查异常: " + e.getMessage());
            result.setAverageTime(-1.0);
            result.setPacketLoss(100.0);
            return result;
        }
    }


}
