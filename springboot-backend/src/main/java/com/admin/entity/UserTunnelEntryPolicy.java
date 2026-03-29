package com.admin.entity;

import com.baomidou.mybatisplus.annotation.IdType;
import com.baomidou.mybatisplus.annotation.TableId;
import lombok.Data;

import java.io.Serializable;

@Data
public class UserTunnelEntryPolicy implements Serializable {

    private static final long serialVersionUID = 1L;

    @TableId(value = "id", type = IdType.AUTO)
    private Long id;

    private Integer userTunnelId;

    private Integer tunnelId;

    private Long entryNodeId;

    private Integer speedLimitMbps;

    private Long flowQuotaGb;

    private Long usedFlow;

    private Integer status;

    private Long createdTime;

    private Long updatedTime;
}
