import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import { accountIdentityDisplay, AccountIdentityCell } from './AccountIdentityCell';
import { AccountPlatformTypeIcons } from './AccountPlatformTypeIcons';

afterEach(cleanup);
const account: AccountResp = {
  id: 1, name: 'test-account', email: null,
  platform: 'openai', type: 'oauth', credentials: {}, model_policy: {},
  state: 'active', priority: 0, max_concurrency: 4, current_concurrency: 0,
  rate_multiplier: 1, upstream_is_pool: false, group_ids: [],
  created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
};
const now = Date.parse('2026-10-01T00:00:00Z');

describe('core account identity', () => {
  it('does not display cognition status in the identity column', () => {
    const glyph = () => <svg />;
    const row: AccountResp = { ...account, extra: { cognition_degraded: true } };
    const { rerender } = render(<AccountIdentityCell row={row} platformLabel="OpenAI" PlatformGlyph={glyph} />);
    expect(screen.queryByText('降智')).not.toBeInTheDocument();
    rerender(<AccountIdentityCell row={{ ...row, extra: { cognition_degraded: false } }} platformLabel="OpenAI" PlatformGlyph={glyph} />);
    expect(screen.queryByText('降智')).not.toBeInTheDocument();
  });
  it('reuses modal icons during output updates and handles a replaced platform glyph', () => {
    const glyph = vi.fn(() => <svg />);
    const { rerender } = render(<AccountPlatformTypeIcons PlatformGlyph={glyph} platformLabel="OpenAI" type="api_key" />);
    expect(screen.getByRole('img', { name: 'API Key' })).toHaveAttribute('title', 'API Key');
    rerender(<AccountPlatformTypeIcons PlatformGlyph={glyph} platformLabel="OpenAI" type="api_key" />);
    expect(glyph).toHaveBeenCalledTimes(1);
    const replacement = vi.fn(() => <svg />);
    rerender(<AccountPlatformTypeIcons PlatformGlyph={replacement} platformLabel="OpenAI" type="api_key" />);
    expect(replacement).toHaveBeenCalledTimes(1);
    expect(screen.queryByText('API Key')).not.toBeInTheDocument();
  });

  it.each([['plus', 'Free'], ['pro', 'Free'], ['team', 'Team'], ['Self_serve_business_prolite', 'ProLite']])('preserves %s expiry rules', (plan, label) => {
    const result = accountIdentityDisplay({ ...account, credentials: { plan_type: plan, subscription_active_until: '2020-01-01' } }, undefined, now);
    expect(result.planLabel).toBe(label);
    expect(result.planKey).toBe(label.toLowerCase());
  });

  it('preserves the pool plan override and free metadata fallback', () => {
    expect(accountIdentityDisplay({ ...account, credentials: { plan_type: 'plus', subscription_active_until: '2020-01-01' } }, 'pro', now).planLabel).toBe('Pro');
    expect(accountIdentityDisplay({ ...account, credentials: { email: 'test@example.invalid' } }).planLabel).toBe('Free');
    expect(accountIdentityDisplay(account).planLabel).toBe('');
  });

  it('keeps agent identity, API key aliases and Claude details distinguishable', () => {
    expect(accountIdentityDisplay({ ...account, credentials: { auth_mode: 'agent_identity' } })).toMatchObject({ type: 'identity', typeLabel: 'Identity' });
    expect(accountIdentityDisplay({ ...account, type: 'api_key' })).toMatchObject({ type: 'apikey', typeLabel: 'API Key' });
    expect(accountIdentityDisplay({ ...account, platform: 'claude', credentials: { access_token: 'test', expires_at: '2030-01-01' } }).typeLabel).toBe('OAuth · 令牌 · 过期时间：2030-01-01');
  });

  it('keeps platform, type and different plan tags in fixed slots', () => {
    const glyph = () => <svg />;
    const row: AccountResp = { ...account, id: 1, name: 'Test', email: null, model_policy: {}, state: 'active', priority: 0, max_concurrency: 4, current_concurrency: 0, rate_multiplier: 1, upstream_is_pool: false, group_ids: [], created_at: '', updated_at: '' };
    const { rerender } = render(<AccountIdentityCell row={{ ...row, credentials: { plan_type: 'plus' } }} platformLabel="OpenAI" PlatformGlyph={glyph} />);
    const platform = screen.getByRole('img', { name: 'OpenAI' });
    const type = screen.getByRole('img', { name: 'OAuth' });
    const group = platform.parentElement!;
    expect(group.children[0]).toBe(platform);
    expect(group.children[1]).toBe(type);
    expect(platform.className).toBe(type.className);
    expect(group.children[2]).toBe(screen.getByText('Plus'));
    expect(screen.getByText('Plus')).toHaveAttribute('data-plan', 'plus');
    rerender(<AccountIdentityCell row={{ ...row, credentials: { plan_type: 'enterprise' } }} platformLabel="OpenAI" PlatformGlyph={glyph} />);
    expect(group.children[2]).toBe(screen.getByText('Ente'));
    expect(screen.getByText('Ente')).toHaveAttribute('title', 'Enterprise');
    rerender(<AccountIdentityCell row={row} platformLabel="OpenAI" PlatformGlyph={glyph} />);
    expect(group.children).toHaveLength(2);
  });
});
