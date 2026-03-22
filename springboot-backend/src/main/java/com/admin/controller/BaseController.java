package com.admin.controller;

import com.admin.common.lang.R;
import com.admin.service.*;
import org.springframework.beans.factory.annotation.Autowired;

import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.function.Function;

public class BaseController {

    @Autowired
    UserService userService;

    @Autowired
    NodeService nodeService;

    @Autowired
    UserTunnelService userTunnelService;

    @Autowired
    TunnelService tunnelService;

    @Autowired
    ForwardService forwardService;

    @Autowired
    ViteConfigService viteConfigService;

    protected R executeBatchDelete(List<Long> ids,
                                   Function<Long, R> deleteAction,
                                   Function<Long, String> nameResolver) {
        if (ids == null || ids.isEmpty()) {
            return R.err("ids不能为空");
        }

        List<Map<String, Object>> failures = new ArrayList<>();
        int successCount = 0;

        for (Long id : ids) {
            String name = nameResolver.apply(id);
            if (name == null || name.trim().isEmpty()) {
                name = String.valueOf(id);
            }

            try {
                R deleteResult = deleteAction.apply(id);
                if (deleteResult != null && deleteResult.getCode() == 0) {
                    successCount++;
                } else {
                    Map<String, Object> failure = new LinkedHashMap<>();
                    failure.put("id", id);
                    failure.put("name", name);
                    failure.put("reason", deleteResult != null ? deleteResult.getMsg() : "删除失败");
                    failures.add(failure);
                }
            } catch (Exception ex) {
                Map<String, Object> failure = new LinkedHashMap<>();
                failure.put("id", id);
                failure.put("name", name);
                failure.put("reason", ex.getMessage() != null ? ex.getMessage() : "删除失败");
                failures.add(failure);
            }
        }

        Map<String, Object> data = new LinkedHashMap<>();
        data.put("totalCount", ids.size());
        data.put("successCount", successCount);
        data.put("failureCount", failures.size());
        data.put("failures", failures);
        return R.ok(data);
    }

}
