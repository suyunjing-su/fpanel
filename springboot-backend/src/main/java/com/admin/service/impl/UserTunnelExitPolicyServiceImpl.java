package com.admin.service.impl;

import com.admin.entity.ChainTunnel;
import com.admin.entity.UserTunnel;
import com.admin.entity.UserTunnelExitPolicy;
import com.admin.mapper.UserTunnelExitPolicyMapper;
import com.admin.service.ChainTunnelService;
import com.admin.service.UserTunnelExitPolicyService;
import com.admin.service.UserTunnelService;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.core.conditions.update.UpdateWrapper;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.springframework.stereotype.Service;

import javax.annotation.Resource;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

@Service
public class UserTunnelExitPolicyServiceImpl extends ServiceImpl<UserTunnelExitPolicyMapper, UserTunnelExitPolicy>
        implements UserTunnelExitPolicyService {

    @Resource
    private UserTunnelService userTunnelService;

    @Resource
    private ChainTunnelService chainTunnelService;

    @Override
    public List<UserTunnelExitPolicy> syncAndListByUserTunnelId(Integer userTunnelId) {
        syncPolicies(userTunnelId);
        return listByUserTunnelId(userTunnelId);
    }

    @Override
    public List<UserTunnelExitPolicy> listByUserTunnelId(Integer userTunnelId) {
        if (userTunnelId == null) {
            return List.of();
        }
        return this.list(new QueryWrapper<UserTunnelExitPolicy>()
                .eq("user_tunnel_id", userTunnelId)
                .orderByAsc("id"));
    }

    @Override
    public void addUsedFlow(Long id, long bytes) {
        if (id == null || bytes <= 0) {
            return;
        }
        UpdateWrapper<UserTunnelExitPolicy> updateWrapper = new UpdateWrapper<>();
        updateWrapper.eq("id", id)
                .setSql("used_flow = used_flow + " + bytes + ", updated_time = " + System.currentTimeMillis());
        this.update(null, updateWrapper);
    }

    private void syncPolicies(Integer userTunnelId) {
        if (userTunnelId == null) {
            return;
        }
        UserTunnel userTunnel = userTunnelService.getById(userTunnelId);
        if (userTunnel == null || userTunnel.getTunnelId() == null) {
            return;
        }

        List<ChainTunnel> exits = chainTunnelService.list(new QueryWrapper<ChainTunnel>()
                .eq("tunnel_id", userTunnel.getTunnelId())
                .eq("chain_type", 3));

        if (exits.isEmpty()) {
            this.remove(new QueryWrapper<UserTunnelExitPolicy>().eq("user_tunnel_id", userTunnelId));
            return;
        }

        List<UserTunnelExitPolicy> currentPolicies = this.list(new QueryWrapper<UserTunnelExitPolicy>()
                .eq("user_tunnel_id", userTunnelId));

        Map<Long, UserTunnelExitPolicy> currentByNode = new HashMap<>();
        for (UserTunnelExitPolicy policy : currentPolicies) {
            if (policy.getExitNodeId() != null) {
                currentByNode.put(policy.getExitNodeId(), policy);
            }
        }

        long now = System.currentTimeMillis();
        List<UserTunnelExitPolicy> toCreate = new ArrayList<>();
        for (ChainTunnel exit : exits) {
            if (exit.getNodeId() == null) {
                continue;
            }
            UserTunnelExitPolicy existing = currentByNode.remove(exit.getNodeId());
            if (existing != null) {
                continue;
            }
            UserTunnelExitPolicy policy = new UserTunnelExitPolicy();
            policy.setUserTunnelId(userTunnelId);
            policy.setTunnelId(userTunnel.getTunnelId());
            policy.setExitNodeId(exit.getNodeId());
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

        for (UserTunnelExitPolicy stale : currentByNode.values()) {
            if (stale.getId() != null) {
                this.removeById(stale.getId());
            }
        }
    }
}
