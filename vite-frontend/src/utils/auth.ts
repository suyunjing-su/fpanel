import { getRoleIdFromToken, isTokenValid } from './jwt';

const DEV_BYPASS_KEY = '__flux_dev_bypass__';

export function isDevBypassEnabled(): boolean {
  if (!import.meta.env.DEV) {
    return false;
  }
  return localStorage.getItem(DEV_BYPASS_KEY) === '1';
}

export function enableDevBypassSession(): void {
  if (!import.meta.env.DEV) {
    return;
  }

  // Keep bypass data explicit so it is easy to turn off from localStorage.
  localStorage.setItem(DEV_BYPASS_KEY, '1');
  localStorage.setItem('token', 'dev-bypass-token');
  localStorage.setItem('role_id', '0');
  localStorage.setItem('admin', 'true');
  localStorage.setItem('name', 'DevAdmin');
}

export function disableDevBypassSession(): void {
  localStorage.removeItem(DEV_BYPASS_KEY);
}

/**
 * 权限工具类
 */

/**
 * 获取当前用户的token
 * @returns token
 */
export function getToken(): string | null {
  return localStorage.getItem('token');
}

/**
 * 获取当前用户的角色ID
 * @returns 角色ID
 */
export function getCurrentUserRoleId(): number | null {
  if (isDevBypassEnabled()) {
    const roleId = parseInt(localStorage.getItem('role_id') || '0', 10);
    return Number.isNaN(roleId) ? 0 : roleId;
  }

  const token = getToken();
  if (!token || !isTokenValid(token)) {
    return null;
  }
  return getRoleIdFromToken(token);
}

/**
 * 判断当前用户是否是管理员
 * @returns 是否是管理员
 */
export function isAdmin(): boolean {
  const roleId = getCurrentUserRoleId();
  return roleId === 0;
}

/**
 * 判断当前用户是否有指定角色
 * @param targetRoleId 目标角色ID
 * @returns 是否有指定角色
 */
export function hasRole(targetRoleId: number): boolean {
  const roleId = getCurrentUserRoleId();
  return roleId === targetRoleId;
}

/**
 * 判断当前用户是否已登录且token有效
 * @returns 是否已登录
 */
export function isLoggedIn(): boolean {
  if (isDevBypassEnabled()) {
    return true;
  }

  const token = getToken();
  return token ? isTokenValid(token) : false;
}

/**
 * 权限检查装饰器函数
 * @param fn 要执行的函数
 * @param errorMsg 权限不足时的错误提示
 * @returns 包装后的函数
 */
export function requireAdmin<T extends (...args: any[]) => any>(
  fn: T, 
  errorMsg: string = '权限不足，仅管理员可操作'
): T {
  return ((...args: Parameters<T>) => {
    if (!isAdmin()) {
      console.warn(errorMsg);
      return false;
    }
    return fn(...args);
  }) as T;
} 