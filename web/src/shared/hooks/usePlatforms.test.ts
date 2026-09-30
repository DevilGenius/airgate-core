import { describe, expect, it } from 'vitest';
import { parseOAuthPlanFilters } from './usePlatforms';

describe('typed account plan filters', () => {
 it('uses the explicit API field for known plans and Unknown', () => {
  expect(parseOAuthPlanFilters('kiro', 'Kiro', [
   { key: 'power', label: 'Power', credential_key: 'plan_type', match: 'contains', matches: ['Power'] },
   { key: 'unknown', label: 'Unknown', credential_key: 'plan_type', match: 'unknown' },
  ]).map(({ id, planLabel }) => ({ id, planLabel }))).toEqual([
   { id: 'oauth_plan:kiro:power', planLabel: 'Power' },
   { id: 'oauth_plan:kiro:unknown', planLabel: 'Unknown' },
  ]);
 });
 it('does not invent vendor plans when the API field is absent', () => {
  expect(parseOAuthPlanFilters('claude', 'Claude', undefined)).toEqual([]);
 });
});
