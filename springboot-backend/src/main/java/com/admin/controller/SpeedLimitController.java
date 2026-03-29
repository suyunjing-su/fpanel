package com.admin.controller;

import com.admin.common.aop.LogAnnotation;
import com.admin.common.annotation.RequireRole;
import com.admin.common.dto.BatchDeleteDto;
import com.admin.common.dto.SpeedLimitDto;
import com.admin.common.dto.SpeedLimitUpdateDto;
import com.admin.common.dto.UserTunnelExitPolicyQueryDto;
import com.admin.common.dto.UserTunnelExitPolicyUpdateDto;
import com.admin.common.dto.UserTunnelQueryDto;
import com.admin.common.dto.UserTunnelUpdateDto;
import com.admin.common.lang.R;
import com.admin.service.SpeedLimitService;
import com.admin.service.TunnelService;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.validation.annotation.Validated;
import org.springframework.web.bind.annotation.*;

import java.util.Map;

/**
 * <p>
 * 限速规则前端控制器
 * </p>
 *
 * @author QAQ
 * @since 2025-06-04
 */
@RestController
@RequestMapping("/api/v1/speed-limit")
@CrossOrigin
public class SpeedLimitController extends BaseController {

    @Autowired
    private SpeedLimitService speedLimitService;

    @Autowired
    private TunnelService tunnelService;

    @LogAnnotation
    @RequireRole
    @PostMapping("/create")
    public R create(@Validated @RequestBody SpeedLimitDto speedLimitDto) {
        return speedLimitService.createSpeedLimit(speedLimitDto);
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/list")
    public R list() {
        return speedLimitService.getAllSpeedLimits();
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/update")
    public R update(@Validated @RequestBody SpeedLimitUpdateDto speedLimitUpdateDto) {
        return speedLimitService.updateSpeedLimit(speedLimitUpdateDto);
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/delete")
    public R delete(@RequestBody Map<String, Object> params) {
        Long id = Long.valueOf(params.get("id").toString());
        return speedLimitService.deleteSpeedLimit(id);
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/batch-delete")
    public R batchDelete(@Validated @RequestBody BatchDeleteDto batchDeleteDto) {
        return executeBatchDelete(
                batchDeleteDto.getIds(),
                speedLimitService::deleteSpeedLimit,
                id -> {
                    com.admin.entity.SpeedLimit speedLimit = speedLimitService.getById(id);
                    return speedLimit != null ? speedLimit.getName() : String.valueOf(id);
                }
        );
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/tunnels")
    public R getTunnels() {
        return tunnelService.getAllTunnels();
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/user-tunnel/list")
    public R getUserTunnelPolicies(@Validated @RequestBody UserTunnelQueryDto queryDto) {
        return speedLimitService.getUserTunnelPolicies(queryDto);
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/user-tunnel/update")
    public R updateUserTunnelPolicy(@Validated @RequestBody UserTunnelUpdateDto updateDto) {
        return speedLimitService.updateUserTunnelPolicy(updateDto);
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/user-tunnel/exit-policy/list")
    public R getUserTunnelExitPolicies(@Validated @RequestBody UserTunnelExitPolicyQueryDto queryDto) {
        return speedLimitService.getUserTunnelExitPolicies(queryDto);
    }

    @LogAnnotation
    @RequireRole
    @PostMapping("/user-tunnel/exit-policy/update")
    public R updateUserTunnelExitPolicy(@Validated @RequestBody UserTunnelExitPolicyUpdateDto updateDto) {
        return speedLimitService.updateUserTunnelExitPolicy(updateDto);
    }
}
