import { Button } from "@heroui/button";
import { Input } from "@heroui/input";
import { Card, CardBody, CardHeader } from "@heroui/card";
import { useState, useEffect, useRef } from "react";
import { useNavigate } from "react-router-dom";
import toast from 'react-hot-toast';
import axios from 'axios';
import { isWebViewFunc } from '@/utils/panel';
import { siteConfig } from '@/config/site';
import { title } from "@/components/primitives";
import { login, LoginData, checkCaptcha } from "@/api";
import "@/utils/tac.css";
import "@/utils/tac.min.js";
import bgImage from "@/images/bg.jpg";


interface LoginForm {
  username: string;
  password: string;
  captchaId: string;
}



interface CaptchaConfig {
  requestCaptchaDataUrl: string;
  validCaptchaUrl: string;
  bindEl: string;
  validSuccess: (res: any, captcha: any, tac: any) => void;
  validFail?: (res: any, captcha: any, tac: any) => void;
  btnCloseFun?: (event: any, tac: any) => void;
  btnRefreshFun?: (event: any, tac: any) => void;
}

interface CaptchaStyle {
  btnUrl?: string;
  bgUrl?: string;
  logoUrl?: string | null;
  moveTrackMaskBgColor?: string;
  moveTrackMaskBorderColor?: string;
}

export default function IndexPage() {
  const [form, setForm] = useState<LoginForm>({
    username: "",
    password: "",
    captchaId: "",
  });
  const [loading, setLoading] = useState(false);
  const [errors, setErrors] = useState<Partial<LoginForm>>({});
  const [showCaptcha, setShowCaptcha] = useState(false);
  const navigate = useNavigate();
  const tacInstanceRef = useRef<any>(null);
  const captchaContainerRef = useRef<HTMLDivElement>(null);
  const [isWebView, setIsWebView] = useState(false);
  const isDarkMode = document.documentElement.classList.contains('dark') ||
    document.documentElement.getAttribute('data-theme') === 'dark' ||
    window.matchMedia('(prefers-color-scheme: dark)').matches;
  // 清理验证码实例
  useEffect(() => {
    return () => {
      if (tacInstanceRef.current) {
        tacInstanceRef.current.destroyWindow();
        tacInstanceRef.current = null;
      }
    };
  }, []);
  // 检测是否在WebView中运行
  useEffect(() => {
    setIsWebView(isWebViewFunc());
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

  // 初始化验证码
  const initCaptcha = async () => {
    if (!window.TAC || !captchaContainerRef.current) {
      return;
    }

    try {
      // 清理之前的验证码实例
      if (tacInstanceRef.current) {
        tacInstanceRef.current.destroyWindow();
        tacInstanceRef.current = null;
      }

      // 使用axios的baseURL，确保在WebView中使用正确的面板地址
      const baseURL = axios.defaults.baseURL || (import.meta.env.VITE_API_BASE ? `${import.meta.env.VITE_API_BASE}/api/v1/` : '/api/v1/');
      
      const config: CaptchaConfig = {
        requestCaptchaDataUrl: `${baseURL}captcha/generate`,
        validCaptchaUrl: `${baseURL}captcha/verify`, 
        bindEl: "#captcha-container",
        validSuccess: (res: any, _: any, tac: any) => {
          

          form.captchaId = res.data.validToken

          setShowCaptcha(false);
          tac.destroyWindow();
          performLogin();
        },
        validFail: (_: any, _captcha: any, tac: any) => {
          tac.reloadCaptcha();
        },
        btnCloseFun: (_event: any, tac: any) => {
          setShowCaptcha(false);
          tac.destroyWindow();
          setLoading(false);
        },
        btnRefreshFun: (_event: any, tac: any) => {
          tac.reloadCaptcha();
        }
      };

      // 检测暗黑模式
      const isDarkMode = document.documentElement.classList.contains('dark') || 
                        document.documentElement.getAttribute('data-theme') === 'dark' ||
                        window.matchMedia('(prefers-color-scheme: dark)').matches;
      
      // 根据主题调整颜色
      const trackColor = isDarkMode ? "#4a5568" : "#7db0be"; // 暗黑模式使用更深的灰蓝色
      
      const style: CaptchaStyle = {
        bgUrl: bgImage,
        logoUrl: null,
        moveTrackMaskBgColor: trackColor,
        moveTrackMaskBorderColor: trackColor
      };

      tacInstanceRef.current = new window.TAC(config, style);
      tacInstanceRef.current.init();

    } catch (error) {
      console.error('初始化验证码失败:', error);
      toast.error('验证码初始化失败，请刷新页面重试');
      setShowCaptcha(false);
      setLoading(false);
    }
  };

  // 执行登录请求
  const performLogin = async () => {


    try {
      const loginData: LoginData = {
        username: form.username.trim(),
        password: form.password,
        captchaId: form.captchaId,
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
      // 先检查是否需要验证码
      const checkResponse = await checkCaptcha();
      
      if (checkResponse.code !== 0) {
        toast.error("检查验证码状态失败，请重试" + checkResponse.msg);
        setLoading(false);
        return;
      }

      // 根据返回值决定是否显示验证码
      if (checkResponse.data === 0) {
        // 不需要验证码，直接登录
        await performLogin();
      } else {
        // 需要验证码，显示验证码弹层
        setShowCaptcha(true);
        // 延时初始化验证码，确保DOM已渲染
        setTimeout(() => {
          initCaptcha();
        }, 100);
      }
    } catch (error) {
      console.error('检查验证码状态错误:', error);
      toast.error("网络错误，请稍后重试" + error);
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

      <main className="relative z-10 min-h-screen flex items-center justify-center p-4 sm:p-8">
        <Card className="w-full max-w-md border border-slate-200/80 dark:border-slate-700/70 bg-white/92 dark:bg-slate-900/88 backdrop-blur-xl shadow-2xl">
          <CardHeader className="pb-0 pt-7 px-6 flex-col items-start">
            <span className="inline-flex items-center rounded-full bg-sky-100 dark:bg-sky-500/20 text-sky-700 dark:text-sky-300 text-xs px-2.5 py-1 font-medium">Welcome Back</span>
            <h1 className={`${title({ size: "sm" })} mt-3`}>登录控制面板</h1>
            <p className="text-small text-default-500 mt-2">输入账号信息以继续</p>
          </CardHeader>

          <CardBody className="px-6 py-6">
            <div className="space-y-4">
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
                className="w-full mt-2 font-semibold"
              >
                {loading ? (showCaptcha ? "验证中..." : "登录中...") : "登录"}
              </Button>

              <div className="grid grid-cols-2 gap-2 pt-1">
                <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-slate-50/70 dark:bg-slate-900/50">
                  <p className="text-[11px] uppercase tracking-[0.14em] panel-muted">Auth</p>
                  <p className="text-xs font-medium text-foreground mt-1">账号密码验证</p>
                </div>
                <div className="rounded-xl border border-slate-200/80 dark:border-slate-700/70 px-3 py-2 bg-slate-50/70 dark:bg-slate-900/50">
                  <p className="text-[11px] uppercase tracking-[0.14em] panel-muted">Captcha</p>
                  <p className="text-xs font-medium text-foreground mt-1">按策略触发滑动验证</p>
                </div>
              </div>
            </div>
          </CardBody>
        </Card>
      </main>

      <footer className="fixed inset-x-0 bottom-4 text-center py-2 z-10">
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
          v{isWebView ? siteConfig.app_version : siteConfig.version}
        </p>
      </footer>

      {/* 验证码弹层 */}
      {showCaptcha && (
        <div className="fixed inset-0 z-50 flex items-center justify-center">
          {/* 背景遮罩层 - 模糊效果，暗黑模式下更深 */}
          <div className="absolute inset-0 bg-black/60 dark:bg-black/80 backdrop-blur-sm captcha-backdrop-enter" />
          {/* 验证码容器 */}
          <div className="mb-4">
            <div
              id="captcha-container"
              ref={captchaContainerRef}
              className={`w-full flex justify-center ${isDarkMode ? 'brightness-[0.8] contrast-[0.9]' : ''}`}
            />
          </div>
        </div>
      )}
    </div>
  );
}
