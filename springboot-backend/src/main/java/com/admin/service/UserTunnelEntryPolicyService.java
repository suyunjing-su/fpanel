package com.admin.service;

import com.admin.entity.UserTunnelEntryPolicy;
import com.baomidou.mybatisplus.extension.service.IService;

import java.util.List;

public interface UserTunnelEntryPolicyService extends IService<UserTunnelEntryPolicy> {

    List<UserTunnelEntryPolicy> syncAndListByUserTunnelId(Integer userTunnelId);

    List<UserTunnelEntryPolicy> listByUserTunnelId(Integer userTunnelId);

    void addUsedFlow(Long id, long bytes);
}
