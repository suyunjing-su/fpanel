import { useState, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button } from "@heroui/button";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { Input } from "@heroui/input";
import { Spinner } from "@heroui/spinner";
import { Divider } from "@heroui/divider";
import { Switch } from "@heroui/switch";
import { Select, SelectItem } from "@heroui/select";
import toast from 'react-hot-toast';
import { updateConfigs } from '@/api';
import { SettingsIcon } from '@/components/icons';

import { isAdmin } from '@/utils/auth';
import { getCachedConfigs, clearConfigCache, updateSiteConfig } from '@/config/site';

// 简单的保存图标组件
const SaveIcon = ({ className }: { className?: string }) => (
  <svg
    className={className}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2"
    strokeLinecap="round"
    strokeLinejoin="round"
  >
    <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
    <polyline points="17,21 17,13 7,13 7,21" />
    <polyline points="7,3 7,8 15,8" />
  </svg>
);

interface ConfigItem {
  key: string;
  label: string;
  placeholder?: string;
  description?: string;
  inputType?: 'text' | 'password';
  type: 'input' | 'switch' | 'select';
  options?: { label: string; value: string; description?: string }[];
  shouldShow?: (configs: Record<string, string>) => boolean;
}

const isCaptchaEnabled = (configs: Record<string, string>) => configs.captcha_enabled === 'true';
const isNativeCaptchaProvider = (configs: Record<string, string>) =>
  isCaptchaEnabled(configs) && (configs.captcha_provider || 'native') === 'native';

// 网站配置项定义
const CONFIG_ITEMS: ConfigItem[] = [
  {
    key: 'ip',
    label: '面板后端地址',
    description: '格式“ip/domain:port”,用于对接节点时使用',
    placeholder: '请输入后端IP/域名:PORT',
    type: 'input'
  },
  {
    key: 'protocol_type',
    label: '连接协议',
    description: '连接后端使用的协议',
    type: 'select',
    options: [
      { label: 'HTTP', value: 'http' },
      { label: 'HTTPS', value: 'https' }
    ]
  },
  {
    key: 'app_name',
    label: '应用名称',
    placeholder: '请输入应用名称',
    description: '在浏览器标签页和导航栏显示的应用名称',
    type: 'input'
  },
  {
    key: 'app_logo',
    label: '登录页Logo链接',
    placeholder: 'https://example.com/logo.png',
    description: '登录页面品牌区域显示的Logo地址，留空使用默认Logo',
    type: 'input'
  },
  {
    key: 'login_page_description',
    label: '登录页面自定义简介',
    placeholder: '例如：Subscription to API Conversion Platform',
    description: '显示在登录页面应用名称下方，默认留空不显示',
    type: 'input'
  },
  {
    key: 'captcha_enabled',
    label: '启用验证码',
    description: '开启后，用户登录时需要完成验证码验证',
    type: 'switch'
  },
  {
    key: 'captcha_provider',
    label: '验证码提供商',
    description: '选择登录验证码提供商，原生为内置验证码，其它为第三方服务',
    type: 'select',
    shouldShow: isCaptchaEnabled,
    options: [
      {
        label: '原生',
        value: 'native',
        description: '使用系统内置验证码能力'
      },
      {
        label: '极验 v4',
        value: 'geetest',
        description: '接入 GeeTest v4 行为验证码'
      },
      {
        label: 'Google reCAPTCHA',
        value: 'recaptcha',
        description: '接入 Google reCAPTCHA (v2 Invisible)'
      },
      {
        label: 'hCaptcha',
        value: 'hcaptcha',
        description: '接入 hCaptcha (Invisible)'
      }
    ]
  },
  {
    key: 'captcha_type',
    label: '验证码类型',
    description: '原生验证码子类型设置',
    type: 'select',
    shouldShow: isNativeCaptchaProvider,
    options: [
      { 
        label: '随机类型', 
        value: 'RANDOM', 
        description: '系统随机选择验证码类型' 
      },
      { 
        label: '滑块验证码', 
        value: 'SLIDER', 
        description: '拖动滑块完成拼图验证' 
      },
      { 
        label: '文字点选验证码', 
        value: 'WORD_IMAGE_CLICK', 
        description: '按顺序点击指定文字' 
      },
      { 
        label: '旋转验证码', 
        value: 'ROTATE', 
        description: '旋转图片到正确角度' 
      },
      { 
        label: '拼图验证码', 
        value: 'CONCAT', 
        description: '拖动滑块完成图片拼接' 
      }
    ]
  },
  {
    key: 'captcha_geetest_id',
    label: 'GeeTest CAPTCHA ID',
    placeholder: '请输入 GeeTest CAPTCHA ID',
    description: '极验控制台中的 captcha_id',
    type: 'input',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'geetest'
  },
  {
    key: 'captcha_geetest_key',
    label: 'GeeTest CAPTCHA KEY',
    placeholder: '请输入 GeeTest CAPTCHA KEY',
    description: '极验控制台中的 captcha_key（敏感信息）',
    type: 'input',
    inputType: 'password',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'geetest'
  },
  {
    key: 'captcha_geetest_domain',
    label: 'GeeTest API 域名',
    placeholder: 'https://gcaptcha4.geetest.com',
    description: '极验服务域名，默认 https://gcaptcha4.geetest.com',
    type: 'input',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'geetest'
  },
  {
    key: 'captcha_recaptcha_site_key',
    label: 'reCAPTCHA Site Key',
    placeholder: '请输入 Google reCAPTCHA Site Key',
    description: '前端公开站点密钥',
    type: 'input',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'recaptcha'
  },
  {
    key: 'captcha_recaptcha_secret_key',
    label: 'reCAPTCHA Secret Key',
    placeholder: '请输入 Google reCAPTCHA Secret Key',
    description: '后端校验密钥（敏感信息）',
    type: 'input',
    inputType: 'password',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'recaptcha'
  },
  {
    key: 'captcha_hcaptcha_site_key',
    label: 'hCaptcha Site Key',
    placeholder: '请输入 hCaptcha Site Key',
    description: '前端公开站点密钥',
    type: 'input',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'hcaptcha'
  },
  {
    key: 'captcha_hcaptcha_secret_key',
    label: 'hCaptcha Secret Key',
    placeholder: '请输入 hCaptcha Secret Key',
    description: '后端校验密钥（敏感信息）',
    type: 'input',
    inputType: 'password',
    shouldShow: (configs) => isCaptchaEnabled(configs) && configs.captcha_provider === 'hcaptcha'
  }
];

// 初始化时从缓存读取配置，避免闪烁
const getInitialConfigs = (): Record<string, string> => {
  if (typeof window === 'undefined') return {};
  
  const configKeys = [
    'app_name',
    'app_logo',
    'login_page_description',
    'captcha_enabled',
    'captcha_provider',
    'captcha_type',
    'captcha_geetest_id',
    'captcha_geetest_key',
    'captcha_geetest_domain',
    'captcha_recaptcha_site_key',
    'captcha_recaptcha_secret_key',
    'captcha_hcaptcha_site_key',
    'captcha_hcaptcha_secret_key',
    'ip',
    'protocol_type'
  ];
  const initialConfigs: Record<string, string> = {};
  
  try {
    configKeys.forEach(key => {
      const cachedValue = localStorage.getItem('vite_config_' + key);
      if (cachedValue) {
        initialConfigs[key] = cachedValue;
      }
    });
  } catch (error) {
  }

  if (!initialConfigs.protocol_type) {
    initialConfigs.protocol_type = 'http';
  }
  if (!initialConfigs.captcha_provider) {
    initialConfigs.captcha_provider = 'native';
  }
  if (!initialConfigs.captcha_geetest_domain) {
    initialConfigs.captcha_geetest_domain = 'https://gcaptcha4.geetest.com';
  }
  
  return initialConfigs;
};

export default function ConfigPage() {
  const navigate = useNavigate();
  const initialConfigs = getInitialConfigs();
  const [configs, setConfigs] = useState<Record<string, string>>(initialConfigs);
  const [loading, setLoading] = useState(Object.keys(initialConfigs).length === 0); // 如果有缓存数据，不显示loading
  const [saving, setSaving] = useState(false);
  const [hasChanges, setHasChanges] = useState(false);
  const [originalConfigs, setOriginalConfigs] = useState<Record<string, string>>(initialConfigs);

  // 权限检查
  useEffect(() => {
    if (!isAdmin()) {
      toast.error('权限不足，只有管理员可以访问此页面');
      navigate('/dashboard', { replace: true });
      return;
    }
  }, [navigate]);

  // 加载配置数据（优先从缓存）
  const loadConfigs = async (currentConfigs?: Record<string, string>) => {
    const configsToCompare = currentConfigs || configs;
    const hasInitialData = Object.keys(configsToCompare).length > 0;
    
    // 如果已有缓存数据，不显示loading，静默更新
    if (!hasInitialData) {
      setLoading(true);
    }
    
    try {
      const configData = await getCachedConfigs();
      if (!configData.protocol_type) {
        configData.protocol_type = 'http';
      }
      if (!configData.captcha_provider) {
        configData.captcha_provider = 'native';
      }
      if (!configData.captcha_geetest_domain) {
        configData.captcha_geetest_domain = 'https://gcaptcha4.geetest.com';
      }
      
      // 只有在数据有变化时才更新
      const hasDataChanged = JSON.stringify(configData) !== JSON.stringify(configsToCompare);
      if (hasDataChanged) {
        setConfigs(configData);
        setOriginalConfigs({ ...configData });
        setHasChanges(false);
      } else {
      }
    } catch (error) {
      // 只有在没有缓存数据时才显示错误
      if (!hasInitialData) {
        toast.error('加载配置出错，请重试');
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    // 延迟加载，避免阻塞初始渲染
    const timer = setTimeout(() => {
      loadConfigs(initialConfigs);
    }, 100);

    return () => clearTimeout(timer);
  }, []); // 只在组件挂载时执行一次

  // 处理配置项变更
  const handleConfigChange = (key: string, value: string) => {
    let newConfigs = { ...configs, [key]: value };
    
    // 特殊处理：启用验证码时，如果验证码类型未设置，默认为随机
    if (key === 'captcha_enabled' && value === 'true') {
      if (!newConfigs.captcha_provider) {
        newConfigs.captcha_provider = 'native';
      }
      if (!newConfigs.captcha_type) {
        newConfigs.captcha_type = 'RANDOM';
      }
    }

    // 切换到原生时，确保原生子选项存在
    if (key === 'captcha_provider' && value === 'native' && !newConfigs.captcha_type) {
      newConfigs.captcha_type = 'RANDOM';
    }

    // 切换极验时，补齐默认域名
    if (key === 'captcha_provider' && value === 'geetest' && !newConfigs.captcha_geetest_domain) {
      newConfigs.captcha_geetest_domain = 'https://gcaptcha4.geetest.com';
    }
    
    setConfigs(newConfigs);
    
    // 检查是否有变更
    const hasChangesNow = Object.keys(newConfigs).some(
      k => newConfigs[k] !== originalConfigs[k]
    ) || Object.keys(originalConfigs).some(
      k => originalConfigs[k] !== newConfigs[k]
    );
    setHasChanges(hasChangesNow);
  };

  const getChangedKeys = () => {
    const changedFromCurrent = Object.keys(configs).filter(
      (key) => configs[key] !== originalConfigs[key]
    );
    const changedFromOriginal = Object.keys(originalConfigs).filter(
      (key) => originalConfigs[key] !== configs[key]
    );
    return Array.from(new Set([...changedFromCurrent, ...changedFromOriginal]));
  };

  const validateCaptchaProviderConfig = (changedKeys: string[]) => {
    const captchaRelatedChanged = changedKeys.some(
      (key) => key === 'captcha_enabled' || key === 'captcha_provider' || key.startsWith('captcha_')
    );

    if (!captchaRelatedChanged) {
      return true;
    }

    if (configs.captcha_enabled !== 'true') {
      return true;
    }

    const provider = (configs.captcha_provider || 'native').trim();
    if (provider === 'native') {
      return true;
    }

    const requiredFields: Record<string, { key: string; label: string }[]> = {
      geetest: [
        { key: 'captcha_geetest_id', label: 'GeeTest CAPTCHA ID' },
        { key: 'captcha_geetest_key', label: 'GeeTest CAPTCHA KEY' }
      ],
      recaptcha: [
        { key: 'captcha_recaptcha_site_key', label: 'reCAPTCHA Site Key' },
        { key: 'captcha_recaptcha_secret_key', label: 'reCAPTCHA Secret Key' }
      ],
      hcaptcha: [
        { key: 'captcha_hcaptcha_site_key', label: 'hCaptcha Site Key' },
        { key: 'captcha_hcaptcha_secret_key', label: 'hCaptcha Secret Key' }
      ]
    };

    const fields = requiredFields[provider] || [];
    const missingLabels = fields
      .filter((field) => !(configs[field.key] || '').trim())
      .map((field) => field.label);

    if (missingLabels.length > 0) {
      toast.error(`请先填写完整验证码配置：${missingLabels.join('、')}`);
      return false;
    }

    return true;
  };

  // 保存配置
  const handleSave = async () => {
    const changedKeys = getChangedKeys();
    if (!validateCaptchaProviderConfig(changedKeys)) {
      return;
    }

    setSaving(true);
    try {
      const sensitiveKeys = new Set([
        'captcha_geetest_key',
        'captcha_recaptcha_secret_key',
        'captcha_hcaptcha_secret_key'
      ]);
      const payload: Record<string, string> = { ...configs };

      sensitiveKeys.forEach((key) => {
        const raw = payload[key] || '';
        if (!raw.trim()) {
          delete payload[key];
        }
      });

      const response = await updateConfigs(payload);
      if (response.code === 0) {
        toast.success('配置保存成功');
        
        // 清除所有配置缓存，强制下次重新获取
        clearConfigCache();
        
        // 获取变更的配置项
        const changedKeys = Object.keys(payload).filter(
          key => payload[key] !== originalConfigs[key]
        );
        
        setOriginalConfigs({ ...configs });
        setHasChanges(false);
        
        // 品牌配置发生变化时，立即更新网站配置（标题、favicon、登录品牌）
        if (changedKeys.some((key) => ['app_name', 'app_logo', 'login_page_description'].includes(key))) {
          await updateSiteConfig();
        }
        
        // 触发配置更新事件，通知其他组件
        window.dispatchEvent(new CustomEvent('configUpdated', { 
          detail: { changedKeys } 
        }));
      } else {
        toast.error('保存配置失败: ' + response.msg);
      }
    } catch (error) {
      toast.error('保存配置出错，请重试');
    } finally {
      setSaving(false);
    }
  };



  // 检查配置项是否应该显示（依赖检查）
  const shouldShowItem = (item: ConfigItem): boolean => {
    if (!item.shouldShow) {
      return true;
    }
    return item.shouldShow(configs);
  };

  // 渲染不同类型的配置项
  const renderConfigItem = (item: ConfigItem) => {
    const isChanged = hasChanges && configs[item.key] !== originalConfigs[item.key];
    
    switch (item.type) {
      case 'input':
        return (
          <Input
            value={configs[item.key] || ''}
            onChange={(e) => handleConfigChange(item.key, e.target.value)}
            placeholder={item.placeholder}
            type={item.inputType || 'text'}
            variant="bordered"
            size="md"
            classNames={{
              input: "text-sm",
              inputWrapper: isChanged 
                ? "border-warning-300 data-[hover=true]:border-warning-400" 
                : ""
            }}
          />
        );

      case 'switch':
        return (
          <Switch
            isSelected={configs[item.key] === 'true'}
            onValueChange={(checked) => handleConfigChange(item.key, checked ? 'true' : 'false')}
            color="primary"
            size="md"
            classNames={{
              wrapper: isChanged ? "border-warning-300" : ""
            }}
          >
            <span className="text-sm text-gray-700 dark:text-gray-300">
              {configs[item.key] === 'true' ? '已启用' : '已禁用'}
            </span>
          </Switch>
        );

      case 'select':
        return (
          <Select
            selectedKeys={configs[item.key] ? [configs[item.key]] : []}
            onSelectionChange={(keys) => {
              const selectedKey = Array.from(keys)[0] as string;
              if (selectedKey) {
                handleConfigChange(item.key, selectedKey);
              }
            }}
            placeholder="请选择"
            variant="bordered"
            size="md"
            classNames={{
              trigger: isChanged 
                ? "border-warning-300 data-[hover=true]:border-warning-400" 
                : ""
            }}
          >
            {item.options?.map((option) => (
              <SelectItem 
                key={option.value}
                description={option.description}
              >
                {option.label}
              </SelectItem>
            )) || []}
          </Select>
        );

      default:
        return null;
    }
  };

  if (loading) {
    return (
      
        <div className="flex items-center justify-center min-h-[400px]">
          <Spinner size="lg" label="加载配置中..." />
        </div>
      
    );
  }

  return (
    
      <div className="p-6 max-w-4xl mx-auto">
        {/* 页面标题 */}
        <div className="flex items-center gap-3 mb-6">
          <SettingsIcon className="w-8 h-8 text-primary" />
          <div>
            <h1 className="text-2xl font-bold">网站配置</h1>
            <p className="text-gray-600 dark:text-gray-400">
              管理网站的基本信息和显示设置
            </p>
          </div>
        </div>

        <Card className="shadow-md">
          <CardHeader className="pb-4">
            <div className="flex justify-between items-center w-full">
              <div>
                <h2 className="text-xl font-semibold">基本设置</h2>
                <p className="text-sm text-gray-600 dark:text-gray-400">
                  配置网站的基本信息，这些设置会影响网站的显示效果
                </p>
              </div>
              <div className="flex gap-2">

                <Button
                  color="primary"
                  startContent={<SaveIcon className="w-4 h-4" />}
                  onClick={handleSave}
                  isLoading={saving}
                  disabled={!hasChanges}
                >
                  {saving ? '保存中...' : '保存配置'}
                </Button>
              </div>
            </div>
          </CardHeader>

          <Divider />

          <CardBody className="space-y-6 pt-6">
            {CONFIG_ITEMS.map((item, index) => {
              // 检查配置项是否应该显示
              if (!shouldShowItem(item)) {
                return null;
              }

              // 计算是否是最后一个显示的项目（用于决定是否显示分隔线）
              const remainingItems = CONFIG_ITEMS.slice(index + 1).filter(shouldShowItem);
              const isLastItem = remainingItems.length === 0;

              return (
                <div key={item.key} className="space-y-3">
                  <div className="flex flex-col gap-1">
                    <label className="text-sm font-medium text-gray-700 dark:text-gray-300">
                      {item.label}
                    </label>
                    {item.description && (
                      <p className="text-xs text-gray-500 dark:text-gray-400">
                        {item.description}
                      </p>
                    )}
                  </div>
                  
                  {/* 渲染配置项 */}
                  {renderConfigItem(item)}
                  
                  {/* 分隔线 */}
                  {!isLastItem && (
                    <Divider className="mt-6" />
                  )}
                </div>
              );
            })}
          </CardBody>
        </Card>

        {/* 操作提示 */}
        {hasChanges && (
          <Card className="mt-4 bg-warning-50 dark:bg-warning-900/20 border-warning-200 dark:border-warning-800">
            <CardBody className="py-3">
              <div className="flex items-center gap-2 text-warning-700 dark:text-warning-300">
                <div className="w-2 h-2 bg-warning-500 rounded-full animate-pulse" />
                <span className="text-sm">
                  检测到配置变更，请记得保存您的修改
                </span>
              </div>
            </CardBody>
          </Card>
        )}
      </div>
    
  );
} 