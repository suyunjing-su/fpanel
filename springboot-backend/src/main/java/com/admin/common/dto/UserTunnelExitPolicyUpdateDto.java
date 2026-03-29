package com.admin.common.dto;

import lombok.Data;

import javax.validation.constraints.Min;
import javax.validation.constraints.NotNull;

@Data
public class UserTunnelExitPolicyUpdateDto {

    @NotNull(message = "策略ID不能为空")
    private Long id;

    @Min(value = 0, message = "出口流量配额不能小于0")
    private Long flowQuotaGb;

    @NotNull(message = "状态不能为空")
    private Integer status;
}
