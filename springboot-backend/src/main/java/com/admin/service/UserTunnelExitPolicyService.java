package com.admin.service;

import com.admin.entity.UserTunnelExitPolicy;
import com.baomidou.mybatisplus.extension.service.IService;

import java.util.List;

public interface UserTunnelExitPolicyService extends IService<UserTunnelExitPolicy> {

    List<UserTunnelExitPolicy> syncAndListByUserTunnelId(Integer userTunnelId);

    List<UserTunnelExitPolicy> listByUserTunnelId(Integer userTunnelId);

    void addUsedFlow(Long id, long bytes);
}
