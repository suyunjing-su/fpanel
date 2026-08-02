import { Button } from "@heroui/button";
import { Input } from "@heroui/input";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { useState, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import toast from 'react-hot-toast';
import ReCAPTCHA from 'react-google-recaptcha';
import HCaptcha from '@hcaptcha/react-hcaptcha';
import { getCachedConfig, getPanelBrandLogo, siteConfig } from '@/config/site';
import { login, LoginData, checkCaptcha, getCaptchaRuntime, CaptchaRuntimeConfig } from "@/api";

interface LoginForm {
  username: string;
  password: string;
  captchaId: string;
  captchaProvider?: string;
  captchaToken?: string;
  captchaPayload?: string;
}



type CaptchaProvider = 'geetest' | 'recaptcha' | 'hcaptcha' | 'turnstile';

const DEFAULT_CAPTCHA_RUNTIME: CaptchaRuntimeConfig = {
  enabled: false,
  provider: 'geetest',
  geetestCaptchaId: '',
  recaptchaSiteKey: '',
  hcaptchaSiteKey: '',
  turnstileSiteKey: ''
};

export default function IndexPage() {
  const [form, setForm] = useState<LoginForm>({
    username: "",
    password: "",
    captchaId: "",
    captchaProvider: '',
    captchaToken: '',
    captchaPayload: ''
  });
  const [loading, setLoading] = useState(false);
  const [errors, setErrors] = useState<Partial<LoginForm>>({});
  const [showCaptcha, setShowCaptcha] = useState(false);
  const [captchaRuntime, setCaptchaRuntime] = useState<CaptchaRuntimeConfig>(DEFAULT_CAPTCHA_RUNTIME);
  const [captchaProvider, setCaptchaProvider] = useState<CaptchaProvider>('geetest');
  const navigate = useNavigate();
  const geeTestRef = useRef<any>(null);
  const recaptchaRef = useRef<ReCAPTCHA | null>(null);
  const hcaptchaRef = useRef<HCaptcha | null>(null);
  const turnstileContainerRef = useRef<HTMLDivElement>(null);
  const turnstileWidgetRef = useRef<string | null>(null);
  const geetestContainerId = 'geetest-captcha-container';
  const [appName, setAppName] = useState(siteConfig.name || 'flux');
  const [loginLogo, setLoginLogo] = useState(siteConfig.app_logo || '');
  const [loginDescription, setLoginDescription] = useState(siteConfig.login_page_description || '');
  const isDarkMode = document.documentElement.classList.contains('dark') ||
    document.documentElement.getAttribute('data-theme') === 'dark' ||
    window.matchMedia('(prefers-color-scheme: dark)').matches;
  useEffect(() => {
    return () => {
      if (geeTestRef.current?.destroy) {
        geeTestRef.current.destroy();
        geeTestRef.current = null;
      }
      if (turnstileWidgetRef.current && window.turnstile) {
        window.turnstile.remove(turnstileWidgetRef.current);
        turnstileWidgetRef.current = null;
      }
    };
  }, []);

  useEffect(() => {
    const syncLoginBranding = async () => {
      try {
        const [cachedAppName, cachedLogo, cachedDescription] = await Promise.all([
          getCachedConfig('app_name'),
          getCachedConfig('app_logo'),
          getCachedConfig('login_page_description')
        ]);

        if (cachedAppName) {
          setAppName(cachedAppName);
        }
        setLoginLogo(cachedLogo || '');
        setLoginDescription(cachedDescription || '');
      } catch (error) {
        console.warn('同步登录品牌配置失败:', error);
      }
    };

    syncLoginBranding();
    window.addEventListener('configUpdated', syncLoginBranding);
    return () => window.removeEventListener('configUpdated', syncLoginBranding);
  }, []);
  // 验证表单
  const validateForm = (): boolean => {
    const newErrors: Partial<LoginForm> = {};

    if (!form.username.trim()) {
      newErrors.username = '请输入用户名';
    }

    if (!form.password.trim()) {
      newErrors.password = '请输入密码';
    } else if (form.password.length < 6) {
      newErrors.password = '密码长度至少6位';
    }


    setErrors(newErrors);
    return Object.keys(newErrors).length === 0;
  };

  // 处理输入变化
  const handleInputChange = (field: keyof LoginForm, value: string) => {
    setForm(prev => ({ ...prev, [field]: value }));
    // 清除该字段的错误
    if (errors[field]) {
      setErrors(prev => ({ ...prev, [field]: undefined }));
    }
  };

  const loadCaptchaRuntime = async (): Promise<CaptchaRuntimeConfig> => {
    const runtimeResponse = await getCaptchaRuntime();
    if (runtimeResponse.code !== 0 || !runtimeResponse.data) {
      throw new Error(runtimeResponse.msg || '获取验证码配置失败');
    }

    const runtime = {
      ...DEFAULT_CAPTCHA_RUNTIME,
      ...runtimeResponse.data
    };
    const provider = (runtime.provider || 'geetest').toLowerCase() as CaptchaProvider;
    runtime.provider = provider;
    setCaptchaRuntime(runtime);
    setCaptchaProvider(provider);
    return runtime;
  };

  const resetCaptchaState = () => {
    if (geeTestRef.current?.destroy) {
      geeTestRef.current.destroy();
      geeTestRef.current = null;
    }
    recaptchaRef.current?.reset();
    hcaptchaRef.current?.resetCaptcha();
    if (turnstileWidgetRef.current && window.turnstile) {
      window.turnstile.remove(turnstileWidgetRef.current);
      turnstileWidgetRef.current = null;
    }
  };

  const ensureGeeTestScript = async (): Promise<void> => {
    if (window.initGeetest4) {
      return;
    }

    await new Promise<void>((resolve, reject) => {
      const script = document.createElement('script');
      script.src = 'https://static.geetest.com/v4/gt4.js';
      script.async = true;
      script.onload = () => resolve();
      script.onerror = () => reject(new Error('加载 GeeTest SDK 失败'));
      document.head.appendChild(script);
    });
  };

  const initGeeTestCaptcha = async (runtime: CaptchaRuntimeConfig) => {
    const captchaId = (runtime.geetestCaptchaId || '').trim();
    if (!captchaId) {
      throw new Error('GeeTest CAPTCHA ID 未配置');
    }

    await ensureGeeTestScript();
    resetCaptchaState();

    await new Promise<void>((resolve, reject) => {
      if (!window.initGeetest4) {
        reject(new Error('GeeTest SDK 未就绪'));
        return;
      }

      window.initGeetest4(
        {
          captchaId,
          product: 'bind'
        },
        (gt) => {
          geeTestRef.current = gt;
          gt
            .appendTo(`#${geetestContainerId}`)
            .onSuccess(() => {
              const result = gt.getValidate();
              const payload = JSON.stringify(result);
              setForm((prev) => ({ ...prev, captchaId, captchaProvider: 'geetest', captchaToken: payload, captchaPayload: payload }));
              setShowCaptcha(false);
              void performLogin({
                captchaId,
                captchaProvider: 'geetest',
                captchaToken: payload,
                captchaPayload: payload
              }).then(() => resolve()).catch((error) => reject(error));
            })
          ;

          if (gt.onError) {
            gt.onError(() => {
              reject(new Error('GeeTest 初始化失败'));
            });
          }

          gt.showBox();
        }
      );
    });
  };

  const executeGoogleRecaptcha = async (runtime: CaptchaRuntimeConfig) => {
    const siteKey = (runtime.recaptchaSiteKey || '').trim();
    if (!siteKey) {
      throw new Error('Google reCAPTCHA Site Key 未配置');
    }

    const token = await recaptchaRef.current?.executeAsync();
    recaptchaRef.current?.reset();
    if (!token) {
      throw new Error('reCAPTCHA 验证失败');
    }

    await performLogin({
      captchaProvider: 'recaptcha',
      captchaToken: token,
      captchaPayload: ''
    });
  };

  const executeHCaptcha = async (runtime: CaptchaRuntimeConfig) => {
    const siteKey = (runtime.hcaptchaSiteKey || '').trim();
    if (!siteKey) {
      throw new Error('hCaptcha Site Key 未配置');
    }

    const tokenResult = await (hcaptchaRef.current as any)?.execute({ async: true });
    const token = typeof tokenResult === 'string' ? tokenResult : tokenResult?.response;
    hcaptchaRef.current?.resetCaptcha();
    if (!token) {
      throw new Error('hCaptcha 验证失败');
    }

    await performLogin({
      captchaProvider: 'hcaptcha',
      captchaToken: token,
      captchaPayload: ''
    });
  };

  const ensureTurnstileScript = async (): Promise<void> => {
    if (window.turnstile) {
      return;
    }
    await new Promise<void>((resolve, reject) => {
      const existing = document.querySelector<HTMLScriptElement>('script[data-flux-turnstile]');
      if (existing) {
        existing.addEventListener('load', () => resolve(), { once: true });
        existing.addEventListener('error', () => reject(new Error('加载 Turnstile SDK 失败')), { once: true });
        return;
      }
      const script = document.createElement('script');
      script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
      script.async = true;
      script.defer = true;
      script.dataset.fluxTurnstile = 'true';
      script.onload = () => resolve();
      script.onerror = () => {
        script.remove();
        reject(new Error('加载 Turnstile SDK 失败'));
      };
      document.head.appendChild(script);
    });
  };

  const initTurnstile = async (runtime: CaptchaRuntimeConfig) => {
    const siteKey = (runtime.turnstileSiteKey || '').trim();
    if (!siteKey) {
      throw new Error('Turnstile Site Key 未配置');
    }
    await ensureTurnstileScript();
    resetCaptchaState();
    if (!window.turnstile || !turnstileContainerRef.current) {
      throw new Error('Turnstile SDK 未就绪');
    }
    turnstileWidgetRef.current = window.turnstile.render(turnstileContainerRef.current, {
      sitekey: siteKey,
      theme: isDarkMode ? 'dark' : 'light',
      callback: (token) => {
        setShowCaptcha(false);
        void performLogin({ captchaProvider: 'turnstile', captchaToken: token, captchaPayload: '' });
      },
      'expired-callback': () => {
        if (turnstileWidgetRef.current) {
          window.turnstile?.reset(turnstileWidgetRef.current);
        }
      },
      'error-callback': () => {
        toast.error('Turnstile 验证加载失败，请重试');
        setShowCaptcha(false);
        setLoading(false);
        return true;
      }
    });
  };

  // 执行登录请求
  const performLogin = async (captchaOverrides?: Partial<LoginData>) => {


    try {
      const loginData: LoginData = {
        username: form.username.trim(),
        password: form.password,
        captchaId: form.captchaId,
        captchaProvider: form.captchaProvider || '',
        captchaToken: form.captchaToken || '',
        captchaPayload: form.captchaPayload || '',
        ...captchaOverrides
      };

      const response = await login(loginData);
      
      if (response.code !== 0) {
        toast.error(response.msg || "登录失败");
        return;
      }

      // 检查是否需要强制修改密码
      if (response.data.requirePasswordChange) {
        localStorage.setItem('token', response.data.token);
        localStorage.setItem("role_id", response.data.role_id.toString());
        localStorage.setItem("name", response.data.name);
        localStorage.setItem("admin", (response.data.role_id === 0).toString());
        toast.success('检测到默认密码，即将跳转到修改密码页面');
        navigate("/change-password");
        return;
      }

      // 保存登录信息
      localStorage.setItem('token', response.data.token);
      localStorage.setItem("role_id", response.data.role_id.toString());
      localStorage.setItem("name", response.data.name);
      localStorage.setItem("admin", (response.data.role_id === 0).toString());

      // 登录成功
      toast.success('登录成功');
      navigate("/dashboard");

    } catch (error) {
      console.error('登录错误:', error);
      toast.error("网络错误，请稍后重试");
    } finally {
      setLoading(false);
    }
  };

  const handleLogin = async () => {
    if (!validateForm()) return;

    setLoading(true);

    try {
      const checkResponse = await checkCaptcha();
      if (checkResponse.code !== 0) {
        toast.error('检查验证码状态失败，请重试' + checkResponse.msg);
        setLoading(false);
        return;
      }

      if (checkResponse.data === 0) {
        setCaptchaRuntime(DEFAULT_CAPTCHA_RUNTIME);
        setForm((prev) => ({
          ...prev,
          captchaId: '',
          captchaProvider: '',
          captchaToken: '',
          captchaPayload: ''
        }));
        await performLogin();
      } else {
        const runtime = await loadCaptchaRuntime();
        const provider = (runtime.provider || 'geetest').toLowerCase() as CaptchaProvider;

        if (provider === 'geetest') {
          setShowCaptcha(true);
          setTimeout(() => {
            initGeeTestCaptcha(runtime).catch((error) => {
              console.error('初始化 GeeTest 失败:', error);
              toast.error(error instanceof Error ? error.message : '初始化 GeeTest 失败');
              setShowCaptcha(false);
              setLoading(false);
            });
          }, 100);
          return;
        }

        if (provider === 'recaptcha') {
          setTimeout(() => {
            executeGoogleRecaptcha(runtime).catch((error) => {
              console.error('执行 reCAPTCHA 失败:', error);
              toast.error(error instanceof Error ? error.message : 'reCAPTCHA 验证失败');
              setLoading(false);
            });
          }, 0);
          return;
        }

        if (provider === 'hcaptcha') {
          setTimeout(() => {
            executeHCaptcha(runtime).catch((error) => {
              console.error('执行 hCaptcha 失败:', error);
              toast.error(error instanceof Error ? error.message : 'hCaptcha 验证失败');
              setLoading(false);
            });
          }, 0);
          return;
        }

        if (provider === 'turnstile') {
          setShowCaptcha(true);
          setTimeout(() => {
            initTurnstile(runtime).catch((error) => {
              console.error('初始化 Turnstile 失败:', error);
              toast.error(error instanceof Error ? error.message : '初始化 Turnstile 失败');
              setShowCaptcha(false);
              setLoading(false);
            });
          }, 100);
          return;
        }

        toast.error('不支持的验证码提供商: ' + provider);
        setLoading(false);
      }
    } catch (error) {
      console.error('检查验证码状态错误:', error);
      toast.error('网络错误，请稍后重试');
      setLoading(false);
    }
  };


  const handleKeyPress = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !loading) {
      handleLogin();
    }
  };

  return (
    <div className="relative min-h-screen overflow-hidden bg-gradient-to-br from-slate-50 via-sky-50 to-blue-100/80 dark:from-slate-950 dark:via-slate-900 dark:to-blue-950/50">
      <div className="absolute inset-0 pointer-events-none">
        <div className="h-72 w-72 rounded-full bg-cyan-300/25 blur-3xl absolute -top-20 -left-16" />
        <div className="h-80 w-80 rounded-full bg-blue-300/20 blur-3xl absolute -bottom-20 -right-20" />
      </div>

      <main className="relative z-10 min-h-screen flex items-center justify-center px-4 py-10 sm:px-6">
        <div className="w-full max-w-md">
          <div className="mb-8 text-center">
            <div className="mb-4 inline-flex h-16 w-16 items-center justify-center overflow-hidden rounded-2xl shadow-lg shadow-sky-500/30 bg-white/80 dark:bg-slate-900/70 border border-slate-200/70 dark:border-slate-700/70">
              <img
                src={getPanelBrandLogo(loginLogo)}
                alt="Logo"
                className="h-full w-full object-contain"
                onError={(event) => {
                  event.currentTarget.src = getPanelBrandLogo('');
                }}
              />
            </div>
            <h1 className="mb-2 text-3xl font-bold bg-gradient-to-r from-sky-600 to-blue-500 bg-clip-text text-transparent">
              {appName}
            </h1>
            {loginDescription.trim() && (
              <p className="text-sm text-slate-500 dark:text-slate-400">{loginDescription}</p>
            )}
          </div>

          <Card className="w-full max-w-md border border-slate-200/80 dark:border-slate-700/70 bg-white/94 dark:bg-slate-900/90 backdrop-blur-xl shadow-2xl">
            <CardHeader className="px-7 pt-8 pb-2 flex-col items-center text-center">
              <h2 className="text-2xl font-bold text-slate-900 dark:text-white">欢迎回来</h2>
              <p className="mt-2 text-sm text-slate-500 dark:text-slate-400">登录到您的账户</p>
            </CardHeader>

            <CardBody className="px-7 pb-8 pt-5">
              <div className="space-y-5">
                <Input
                  label="用户名"
                  placeholder="请输入用户名"
                  value={form.username}
                  onChange={(e) => handleInputChange('username', e.target.value)}
                  onKeyDown={handleKeyPress}
                  variant="bordered"
                  isDisabled={loading}
                  isInvalid={!!errors.username}
                  errorMessage={errors.username}
                />

                <Input
                  label="密码"
                  placeholder="请输入密码"
                  type="password"
                  value={form.password}
                  onChange={(e) => handleInputChange('password', e.target.value)}
                  onKeyDown={handleKeyPress}
                  variant="bordered"
                  isDisabled={loading}
                  isInvalid={!!errors.password}
                />

                <Button
                  color="primary"
                  size="lg"
                  onClick={handleLogin}
                  isLoading={loading}
                  disabled={loading}
                  className="w-full font-semibold"
                >
                  {loading ? (showCaptcha ? "验证中..." : "登录中...") : "登录"}
                </Button>
              </div>
            </CardBody>
          </Card>
        </div>
      </main>

      <footer className="absolute inset-x-0 bottom-4 text-center py-2 z-10">
        <p className="text-xs panel-muted">
          Powered by{' '}
          <a
            href="https://github.com/suyunjing-su/fpanel"
            target="_blank"
            rel="noopener noreferrer"
            className="text-slate-500 dark:text-slate-300 hover:text-sky-600 dark:hover:text-sky-300 transition-colors"
          >
            flux-panel
          </a>
        </p>
        <p className="text-xs panel-muted mt-1">
          v{siteConfig.version}
        </p>
      </footer>

      {(captchaRuntime.recaptchaSiteKey || '').trim() && (
        <div className="hidden">
          <ReCAPTCHA
            ref={recaptchaRef}
            sitekey={captchaRuntime.recaptchaSiteKey || ''}
            size="invisible"
          />
        </div>
      )}

      {(captchaRuntime.hcaptchaSiteKey || '').trim() && (
        <div className="hidden">
          <HCaptcha
            ref={hcaptchaRef}
            sitekey={captchaRuntime.hcaptchaSiteKey || ''}
            size="invisible"
          />
        </div>
      )}

      {/* 验证码弹层 */}
      {showCaptcha && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          <div className="absolute inset-0 bg-black/60 dark:bg-black/80 backdrop-blur-sm captcha-backdrop-enter" />
          <div className="relative mb-4 rounded-2xl border border-slate-200 dark:border-slate-700 bg-white/95 dark:bg-slate-900/95 px-6 py-5 shadow-2xl min-w-[320px]">
            <p className="text-sm text-slate-500 dark:text-slate-400 mb-3 text-center">
              {captchaProvider === 'geetest' ? '请完成极验验证' : '请完成 Cloudflare Turnstile 验证'}
            </p>
            {captchaProvider === 'geetest' && (
              <div id={geetestContainerId} className="w-full flex justify-center min-h-[56px]" />
            )}
            {captchaProvider === 'turnstile' && (
              <div ref={turnstileContainerRef} className="w-full flex justify-center min-h-[65px]" />
            )}
          </div>
        </div>
      )}
    </div>
  );
}
