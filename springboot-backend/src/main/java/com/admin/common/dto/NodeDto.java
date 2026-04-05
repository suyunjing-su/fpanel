package com.admin.common.dto;

import lombok.Data;

import javax.validation.constraints.NotBlank;
import javax.validation.constraints.Min;

@Data
public class NodeDto {

    @NotBlank(message = "节点名称不能为空")
    private String name;

    @NotBlank(message = "服务器ip不能为空")
    private String serverIp;

    @NotBlank(message = "可用端口不能为空")
    private String port;

    private String interfaceName;

    private String tcpListenAddr = "0.0.0.0";

    private String udpListenAddr = "0.0.0.0";

    @Min(value = 1, message = "最大带宽必须大于0")
    private Integer maxBandwidthMbps;

} 