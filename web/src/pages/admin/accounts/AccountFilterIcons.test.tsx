import { useState } from 'react';
import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { AccountFilterIcons } from './AccountFilterIcons';
import { MonitorMultiFilterSelect } from '../monitor/MonitorFilterSelect';
import * as registry from '../../../app/plugin-frontend-registry';

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it('keeps icon order and selections stable when refreshed platforms arrive in a different order', () => {
  const options = ['zeta', 'kiro', 'claude', 'openai', 'alpha'].map((key) => ({ key, label: key }));
  const props = { selectedPlatforms: ['kiro', 'openai'], selectedTypes: ['apikey', 'oauth'],
    platformLabel: '平台', typeLabel: '账号类型', onPlatformsChange: vi.fn(), onTypesChange: vi.fn() };
  const { rerender } = render(<AccountFilterIcons {...props} platforms={options} />);
  const before = screen.getAllByRole('button');
  expect(before.map((button) => button.getAttribute('aria-label'))).toEqual(['openai', 'claude', 'kiro', 'alpha', 'zeta', 'OAuth', 'API Key']);
  rerender(<AccountFilterIcons {...props} platforms={[...options].reverse()} />);
  const after = screen.getAllByRole('button');
  after.forEach((button, index) => expect(button).toBe(before[index]));
  expect(screen.getAllByRole('button', { pressed: true }).map((button) => button.getAttribute('aria-label'))).toEqual(['openai', 'kiro', 'OAuth', 'API Key']);
  expect(options.map(({ key }) => key)).toEqual(['zeta', 'kiro', 'claude', 'openai', 'alpha']);
});

it('supports independent multiple platform and authentication selections, including keyboard toggles', async () => {
  const subscribe = vi.spyOn(registry, 'onPlatformIconChange');
  const user = userEvent.setup();
  function Harness() {
    const [platforms, setPlatforms] = useState<string[]>([]);
    const [types, setTypes] = useState<string[]>([]);
    return <AccountFilterIcons platforms={[{ key: 'openai', label: 'OpenAI' }, { key: 'claude', label: 'Claude' }]}
      selectedPlatforms={platforms} selectedTypes={types} platformLabel="平台" typeLabel="账号类型"
      onPlatformsChange={setPlatforms} onTypesChange={setTypes} />;
  }
  render(<Harness />);
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(4);
  await user.click(screen.getByRole('button', { name: 'OpenAI' }));
  await user.click(screen.getByRole('button', { name: 'OAuth' }));
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(2);
  const oauth = screen.getByRole('button', { name: 'OAuth' });
  oauth.focus();
  await user.keyboard(' ');
  expect(oauth).toHaveAttribute('aria-pressed', 'true');
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(3);
  expect(subscribe).toHaveBeenCalledTimes(1);
});

it('selects asynchronously loaded platforms by default without rewriting stored subsets', () => {
  const props = { selectedPlatforms: [] as string[], selectedTypes: [] as string[], platformLabel: '平台', typeLabel: '账号类型',
    onPlatformsChange: vi.fn(), onTypesChange: vi.fn() };
  const { rerender } = render(<AccountFilterIcons {...props} platforms={[]} />);
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(2);
  const platforms = [{ key: 'openai', label: 'OpenAI' }, { key: 'claude', label: 'Claude' }];
  rerender(<AccountFilterIcons {...props} platforms={platforms} />);
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(4);
  rerender(<AccountFilterIcons {...props} platforms={platforms} selectedPlatforms={['claude']} selectedTypes={['apikey']} />);
  expect(screen.getByRole('button', { name: 'OpenAI' })).toHaveAttribute('aria-pressed', 'false');
  expect(screen.getByRole('button', { name: 'OAuth' })).toHaveAttribute('aria-pressed', 'false');
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(2);
  expect(props.onPlatformsChange).not.toHaveBeenCalled();
  expect(props.onTypesChange).not.toHaveBeenCalled();
});

it('keeps the combined menu open while selecting across group, status, and proxy sections', async () => {
  const user = userEvent.setup();
  function Harness() {
    const [selection, setSelection] = useState<Record<string, string[]>>({});
    return <MonitorMultiFilterSelect allLabel="全部筛选" ariaLabel="分组 / 状态 / 代理" label="筛选" collapsePlaceholder
      groups={[{ id: 'group', label: '所属分组', options: [{ id: '1', label: '默认分组' }, { id: '2', label: '测试分组' }] },
        { id: 'state', label: '账号状态', options: [{ id: 'active', label: '正常' }] },
        { id: 'proxy', label: '代理', options: [{ id: '3', label: '代理一' }] }].map((group) => ({ ...group, selectedValues: selection[group.id] ?? [] }))}
      onClear={() => setSelection({})}
      onToggle={(group, value) => setSelection((previous) => {
        const selected = previous[group] ?? [];
        return { ...previous, [group]: selected.includes(value) ? selected.filter((item) => item !== value) : [...selected, value] };
      })} />;
  }
  render(<Harness />);
  await user.click(screen.getByRole('button', { name: '分组 / 状态 / 代理' }));
  // JSDOM does not implement the native popover top layer.
  screen.getByRole('menu', { hidden: true }).parentElement?.removeAttribute('popover');
  for (const name of ['默认分组', '测试分组', '正常', '代理一']) {
    await user.click(screen.getByRole('menuitemcheckbox', { name }));
  }
  expect(within(screen.getByRole('menu')).getAllByRole('menuitemcheckbox', { checked: true })).toHaveLength(4);
  await user.click(screen.getByRole('menuitemcheckbox', { name: '全部筛选' }));
  expect(screen.getByRole('menuitemcheckbox', { name: '全部筛选' })).toHaveAttribute('aria-checked', 'true');
  expect(within(screen.getByRole('menu')).getAllByRole('menuitemcheckbox', { checked: true })).toHaveLength(1);
});
