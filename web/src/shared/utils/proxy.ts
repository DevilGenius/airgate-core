import type { ProxyResp } from '../types';

export function formatProxySlot(slot: number | undefined, fallback: string) {
  return typeof slot === 'number' && Number.isInteger(slot) && slot >= 0 && slot <= 0xffff
    ? slot.toString(16).padStart(4, '0')
    : fallback;
}

export function parseProxySlotNumber(value: string) {
  const normalized = value.trim();
  if (!/^\d+$/.test(normalized)) return null;
  const slot = Number(normalized);
  return Number.isSafeInteger(slot) && slot >= 0 && slot <= 0xffff ? slot : null;
}

export function proxyEndpointLabel(proxy: ProxyResp) {
  const endpoint = `${proxy.protocol}://${proxy.address}:${proxy.port}`;
  if (proxy.mode === 'group') {
    return `${proxy.name} (${endpoint} · ${formatProxySlot(proxy.slot_start, '0000')}-${formatProxySlot(proxy.slot_end, 'ffff')})`;
  }
  return `${proxy.name} (${endpoint})`;
}
