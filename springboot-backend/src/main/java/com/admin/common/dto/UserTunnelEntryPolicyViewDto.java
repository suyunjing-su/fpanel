package com.admin.common.dto;

import lombok.Data;

@Data
public class UserTunnelEntryPolicyViewDto {

    private Long id;

    private Integer userTunnelId;

    private Integer tunnelId;

    private Long entryNodeId;

    private String entryNodeName;

    private Integer speedLimitMbps;

    private Long flowQuotaGb;

    private Long usedFlow;

    private Integer status;
}
