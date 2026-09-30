import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it } from 'vitest';
import { renderAccountTypeFilterOption } from './accountTypeFilterSupport';
import { AccountPlanTag } from './AccountPlanTag';

afterEach(cleanup);
const Glyph = () => <svg data-testid="plugin-platform-icon" />;

it('reuses platform icons and the table tag without an OAuth icon, preserving full plan names', () => {
  render(<>{renderAccountTypeFilterOption({ id: 'oauth_plan:openai:prolite', label: 'OpenAI ProLite',
    platform: 'openai', platformLabel: 'OpenAI', planLabel: 'ProLite' }, Glyph)}
    <AccountPlanTag label="ProLite" planKey="prolite" />
  </>);
  expect(screen.getByTestId('plugin-platform-icon')).toBeInTheDocument();
  expect(screen.getByRole('img', { name: 'OpenAI' })).toBeInTheDocument();
  expect(screen.queryByRole('img', { name: 'OAuth' })).not.toBeInTheDocument();
  const tags = [screen.getByText('ProLite'), screen.getByText('ProL')];
  expect(tags).toHaveLength(2);
  expect(tags[0]?.className).toBe(tags[1]?.className);
  expect(tags[0]).toHaveAttribute('data-plan', 'prolite');
  expect(tags[0]).toHaveAttribute('title', 'ProLite');
  expect(screen.getByLabelText('OpenAI ProLite')).toHaveAttribute('title', 'OpenAI ProLite');
});

it('keeps unknown plans identifiable without inventing a known plan color', () => {
  render(<>{renderAccountTypeFilterOption({ id: 'oauth_plan:kiro:unknown', label: 'Kiro Unknown',
    platform: 'kiro', platformLabel: 'Kiro', planLabel: 'Unknown' }, Glyph)}</>);
  expect(screen.getByText('Unknown')).toHaveAttribute('data-plan', 'unknown');
  expect(screen.getByText('Unknown')).toHaveAttribute('aria-label', 'Unknown');
  expect(screen.getByRole('img', { name: 'Kiro' })).toBeInTheDocument();
});

it('limits filter tags to eight characters while keeping table tags at four', () => {
  render(<>{renderAccountTypeFilterOption({ id: 'oauth_plan:custom:enterprise', label: 'Custom Enterprise',
    platform: 'custom', platformLabel: 'Custom', planLabel: 'Enterprise' }, Glyph)}
    <AccountPlanTag label="Enterprise" planKey="enterprise" />
  </>);
  expect(screen.getByText('Enterpri')).toHaveAttribute('title', 'Enterprise');
  expect(screen.getByText('Ente')).toHaveAttribute('title', 'Enterprise');
});
