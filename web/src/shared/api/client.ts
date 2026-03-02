import type { ApiResponse } from '../types';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || '';

// Token 管理
let accessToken: string | null = localStorage.getItem('token');

export function setToken(token: string | null) {
  accessToken = token;
  if (token) {
    localStorage.setItem('token', token);
  } else {
    localStorage.removeItem('token');
  }
}

export function getToken(): string | null {
  return accessToken;
}

// 查询参数类型
// eslint-disable-next-line @typescript-eslint/no-explicit-any
type QueryParams = Record<string, any>;

// 统一请求方法
async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  params?: QueryParams,
): Promise<T> {
  const url = new URL(`${BASE_URL}${path}`, window.location.origin);

  // 添加查询参数
  if (params) {
    Object.entries(params).forEach(([key, value]) => {
      if (value !== undefined && value !== null && value !== '') {
        url.searchParams.set(key, String(value));
      }
    });
  }

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };

  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`;
  }

  const res = await fetch(url.toString(), {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  const json: ApiResponse<T> = await res.json();

  if (json.code !== 0) {
    // Token 过期，尝试刷新
    if (res.status === 401 && accessToken) {
      setToken(null);
      window.location.href = '/login';
    }
    throw new ApiError(json.code, json.message, res.status);
  }

  return json.data;
}

// API 错误类
export class ApiError extends Error {
  constructor(
    public code: number,
    message: string,
    public httpStatus: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

// 导出快捷方法
export function get<T>(path: string, params?: QueryParams): Promise<T> {
  return request<T>('GET', path, undefined, params);
}

export function post<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('POST', path, body);
}

export function put<T>(path: string, body?: unknown): Promise<T> {
  return request<T>('PUT', path, body);
}

export function del<T>(path: string): Promise<T> {
  return request<T>('DELETE', path);
}
