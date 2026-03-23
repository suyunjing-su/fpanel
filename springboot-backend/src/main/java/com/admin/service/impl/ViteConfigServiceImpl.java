package com.admin.service.impl;

import com.admin.entity.ViteConfig;
import com.admin.mapper.ViteConfigMapper;
import com.admin.service.ViteConfigService;
import com.admin.common.lang.R;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import com.baomidou.mybatisplus.extension.service.impl.ServiceImpl;
import org.springframework.stereotype.Service;
import org.springframework.util.StringUtils;

import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;

/**
 * <p>
 *  网站配置服务实现类
 * </p>
 *
 * @author QAQ
 * @since 2025-07-24
 */
@Service
public class ViteConfigServiceImpl extends ServiceImpl<ViteConfigMapper, ViteConfig> implements ViteConfigService {

    private static final Set<String> PRIVATE_CONFIG_KEYS = Set.of(
            "captcha_geetest_key",
            "captcha_recaptcha_secret_key",
            "captcha_hcaptcha_secret_key"
    );


    @Override
    public R getConfigs() {
        List<ViteConfig> configList = this.list();
        Map<String, String> configMap = new HashMap<>();
        
        for (ViteConfig config : configList) {
            if (PRIVATE_CONFIG_KEYS.contains(config.getName())) {
                continue;
            }
            configMap.put(config.getName(), config.getValue());
        }
        
        return R.ok(configMap);
    }


    @Override
    public R getConfigByName(String name) {
        if (!StringUtils.hasText(name)) return R.err("配置名称不能为空");
        if (PRIVATE_CONFIG_KEYS.contains(name)) return R.err("敏感配置不允许公开读取");

        QueryWrapper<ViteConfig> queryWrapper = new QueryWrapper<>();
        queryWrapper.eq("name", name);
        ViteConfig config = this.getOne(queryWrapper);

        if (config == null) return R.err("配置不存在");

        return R.ok(config);
    }


    @Override
    public R updateConfigs(Map<String, String> configMap) {
        if (configMap == null || configMap.isEmpty()) return R.err("配置数据不能为空");

        for (Map.Entry<String, String> entry : configMap.entrySet()) {
            String name = entry.getKey();
            String value = entry.getValue();

            if (!StringUtils.hasText(name)) {
                continue;
            }

            updateOrCreateConfig(name, value);
        }
        return R.ok();
    }


    @Override
    public R updateConfig(String name, String value) {
        if (!StringUtils.hasText(name)) return R.err("配置名称不能为空");
        if (!StringUtils.hasText(value)) return R.err("配置值不能为空");
        updateOrCreateConfig(name, value);
        return R.ok();
    }


    private void updateOrCreateConfig(String name, String value) {
        QueryWrapper<ViteConfig> queryWrapper = new QueryWrapper<>();
        queryWrapper.eq("name", name);
        ViteConfig existingConfig = this.getOne(queryWrapper);

        if (existingConfig != null) {
            // 更新现有配置
            existingConfig.setValue(value);
            existingConfig.setTime(System.currentTimeMillis());
            this.updateById(existingConfig);
        } else {
            // 创建新配置
            ViteConfig newConfig = new ViteConfig();
            newConfig.setName(name);
            newConfig.setValue(value);
            newConfig.setTime(System.currentTimeMillis());
            this.save(newConfig);
        }
    }

}
