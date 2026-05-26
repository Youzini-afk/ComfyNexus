// Tiny fetch wrapper that:
//   - sends credentials (cookie session)
//   - adds X-Requested-With for CSRF guard
//   - parses {error: {code, message}} and throws ApiError
export class ApiError extends Error {
  code: string;
  status: number;
  constructor(code: string, status: number, message: string) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

export async function api<T = unknown>(
  path: string,
  init: RequestInit = {}
): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set('X-Requested-With', 'XMLHttpRequest');
  if (init.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const res = await fetch(path, {
    ...init,
    credentials: 'include',
    headers,
  });
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get('content-type') ?? '';
  if (!res.ok) {
    let code = 'INTERNAL';
    let message = res.statusText;
    if (ct.includes('application/json')) {
      const body = await res.json().catch(() => null);
      if (body?.error) {
        code = body.error.code ?? code;
        message = body.error.message ?? message;
      }
    }
    throw new ApiError(code, res.status, message);
  }
  if (ct.includes('application/json')) return (await res.json()) as T;
  return (await res.text()) as unknown as T;
}

// Convenience helpers.
export const get = <T,>(p: string) => api<T>(p);
export const post = <T,>(p: string, body?: unknown) =>
  api<T>(p, { method: 'POST', body: body ? JSON.stringify(body) : undefined });
export const put = <T,>(p: string, body?: unknown) =>
  api<T>(p, { method: 'PUT', body: body ? JSON.stringify(body) : undefined });
export const del = <T,>(p: string) => api<T>(p, { method: 'DELETE' });

// Binary upload helper (does NOT JSON-stringify or set Content-Type).
export async function putBinary(path: string, body: ArrayBuffer | Blob, signal?: AbortSignal): Promise<void> {
  const res = await fetch(path, {
    method: 'PUT',
    credentials: 'include',
    headers: { 'X-Requested-With': 'XMLHttpRequest' },
    body,
    signal,
  });
  if (!res.ok) {
    const ct = res.headers.get('content-type') ?? '';
    let code = 'INTERNAL';
    let message = res.statusText;
    if (ct.includes('application/json')) {
      const bodyJson = await res.json().catch(() => null);
      if (bodyJson?.error) {
        code = bodyJson.error.code ?? code;
        message = bodyJson.error.message ?? message;
      }
    }
    throw new ApiError(code, res.status, message);
  }
}
