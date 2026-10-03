import { fireEvent, render, screen, within } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { ModelDistributionCard } from './DashboardPage';
import type { DashboardTrendResp } from '../shared/types';

vi.mock('@heroui/react', async () => import('../test/herouiMock'));
vi.mock('react-i18next', () => ({
  initReactI18next: { init: () => {}, type: '3rdParty' },
  useTranslation: () => ({ t: (key: string) => key }),
}));

const totals = { requests: 8, tokens: 123, actual_cost: 1.25, standard_cost: 2.5 };
const trend: DashboardTrendResp = {
  model_distribution: [{ model: 'gpt-test', ...totals }],
  user_ranking: [{ user_id: 42, email: 'user@example.com', ...totals }],
  account_distribution: [{ id: 31, name: 'upstream-credential', ...totals }],
  group_distribution: [{ id: 7, name: 'billing-group', ...totals }],
  token_trend: [],
  top_users: [],
};

describe('dashboard distribution dimensions', () => {
  it('switches between model, user, upstream credential and group without another query', () => {
    render(<ModelDistributionCard trend={trend} />);
    const cases = [
      ['dashboard.model_distribution', 'dashboard.model', 'gpt-test'],
      ['dashboard.user_ranking', 'dashboard.email', 'user@example.com'],
      ['dashboard.by_account', 'usage.upstream_credential', 'upstream-credential'],
      ['dashboard.by_group', 'groups.group', 'billing-group'],
    ] as const;
    for (const [title, header, name] of cases) {
      fireEvent.click(screen.getByRole('tab', { name: title }));
      const table = screen.getByRole('table', { name: title });
      expect(within(table).getByRole('columnheader', { name: header })).toBeInTheDocument();
      expect(within(table).getByText(name)).toBeInTheDocument();
      expect(within(table).getByText('123')).toBeInTheDocument();
      expect(within(table).getByText('1.25')).toBeInTheDocument();
      expect(within(table).getByText('2.50')).toBeInTheDocument();
      expect(screen.getByRole('tab', { name: title })).toHaveAttribute('aria-selected', 'true');
    }
  });

  it('uses IDs for missing names and keeps duplicate display names as separate rows', () => {
    render(<ModelDistributionCard trend={{ ...trend,
      account_distribution: [{ id: 0, name: '', ...totals }, { id: 1, name: '', ...totals }],
      group_distribution: [{ id: 7, name: 'shared', ...totals }, { id: 8, name: 'shared', ...totals }],
    }} />);
    fireEvent.click(screen.getByRole('tab', { name: 'dashboard.by_account' }));
    expect(screen.getByTitle('#0')).toBeInTheDocument();
    expect(screen.getByTitle('#1')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('tab', { name: 'dashboard.by_group' }));
    const table = screen.getByRole('table', { name: 'dashboard.by_group' });
    expect(within(table).getAllByText('shared')).toHaveLength(2);
    expect(within(table).getAllByRole('row')).toHaveLength(3);
  });

  it('renders an empty state for omitted dimensions', () => {
    render(<ModelDistributionCard trend={{ ...trend, account_distribution: undefined, group_distribution: undefined }} />);
    for (const title of ['dashboard.by_account', 'dashboard.by_group']) {
      fireEvent.click(screen.getByRole('tab', { name: title }));
      expect(within(screen.getByRole('table', { name: title })).getByText('common.no_data')).toBeInTheDocument();
    }
  });
});
