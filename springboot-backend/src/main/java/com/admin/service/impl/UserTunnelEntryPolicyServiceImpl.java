package com.admin.service.impl;

import com.admin.entity.ChainTunnel;
import com.admin.entity.Forward;
import com.admin.entity.ForwardPort;
import com.admin.entity.UserTunnel;
import com.admin.entity.UserTunnelEntryPolicy;
import com.admin.mapper.UserTunnelEntryPolicyMapper;
import com.admin.service.ChainTunnelService;
import com.admin.service.ForwardPortService;
import com.admin.service.ForwardService;
import com.admin.service.UserTunnelEntryPolicyService;
import com.admin.service.UserTunnelService;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.springframework.stereotype.Service;

import javax.annotation.Resource;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.LinkedHashSet;
import java.util.List;
import java.util.Map;
import java.util.Set;

@Service
public class UserTunnelEntryPolicyServiceImpl extends ServiceImpl<UserTunnelEntryPolicyMapper, UserTunnelEntryPolicy>
        implements UserTunnelEntryPolicyService {

    @Resource
    private UserTunnelService userTunnelService;

    @Resource
    private ChainTunnelService chainTunnelService;

    @Resource
    private ForwardService forwardService;

    @Resource
    private ForwardPortService forwardPortService;

    @Override
    public List<UserTunnelEntryPolicy> syncAndListByUserTunnelId(Integer userTunnelId) {
        syncPolicies(userTunnelId);
        return listByUserTunnelId(userTunnelId);
    }

    @Override
    public List<UserTunnelEntryPolicy> listByUserTunnelId(Integer userTunnelId) {
        if (userTunnelId == null) {
            return List.of();
        }
        return this.list(new QueryWrapper<UserTunnelEntryPolicy>()
                .eq("user_tunnel_id", userTunnelId)
                .orderByAsc("id"));
    }

    @Override
    public void addUsedFlow(Long id, long bytes) {
        if (id == null || bytes <= 0) {
            return;
        }
        UpdateWrapper<UserTunnelEntryPolicy> updateWrapper = new UpdateWrapper<>();
        updateWrapper.eq("id", id)
                .setSql("used_flow = used_flow + " + bytes + ", updated_time = " + System.currentTimeMillis());
        this.update(null, updateWrapper);
    }

    private void syncPolicies(Integer userTunnelId) {
        if (userTunnelId == null) {
            return;
        }
        UserTunnel userTunnel = userTunnelService.getById(userTunnelId);
        if (userTunnel == null || userTunnel.getTunnelId() == null || userTunnel.getUserId() == null) {
            return;
        }

        Set<Long> entryNodeIds = new LinkedHashSet<>();
        List<ChainTunnel> chainEntries = chainTunnelService.list(new QueryWrapper<ChainTunnel>()
                .eq("tunnel_id", userTunnel.getTunnelId())
                .eq("chain_type", 1));
        for (ChainTunnel entry : chainEntries) {
            if (entry.getNodeId() != null) {
                entryNodeIds.add(entry.getNodeId());
            }
        }

        List<Forward> forwards = forwardService.list(new QueryWrapper<Forward>()
                .eq("user_id", userTunnel.getUserId())
                .eq("tunnel_id", userTunnel.getTunnelId()));
        if (!forwards.isEmpty()) {
            List<Integer> forwardIds = forwards.stream().map(Forward::getId).toList();
            List<ForwardPort> ports = forwardPortService.list(new QueryWrapper<ForwardPort>()
                    .in("forward_id", forwardIds));
            for (ForwardPort port : ports) {
                if (port.getNodeId() != null) {
                    entryNodeIds.add(port.getNodeId());
                }
            }
        }

        if (entryNodeIds.isEmpty()) {
            this.remove(new QueryWrapper<UserTunnelEntryPolicy>().eq("user_tunnel_id", userTunnelId));
            return;
        }

        List<UserTunnelEntryPolicy> currentPolicies = this.list(new QueryWrapper<UserTunnelEntryPolicy>()
                .eq("user_tunnel_id", userTunnelId));

        Map<Long, UserTunnelEntryPolicy> currentByNode = new HashMap<>();
        for (UserTunnelEntryPolicy policy : currentPolicies) {
            if (policy.getEntryNodeId() != null) {
                currentByNode.put(policy.getEntryNodeId(), policy);
            }
        }

        long now = System.currentTimeMillis();
        List<UserTunnelEntryPolicy> toCreate = new ArrayList<>();
        for (Long entryNodeId : entryNodeIds) {
            UserTunnelEntryPolicy existing = currentByNode.remove(entryNodeId);
            if (existing != null) {
                continue;
            }
            UserTunnelEntryPolicy policy = new UserTunnelEntryPolicy();
            policy.setUserTunnelId(userTunnelId);
            policy.setTunnelId(userTunnel.getTunnelId());
            policy.setEntryNodeId(entryNodeId);
            policy.setSpeedLimitMbps(null);
            policy.setFlowQuotaGb(null);
            policy.setUsedFlow(0L);
            policy.setStatus(1);
            policy.setCreatedTime(now);
            policy.setUpdatedTime(now);
            toCreate.add(policy);
        }

        if (!toCreate.isEmpty()) {
            this.saveBatch(toCreate);
        }

        for (UserTunnelEntryPolicy stale : currentByNode.values()) {
            if (stale.getId() != null) {
                this.removeById(stale.getId());
            }
        }
    }
}
