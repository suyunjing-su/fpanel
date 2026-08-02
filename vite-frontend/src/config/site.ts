import { getConfigByName, getConfigs } from '@/api';
import defaultBrandIcon from '@/images/logo.ico';
import { getPanelPageTitle } from '@/config/panel-navigation';

export type SiteConfig = typeof siteConfig;

// 缓存相关常量
const CACHE_PREFIX = 'vite_config_';
const VERSION = "3.0.30-beta";
const APP_VERSION = "1.0.3";

const getInitialConfig = () => {
  if (typeof window === 'undefined') {
    return {
      name: "flux",
      version: VERSION,
      app_version: APP_VERSION,
      app_logo: "",
      login_page_description: "",
    };
  }

  const cachedAppName = localStorage.getItem(CACHE_PREFIX + 'app_name');
  const cachedAppLogo = localStorage.getItem(CACHE_PREFIX + 'app_logo') || '';
  const cachedLoginDescription = localStorage.getItem(CACHE_PREFIX + 'login_page_description') || '';
    if (cachedAppName) {
      return {
        name: cachedAppName,
        version: VERSION,
        app_version: APP_VERSION,
        app_logo: cachedAppLogo,
        login_page_description: cachedLoginDescription,
      };
    }
  return {
    name: "flux",
    version: VERSION,
    app_version: APP_VERSION,
    app_logo: cachedAppLogo,
    login_page_description: cachedLoginDescription,
  };
};

export const siteConfig = getInitialConfig();

export { getPanelPageTitle };

export const formatPanelTitle = (pageTitle?: string, appName?: string) => {
  const normalizedPageTitle = (pageTitle || getPanelPageTitle()).trim();
  const normalizedAppName = (appName || siteConfig.name || 'flux').trim();
  return `${normalizedPageTitle} | ${normalizedAppName}`;
};

export const getPanelBrandLogo = (logo?: string) => {
  const normalized = (logo || '').trim();
  return normalized || defaultBrandIcon;
};

export const updateFavicon = (logo?: string) => {
  if (typeof window === 'undefined') {
    return;
  }

  const faviconHref = getPanelBrandLogo(logo);
  let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
  if (!link) {
    link = document.createElement('link');
    link.rel = 'icon';
    document.head.appendChild(link);
  }

  link.type = faviconHref.endsWith('.svg') ? 'image/svg+xml' : 'image/x-icon';
  link.href = faviconHref;
};

const CAPTCHA_SECRET_KEYS = [
  'captcha_geetest_key',
  'captcha_recaptcha_secret_key',
  'captcha_hcaptcha_secret_key',
  'captcha_turnstile_secret_key'
];

// 缓存工具函数
export const configCache = {
  // 获取缓存的配置
  get: (key: string): string | null => {
    const cacheKey = CACHE_PREFIX + key;
      return localStorage.getItem(cacheKey);
  },

  // 设置缓存的配置
  set: (key: string, value: string): void => {
    const cacheKey = CACHE_PREFIX + key;
      localStorage.setItem(cacheKey, value);
  },

  // 删除指定配置的缓存
  remove: (key: string): void => {
    const cacheKey = CACHE_PREFIX + key;
    localStorage.removeItem(cacheKey);
  },

  // 清空所有配置缓存
  clear: (): void => {
   // 获取所有localStorage的key
   const keys = Object.keys(localStorage);
   keys.forEach(key => {
     if (key.startsWith(CACHE_PREFIX)) {
       localStorage.removeItem(key);
     }
   });
  }
};

// 获取单个配置（优先从缓存）
export const getCachedConfig = async (key: string): Promise<string | null> => {
  const cachedValue = configCache.get(key);
  if (cachedValue !== null) {
    return cachedValue;
  }

  const response = await getConfigByName(key);
  if (response.code === 0 && response.data?.value) {
    const value = response.data.value;
    configCache.set(key, value);
    return value;
  }

  return null;
};

// 获取所有配置（优先从缓存）
export const getCachedConfigs = async (): Promise<Record<string, string>> => {
  // 尝试从缓存获取所有配置
  const configKeys = [
    'app_name',
    'app_logo',
    'login_page_description',
    'captcha_enabled',
    'captcha_provider',
    'captcha_geetest_id',
    'captcha_recaptcha_site_key',
    'captcha_hcaptcha_site_key',
    'captcha_turnstile_site_key'
  ];
  const cachedConfigs: Record<string, string> = {};
  let hasCachedData = false;

  configKeys.forEach(key => {
    const cachedValue = configCache.get(key);
    if (cachedValue !== null) {
      cachedConfigs[key] = cachedValue;
      hasCachedData = true;
    }
  });



  // 从API获取最新配置
  try {
    CAPTCHA_SECRET_KEYS.forEach((key) => configCache.remove(key));
    const response = await getConfigs();
    if (response.code === 0 && response.data) {
      const configs = response.data;
      // 将所有配置存入缓存
      Object.entries(configs).forEach(([key, value]) => {
        configCache.set(key, value as string);
      });
      return configs;
    }
  } catch (error) {
    // API失败时返回缓存的数据
    if (hasCachedData) {
      return cachedConfigs;
    }
  }

  return {};
};

// 动态更新网站配置
export const updateSiteConfig = async () => {
  const appName = await getCachedConfig('app_name');
  const appLogo = await getCachedConfig('app_logo');
  const loginDescription = await getCachedConfig('login_page_description');

  if (appName && appName !== siteConfig.name) {
    siteConfig.name = appName;
    const currentPath = typeof window !== 'undefined' ? window.location.pathname : '/dashboard';
    document.title = formatPanelTitle(getPanelPageTitle(currentPath), appName);
  }

  siteConfig.app_logo = appLogo || '';
  siteConfig.login_page_description = loginDescription || '';
  updateFavicon(siteConfig.app_logo);
};

// 清除配置缓存的工具函数
// 缓存清除时机：
// 1. 配置更新时：调用此函数清除所有缓存
// 2. 退出登录时：safeLogout()中的localStorage.clear()会自动清除
export const clearConfigCache = (keys?: string[]) => {
  if (keys && keys.length > 0) {
    // 删除指定的配置缓存
    keys.forEach(key => configCache.remove(key));
  } else {
    // 清空所有配置缓存
    configCache.clear();
  }
};

// 在页面加载时异步更新配置（如果有更新的话）
if (typeof window !== 'undefined') {
  updateFavicon(siteConfig.app_logo);

  // 延迟执行，避免阻塞初始渲染
  setTimeout(() => {
    updateSiteConfig();
  }, 200);
}
