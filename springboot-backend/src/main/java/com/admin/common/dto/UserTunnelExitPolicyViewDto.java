package com.admin.common.dto;

import lombok.Data;

@Data
public class UserTunnelExitPolicyViewDto {

    private Long id;

    private Integer userTunnelId;

    private Integer tunnelId;

    private Long exitNodeId;

    private String exitNodeName;

    private Long flowQuotaGb;

    private Long usedFlow;

    private Integer status;

    private Integer healthStatus;

    private Long lastLatencyMs;
}
