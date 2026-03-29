package com.admin.controller;

import cloud.tianai.captcha.application.ImageCaptchaApplication;
import cloud.tianai.captcha.common.constant.CaptchaTypeConstant;
import cloud.tianai.captcha.common.response.ApiResponse;
import cloud.tianai.captcha.application.vo.ImageCaptchaVO;
import com.admin.common.dto.CaptchaVerifyDto;
import com.admin.common.lang.R;
import com.admin.entity.ViteConfig;
import com.baomidou.mybatisplus.core.conditions.query.QueryWrapper;
import lombok.extern.slf4j.Slf4j;
import org.springframework.web.bind.annotation.*;
import cloud.tianai.captcha.application.vo.CaptchaResponse;

import javax.annotation.Resource;
import javax.validation.Valid;
import java.util.Collections;
import java.util.HashMap;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;
import java.util.concurrent.ThreadLocalRandom;

/**
 * 验证码控制器
 */
@Slf4j
@RestController
@CrossOrigin
@RequestMapping("/api/v1/captcha")
public class CaptchaController extends BaseController {

    @Resource
     ImageCaptchaApplication application;

    private static final String[] OPTIONS = {
            "SLIDER", "WORD_IMAGE_CLICK", "ROTATE", "CONCAT"
    };

    @PostMapping("/check")
    public R check() {
        if (!isCaptchaEnabled()) return R.ok(0);
        return R.ok(1);
    }

    @PostMapping("/runtime")
    public R runtime() {
        Map<String, Object> runtime = new HashMap<>();
        boolean enabled = isCaptchaEnabled();
        String provider = getConfigValueOrDefault("captcha_provider", "native").toLowerCase(Locale.ROOT);
        runtime.put("enabled", enabled);
        runtime.put("provider", provider);
        runtime.put("nativeType", getConfigValueOrDefault("captcha_type", "RANDOM"));
        runtime.put("geetestCaptchaId", getConfigValueOrDefault("captcha_geetest_id", ""));
        runtime.put("geetestKeyConfigured", hasConfigValue("captcha_geetest_key"));
        runtime.put("recaptchaSiteKey", getConfigValueOrDefault("captcha_recaptcha_site_key", ""));
        runtime.put("recaptchaSecretKeyConfigured", hasConfigValue("captcha_recaptcha_secret_key"));
        runtime.put("hcaptchaSiteKey", getConfigValueOrDefault("captcha_hcaptcha_site_key", ""));
        runtime.put("hcaptchaSecretKeyConfigured", hasConfigValue("captcha_hcaptcha_secret_key"));
        return R.ok(runtime);
    }


    @PostMapping("/generate")
    public CaptchaResponse<ImageCaptchaVO> genCaptcha() {
        String provider = getConfigValueOrDefault("captcha_provider", "native").toLowerCase(Locale.ROOT);
        if (!Objects.equals(provider, "native")) {
            throw new IllegalStateException("当前验证码提供商不支持原生验证码生成");
        }

        ViteConfig viteConfig = viteConfigService.getOne(new QueryWrapper<ViteConfig>().eq("name", "captcha_type"));
        String captchaType;
        if (viteConfig == null || Objects.equals(viteConfig.getValue(), "RANDOM")) {
            captchaType = getRandomOption();
        }else {
            captchaType = viteConfig.getValue();
        }
        return application.generateCaptcha(captchaType);
    }


    @PostMapping("/verify")
    public ApiResponse<?> verify(@Valid @RequestBody CaptchaVerifyDto verifyDto) {
        ApiResponse<?> response = application.matching(verifyDto.getId(), verifyDto.getData());
        if (response.isSuccess()) {
            return ApiResponse.ofSuccess(Collections.singletonMap("validToken", verifyDto.getId()));
        }
        return response;
    }

    public static String getRandomOption() {
        int index = ThreadLocalRandom.current().nextInt(OPTIONS.length);
        return OPTIONS[index];
    }

    private boolean isCaptchaEnabled() {
        ViteConfig viteConfig = viteConfigService.getOne(new QueryWrapper<ViteConfig>().eq("name", "captcha_enabled"));
        return viteConfig != null && Objects.equals(viteConfig.getValue(), "true");
    }

    private String getConfigValueOrDefault(String key, String defaultValue) {
        String value = getConfigValue(key);
        return value == null ? defaultValue : value;
    }

    private String getConfigValue(String key) {
        ViteConfig config = viteConfigService.getOne(new QueryWrapper<ViteConfig>().eq("name", key));
        if (config == null) {
            return null;
        }
        String value = config.getValue();
        if (value == null || value.trim().isEmpty()) {
            return null;
        }
        return value.trim();
    }

    private boolean hasConfigValue(String key) {
        return getConfigValue(key) != null;
    }
}
