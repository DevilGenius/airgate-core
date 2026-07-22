import { accountsApi } from '../../../shared/api/accounts';
import { getToken } from '../../../shared/api/client';
import type { ModelInfo } from '../../../shared/types';

export interface AccountTestRunResult {
  success: boolean;
  error?: string;
  firstEventMs?: number;
  durationMs?: number;
}

export interface AccountTestStreamHandlers {
  onStart?: (model: string) => void;
  onTextDelta?: (text: string) => void;
  onRawError?: (message: string) => void;
}

export function filterConnectivityTestModels(models: ModelInfo[] | null | undefined): ModelInfo[] {
  return (models ?? []).filter((model) => !model.id.toLowerCase().startsWith('gpt-image-'));
}

export async function runAccountConnectivityTest({
  accountId,
  fallbackError,
  handlers,
  modelId,
  signal,
}: {
  accountId: number;
  fallbackError: string;
  handlers?: AccountTestStreamHandlers;
  modelId: string;
  signal: AbortSignal;
}): Promise<AccountTestRunResult> {
  const url = new URL(accountsApi.testUrl(accountId), window.location.origin);
  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  const token = getToken();
  if (token) headers.Authorization = `Bearer ${token}`;

  const response = await fetch(url.toString(), {
    method: 'POST',
    headers,
    body: JSON.stringify({ model_id: modelId }),
    signal,
  });
  if (!response.ok || !response.body) {
    return { success: false, error: `HTTP ${response.status}` };
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let rawError = '';
  let result: AccountTestRunResult | null = null;

  const processLine = (line: string) => {
    const trimmed = line.trim();
    if (!trimmed) return;

    const payloads: string[] = [];
    let rawNonSSE = '';
    const dataPrefix = 'data: ';
    const firstDataIndex = trimmed.indexOf(dataPrefix);
    if (firstDataIndex < 0) {
      rawNonSSE = trimmed;
    } else {
      if (firstDataIndex > 0) rawNonSSE = trimmed.slice(0, firstDataIndex).trim();
      let index = firstDataIndex;
      while (index >= 0 && index < trimmed.length) {
        const payloadStart = index + dataPrefix.length;
        const nextIndex = trimmed.indexOf(dataPrefix, payloadStart);
        const payload = nextIndex >= 0
          ? trimmed.slice(payloadStart, nextIndex).trim()
          : trimmed.slice(payloadStart).trim();
        if (payload && payload !== '[DONE]') payloads.push(payload);
        index = nextIndex;
      }
    }

    if (rawNonSSE && payloads.length === 0) {
      const message = accountTestRawErrorMessage(rawNonSSE);
      if (message) {
        rawError = message;
        handlers?.onRawError?.(message);
      }
    }

    for (const payload of payloads) {
      try {
        const event = JSON.parse(payload);
        if (event.type === 'test_start') {
          handlers?.onStart?.(event.model ?? modelId);
          continue;
        }
        if (event.type === 'test_complete') {
          const timing = {
            ...(Number.isFinite(event.first_event_ms) ? { firstEventMs: event.first_event_ms as number } : {}),
            ...(Number.isFinite(event.duration_ms) ? { durationMs: event.duration_ms as number } : {}),
          };
          result = event.success
            ? { success: true, ...timing }
            : { success: false, error: event.error || fallbackError, ...timing };
          continue;
        }
        const delta = accountTestTextDelta(event);
        if (delta) handlers?.onTextDelta?.(delta);
      } catch {
        // 忽略插件流中的非 JSON 片段。
      }
    }
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split('\n');
    buffer = lines.pop() ?? '';
    for (const line of lines) processLine(line);
  }

  buffer += decoder.decode();
  if (buffer.trim()) processLine(buffer);
  return result ?? { success: false, error: rawError || fallbackError };
}

function accountTestRawErrorMessage(rawText: string): string {
  try {
    const raw = JSON.parse(rawText);
    if (raw?.error) {
      return typeof raw.error === 'string'
        ? raw.error
        : raw.error.message || JSON.stringify(raw.error);
    }
    if (raw?.message) {
      return raw.code ? `${raw.code}: ${raw.message}` : raw.message;
    }
  } catch {
    // 非 JSON 文本不直接展示，避免把上游流片段误当错误。
  }
  return '';
}

function accountTestTextDelta(event: any): string {
  if (event?.type === 'response.output_text.delta' && event?.delta) {
    return event.delta;
  }
  if (event?.object === 'chat.completion.chunk') {
    return event.choices?.[0]?.delta?.content ?? '';
  }
  if (event?.type === 'content_block_delta' && event?.delta?.type === 'text_delta') {
    return event.delta.text ?? '';
  }
  return '';
}
