type LabelContext = "admin" | "h5" | "profile";

type PanelLabelEntry = {
  title: string;
  admin?: string;
  h5?: string;
  profile?: string;
};

const DEFAULT_PAGE_PATH = "/dashboard";

const PANEL_LABELS: Record<string, PanelLabelEntry> = {
  "/": { title: "登录" },
  "/dashboard": { title: "仪表盘", admin: "仪表板", h5: "首页" },
  "/forward": { title: "转发管理", admin: "转发管理", h5: "转发" },
  "/tunnel": { title: "隧道管理", admin: "隧道管理", h5: "隧道" },
  "/node": { title: "节点监控", admin: "节点监控", h5: "节点" },
  "/limit": { title: "限速管理", admin: "限速管理", profile: "限速管理" },
  "/user": { title: "用户管理", admin: "用户管理", profile: "用户管理" },
  "/config": { title: "网站配置", admin: "网站配置", profile: "网站配置" },
  "/runtime-policy": {
    title: "运行策略",
    admin: "运行策略",
    profile: "运行策略",
  },
  "/audit": { title: "审计日志", admin: "审计日志", profile: "审计日志" },
  "/operations": {
    title: "运维与恢复",
    admin: "运维与恢复",
    profile: "运维与恢复",
  },
  "/profile": { title: "个人中心", h5: "我的" },
  "/change-password": { title: "修改密码" },
  "/settings": { title: "设置" },
};

export const getPanelPageTitle = (pathname?: string) => {
  if (!pathname) {
    return PANEL_LABELS[DEFAULT_PAGE_PATH].title;
  }

  return PANEL_LABELS[pathname]?.title || PANEL_LABELS[DEFAULT_PAGE_PATH].title;
};

export const getPanelLabel = (pathname: string, context: LabelContext) => {
  const labelEntry = PANEL_LABELS[pathname];
  if (!labelEntry) {
    return getPanelPageTitle(pathname);
  }

  if (context === "admin") {
    return labelEntry.admin || labelEntry.title;
  }

  if (context === "h5") {
    return labelEntry.h5 || labelEntry.title;
  }

  return labelEntry.profile || labelEntry.admin || labelEntry.title;
};
