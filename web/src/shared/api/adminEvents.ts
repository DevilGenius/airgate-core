import { getToken } from './client';

const BASE_URL = import.meta.env.VITE_API_BASE_URL || '';
const ADMIN_EVENTS_PATH = '/api/v1/admin/events';

export interface AdminServerEvent {
  type: string;
  ts?: string;
  account_id?: number;
  current_concurrency?: number;
  reason?: string;
  [key: string]: unknown;
}

type AdminEventListener = (event: AdminServerEvent) => void;

function adminEventsUrl() {
  return new URL(`${BASE_URL}${ADMIN_EVENTS_PATH}`, window.location.origin).toString();
}

function isAbortError(err: unknown) {
  return typeof err === 'object'
    && err !== null
    && 'name' in err
    && (err as { name?: unknown }).name === 'AbortError';
}

function sleep(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    if (signal.aborted) {
      reject(signal.reason);
      return;
    }
    const timeoutId = window.setTimeout(resolve, ms);
    signal.addEventListener('abort', () => {
      window.clearTimeout(timeoutId);
      reject(signal.reason);
    }, { once: true });
  });
}

class AdminEventStream {
  private listeners = new Set<AdminEventListener>();
  private controller: AbortController | null = null;
  private generation = 0;
  private running = false;

  subscribe(listener: AdminEventListener) {
    this.listeners.add(listener);
    this.start();

    return () => {
      this.listeners.delete(listener);
      if (this.listeners.size === 0) {
        this.stop();
      }
    };
  }

  private start() {
    if (this.running || typeof window === 'undefined') return;
    this.running = true;
    this.generation += 1;
    void this.run(this.generation);
  }

  private stop() {
    this.running = false;
    this.generation += 1;
    this.controller?.abort();
    this.controller = null;
  }

  private async run(generation: number) {
    let retryDelayMs = 1000;

    while (this.running && this.generation === generation && this.listeners.size > 0) {
      const controller = new AbortController();
      this.controller = controller;

      try {
        const token = getToken();
        const headers: Record<string, string> = {};
        if (token) headers.Authorization = `Bearer ${token}`;

        const res = await fetch(adminEventsUrl(), {
          method: 'GET',
          headers,
          signal: controller.signal,
        });
        if (!res.ok || !res.body) {
          throw new Error(`admin events HTTP ${res.status}`);
        }

        retryDelayMs = 1000;
        await this.readStream(res.body, controller.signal);
      } catch (err) {
        if (!this.running || this.generation !== generation || controller.signal.aborted || isAbortError(err)) {
          break;
        }
      } finally {
        if (this.controller === controller) {
          this.controller = null;
        }
      }

      if (!this.running || this.generation !== generation || this.listeners.size === 0) {
        break;
      }
      try {
        await sleep(retryDelayMs, controller.signal);
      } catch {
        break;
      }
      retryDelayMs = Math.min(Math.round(retryDelayMs * 1.7), 15000);
    }

    if (this.generation === generation) {
      this.running = false;
    }
  }

  private async readStream(body: ReadableStream<Uint8Array>, signal: AbortSignal) {
    const reader = body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
      while (!signal.aborted) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() ?? '';

        for (const line of lines) {
          this.handleLine(line);
        }
      }
    } finally {
      reader.releaseLock();
    }
  }

  private handleLine(line: string) {
    const trimmed = line.trim();
    if (!trimmed.startsWith('data:')) return;

    const raw = trimmed.slice(5).trimStart();
    if (!raw) return;

    let event: AdminServerEvent;
    try {
      event = JSON.parse(raw) as AdminServerEvent;
    } catch {
      return;
    }
    if (!event.type) return;

    for (const listener of this.listeners) {
      listener(event);
    }
  }
}

const adminEventStream = new AdminEventStream();

export function subscribeAdminEvents(listener: AdminEventListener) {
  return adminEventStream.subscribe(listener);
}
