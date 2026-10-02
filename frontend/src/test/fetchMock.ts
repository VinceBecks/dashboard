import { vi } from 'vitest';

export interface MockResponse {
  status: number;
  body?: unknown;
  etag?: string;
}

/** Ersetzt fetch durch einen Mock, der die übergebenen Handler nach URL beantwortet. */
export function mockFetch(handler: (url: string, init?: RequestInit) => MockResponse) {
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const res = handler(String(input), init);
    const headers = new Headers(res.etag ? { ETag: res.etag } : {});
    const body = res.body === undefined ? null : JSON.stringify(res.body);
    return new Response(res.status === 304 ? null : body, { status: res.status, headers });
  });
  vi.stubGlobal('fetch', fn);
  return fn;
}
