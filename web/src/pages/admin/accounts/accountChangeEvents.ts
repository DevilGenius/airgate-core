import { getAdminServerNowMs, type AdminServerEvent } from '../../../shared/api/adminEvents';
import type { AccountResp } from '../../../shared/types';

// Threshold edits are rare configuration changes. Statistics remain list-only.
export function accountChangeNeedsListRefresh(event: AdminServerEvent): boolean {
  return typeof event.model_downgrade_threshold === 'number' && Number.isFinite(event.model_downgrade_threshold);
}

export function isAccountState(value: unknown): value is AccountResp['state'] {
  return value === 'active' || value === 'rate_limited' || value === 'degraded' || value === 'disabled';
}

export function applyAccountChangeToRow(row: AccountResp, event: AdminServerEvent): AccountResp {
  let next = row;
  if (accountChangeNeedsListRefresh(event)) next = { ...next, model_downgrade_threshold: event.model_downgrade_threshold };
  if (typeof event.max_concurrency === 'number' && Number.isFinite(event.max_concurrency)) next = { ...next, max_concurrency: event.max_concurrency };
  if (isAccountState(event.state)) {
    next = {
      ...next,
      state: event.state,
      state_until: typeof event.state_until === 'string' && event.state_until ? event.state_until : undefined,
      error_msg: typeof event.error_msg === 'string' && event.error_msg ? event.error_msg : undefined,
    };
  }

  if (event.family_cooldown_action === 'clear') {
    if (next.family_cooldowns?.length) {
      next = { ...next, family_cooldowns: undefined };
    }
  } else if (
    event.family_cooldown_action === 'upsert'
    && typeof event.family === 'string'
    && event.family
    && typeof event.family_until === 'string'
    && event.family_until
  ) {
    const familyCooldown = {
      family: event.family,
      until: event.family_until,
      ...(typeof event.family_reason === 'string' && event.family_reason
        ? { reason: event.family_reason }
        : {}),
      ...(typeof event.family_duration_ms === 'number' && event.family_duration_ms > 0
        ? { duration_ms: event.family_duration_ms }
        : {}),
    };
    const now = getAdminServerNowMs();
    next = {
      ...next,
      family_cooldowns: [
        ...(next.family_cooldowns ?? []).filter(
          (item) => item.family !== event.family && Date.parse(item.until) > now,
        ),
        familyCooldown,
      ],
    };
  }

  if (typeof event.priority === 'number' && Number.isFinite(event.priority)) {
    next = { ...next, priority: event.priority };
  }
  if (typeof event.scheduling_weight === 'number' && Number.isFinite(event.scheduling_weight)) {
    next = { ...next, scheduling_weight: event.scheduling_weight };
  }
  if (event.cognition && (event.cognition.degraded === null || typeof event.cognition.degraded === 'boolean')) {
    const extra = { ...next.extra };
    if (event.cognition.degraded === null) delete extra.cognition_degraded;
    else extra.cognition_degraded = event.cognition.degraded;
    next = { ...next, extra };
  }
  return next;
}
