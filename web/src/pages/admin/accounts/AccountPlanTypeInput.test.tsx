import { useState } from 'react';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it } from 'vitest';
import { AccountPlanTypeInput, ACCOUNT_PLAN_PRESETS } from './AccountPlanTypeInput';
import styles from './AccountPlanTypeInput.module.css';
import componentCSS from './AccountPlanTypeInput.module.css?raw';
import sharedFieldCSS from '../../../styles/components/forms.css?raw';

function Harness({ initial = '', disabled = false }: { initial?: string; disabled?: boolean }) {
  const [value, setValue] = useState(initial);
  return <AccountPlanTypeInput value={value} onChange={setValue} label="Account plan" disabled={disabled} />;
}

describe('editable account plan dropdown', () => {
  it.each(['global-first', 'component-first'])('keeps the arrow inside the input without a separate field surface (%s)', (order) => {
    const scopedCSS = componentCSS.replace(/\.([A-Za-z_][\w-]*)/g, (selector, name: string) => styles[name] ? '.' + styles[name] : selector);
    const stylesheet = document.createElement('style');
    stylesheet.textContent = order === 'global-first'
      ? sharedFieldCSS + scopedCSS
      : scopedCSS + sharedFieldCSS;
    document.head.append(stylesheet);
    try {
      render(<div id="root"><div className="ag-elevation-modal"><Harness initial="plus" /></div></div>);
      const input = screen.getByRole('combobox');
      const trigger = screen.getByRole('button', { name: 'Account plan' });
      const group = input.parentElement!;
      expect(trigger.parentElement).toBe(group);
      expect(getComputedStyle(group).position).toBe('relative');
      expect(getComputedStyle(input).boxSizing).toBe('border-box');
      const buttonStyle = getComputedStyle(trigger);
      expect(buttonStyle.position).toBe('absolute');
      expect(buttonStyle.top).toBe('1px');
      expect(buttonStyle.bottom).toBe('1px');
      expect(buttonStyle.backgroundColor).toBe('rgba(0, 0, 0, 0)');
      expect(buttonStyle.boxShadow).toBe('none');
      expect(buttonStyle.transform).toBe('none');
      expect(buttonStyle.translate).toBe('none');
    } finally {
      stylesheet.remove();
    }
  });

  it.each(['plus', 'custom-existing-plan'])('opens the full preset list with existing value %s', async (initial) => {
    const user = userEvent.setup();
    render(<Harness initial={initial} />);
    await user.click(screen.getByRole('button', { name: 'Account plan' }));
    const options = await screen.findAllByRole('option');
    expect(options.map((option) => option.textContent)).toEqual([...ACCOUNT_PLAN_PRESETS]);
    expect(screen.getByRole('combobox')).toHaveValue(initial);
  });

  it('selects the upstream ProLite value, allows edits, and reopens all options', async () => {
    const user = userEvent.setup();
    render(<Harness initial="plus" />);
    await user.click(screen.getByRole('button', { name: 'Account plan' }));
    await user.click(await screen.findByRole('option', { name: 'self_serve_business_prolite' }));
    const input = screen.getByRole('combobox');
    expect(input).toHaveValue('self_serve_business_prolite');
    await user.clear(input);
    await user.type(input, 'my_custom_plan');
    await user.tab();
    expect(input).toHaveValue('my_custom_plan');
    await user.click(screen.getByRole('button', { name: 'Account plan' }));
    expect(await screen.findAllByRole('option')).toHaveLength(ACCOUNT_PLAN_PRESETS.length);
    await user.click(screen.getByRole('option', { name: 'free' }));
    expect(input).toHaveValue('free');
  });

  it('disables both typing and opening in an inactive bulk field', async () => {
    render(<Harness initial="plus" disabled />);
    expect(screen.getByRole('combobox')).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Account plan' })).toBeDisabled();
  });
});
