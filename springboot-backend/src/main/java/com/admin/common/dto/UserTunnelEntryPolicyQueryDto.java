package com.admin.common.dto;

import lombok.Data;

import javax.validation.constraints.NotNull;

@Data
public class UserTunnelEntryPolicyQueryDto {

    @NotNull(message = "用户隧道权限ID不能为空")
    private Integer userTunnelId;
}
