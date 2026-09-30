import { describe, expect, it } from 'vitest';
import { parseOAuthPlanFilters } from './usePlatforms';

describe('OAuth plan filter labels', () => {
  it('maps an installed empty-only None declaration to Unknown', () => {
    const filters = parseOAuthPlanFilters('openai', 'OpenAI', JSON.stringify([
      { key: 'none', label: 'None', credential_key: 'plan_type', match: 'empty' },
      { key: 'plus', label: 'Plus', matches: ['plus'] },
    ]));
    expect(filters.map(({ id, planLabel }) => ({ id, planLabel }))).toEqual([
      { id: 'oauth_plan:openai:unknown', planLabel: 'Unknown' },
      { id: 'oauth_plan:openai:plus', planLabel: 'Plus' },
    ]);
  });

  it('accepts the current Unknown declaration without changing other filters', () => {
    expect(parseOAuthPlanFilters('openai', 'OpenAI', JSON.stringify([
      { key: 'unknown', label: 'Unknown', match: 'unknown' },
    ]))[0]).toMatchObject({ id: 'oauth_plan:openai:unknown', planLabel: 'Unknown' });
    expect(parseOAuthPlanFilters('openai', 'OpenAI', 'invalid')).toEqual([]);
  });
});
