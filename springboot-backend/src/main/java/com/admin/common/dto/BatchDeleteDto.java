package com.admin.common.dto;

import lombok.Data;

import javax.validation.constraints.NotEmpty;
import java.util.List;

@Data
public class BatchDeleteDto {

    @NotEmpty(message = "ids不能为空")
    private List<Long> ids;
}
