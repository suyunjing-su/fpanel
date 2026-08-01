import axios, { AxiosResponse } from "axios";
import { getCurrentPanelAddress } from "@/utils/panel";
import { isDevBypassEnabled } from "@/utils/auth";

let baseURL = "";

export const reinitializeBaseURL = () => {
  const configuredAddress = getCurrentPanelAddress();
  const address = configuredAddress || import.meta.env.VITE_API_BASE;
  baseURL = address ? `${address.replace(/\/$/, "")}/api/v1/` : "/api/v1/";
  axios.defaults.baseURL = baseURL;
};

reinitializeBaseURL();

export interface ApiResponse<T = any> {
  code: number;
  msg: string;
  data: T;
}

export interface DownloadResponse {
  ok: boolean;
  blob?: Blob;
  filename?: string;
  msg?: string;
}

const authorizationHeader = () => ({
  Authorization: window.localStorage.getItem("token"),
});

const filenameFromDisposition = (value?: string) => {
  const match = value?.match(/filename="?([^";]+)"?/i);
  return match?.[1];
};

const parseBlobError = async (blob: Blob, fallback: string) => {
  try {
    return JSON.parse(await blob.text()) as ApiResponse;
  } catch {
    return { code: -1, msg: fallback, data: null } as ApiResponse;
  }
};

const processAuthenticationFailure = (
  status: number | undefined,
  payload: ApiResponse,
) => {
  if (status === 401 || isTokenExpired(payload)) {
    handleTokenExpired();
  }
};

// 处理token失效的逻辑
function handleTokenExpired() {
  if (isDevBypassEnabled()) {
    return;
  }

  // 清除localStorage中的token
  window.localStorage.removeItem("token");
  window.localStorage.removeItem("role_id");
  window.localStorage.removeItem("name");

  // 跳转到登录页面
  if (window.location.pathname !== "/") {
    window.location.href = "/";
  }
}

// 检查响应是否为token失效
function isTokenExpired(response: ApiResponse) {
  return (
    response &&
    response.code === 401 &&
    (response.msg === "未登录或token已过期" ||
      response.msg === "无效的token或token已过期" ||
      response.msg === "无法获取用户权限信息")
  );
}

const Network = {
  get: function <T = any>(
    path: string = "",
    data: any = {},
  ): Promise<ApiResponse<T>> {
    return new Promise(function (resolve) {
      // 如果baseURL是默认值且是WebView环境，说明没有设置面板地址
      if (baseURL === "") {
        resolve({ code: -1, msg: " - 请先设置面板地址", data: null as T });
        return;
      }

      axios
        .get(path, {
          params: data,
          timeout: 30000,
          headers: authorizationHeader(),
        })
        .then(function (response: AxiosResponse<ApiResponse<T>>) {
          // 检查是否token失效
          if (isTokenExpired(response.data)) {
            handleTokenExpired();
            return;
          }
          resolve(response.data);
        })
        .catch(function (error: any) {
          console.error("GET请求错误:", error);

          if (
            error.response?.status === 401 &&
            isTokenExpired(error.response.data)
          ) {
            handleTokenExpired();
          }
          if (error.response?.data) {
            resolve(error.response.data as ApiResponse<T>);
            return;
          }
          resolve({
            code: -1,
            msg: error.message || "网络请求失败",
            data: null as T,
          });
        });
    });
  },

  download: async function (
    path: string,
    data?: FormData,
    timeout = 120000,
  ): Promise<DownloadResponse> {
    try {
      const response = await axios.post(path, data ?? {}, {
        timeout,
        responseType: "blob",
        headers: data
          ? authorizationHeader()
          : { ...authorizationHeader(), "Content-Type": "application/json" },
      });
      return {
        ok: true,
        blob: response.data as Blob,
        filename:
          filenameFromDisposition(response.headers["content-disposition"]) ||
          "download.bin",
      };
    } catch (error: any) {
      if (error.response?.data instanceof Blob) {
        const payload = await parseBlobError(error.response.data, "下载失败");
        processAuthenticationFailure(error.response.status, payload);
        return { ok: false, msg: payload.msg };
      }
      const payload = error.response?.data as ApiResponse | undefined;
      if (payload) {
        processAuthenticationFailure(error.response?.status, payload);
      }
      return { ok: false, msg: payload?.msg || error.message || "下载失败" };
    }
  },

  upload: function <T = any>(
    path: string,
    data: FormData,
    timeout = 120000,
  ): Promise<ApiResponse<T>> {
    return axios
      .post<ApiResponse<T>>(path, data, {
        timeout,
        headers: authorizationHeader(),
      })
      .then((response) => {
        if (isTokenExpired(response.data)) {
          handleTokenExpired();
        }
        return response.data;
      })
      .catch((error: any) => {
        const payload = error.response?.data as ApiResponse<T> | undefined;
        if (payload) {
          processAuthenticationFailure(error.response?.status, payload);
          return payload;
        }
        return {
          code: -1,
          msg: error.message || "上传失败",
          data: null as T,
        };
      });
  },

  post: function <T = any>(
    path: string = "",
    data: any = {},
    options: { timeout?: number } = {},
  ): Promise<ApiResponse<T>> {
    return new Promise(function (resolve) {
      // 如果baseURL是默认值且是WebView环境，说明没有设置面板地址
      if (baseURL === "") {
        resolve({ code: -1, msg: " - 请先设置面板地址", data: null as T });
        return;
      }

      const requestTimeout = options.timeout ?? 30000;

      axios
        .post(path, data, {
          timeout: requestTimeout,
          headers: {
            Authorization: window.localStorage.getItem("token"),
            "Content-Type": "application/json",
          },
        })
        .then(function (response: AxiosResponse<ApiResponse<T>>) {
          // 检查是否token失效
          if (isTokenExpired(response.data)) {
            handleTokenExpired();
            return;
          }
          resolve(response.data);
        })
        .catch(function (error: any) {
          console.error("POST请求错误:", error);

          if (
            error.response?.status === 401 &&
            isTokenExpired(error.response.data)
          ) {
            handleTokenExpired();
          }
          if (error.response?.data) {
            resolve(error.response.data as ApiResponse<T>);
            return;
          }
          resolve({
            code: -1,
            msg: error.message || "网络请求失败",
            data: null as T,
          });
        });
    });
  },
};

export default Network;
