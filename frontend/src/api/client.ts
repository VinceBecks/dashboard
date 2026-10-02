import type { ActionResult, FormValue, ObjectRef } from './types';

/** Fehler einer API-Anfrage mit HTTP-Status. */
export class ApiError extends Error {
  readonly status: number;
  readonly problems: string[];

  constructor(status: number, message: string, problems: string[] = []) {
    super(message);
    this.status = status;
    this.problems = problems;
  }
}

export type ConditionalResult<T> = { notModified: true } | { notModified: false; data: T; etag: string | null };

/**
 * GET mit ETag: Bei 304 ist das Frontend bereits auf dem aktuellen Stand und der bestehende Zustand bleibt.
 */
export async function getConditional<T>(url: string, etag: string | null, signal?: AbortSignal): Promise<ConditionalResult<T>> {
  const headers: HeadersInit = etag ? { 'If-None-Match': etag } : {};
  const res = await fetch(url, { headers, signal });
  if (res.status === 304) {
    return { notModified: true };
  }
  if (!res.ok) {
    throw await toError(res);
  }
  return { notModified: false, data: (await res.json()) as T, etag: res.headers.get('ETag') };
}

/** Führt eine Aktion auf einem Objekt aus. */
export async function executeAction(
  actionId: string,
  object: ObjectRef,
  inputs: Record<string, FormValue>,
): Promise<ActionResult> {
  const res = await fetch(`/api/actions/${encodeURIComponent(actionId)}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ object, inputs }),
  });
  if (!res.ok) {
    throw await toError(res);
  }
  return (await res.json()) as ActionResult;
}

async function toError(res: Response): Promise<ApiError> {
  try {
    const body = (await res.json()) as { error?: string; problems?: string[] };
    return new ApiError(res.status, body.error ?? res.statusText, body.problems);
  } catch {
    return new ApiError(res.status, res.statusText || `HTTP ${res.status}`);
  }
}
