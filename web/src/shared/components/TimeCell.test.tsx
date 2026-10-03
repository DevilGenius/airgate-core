import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import { TimeCell } from './TimeCell';

describe('shared time cell', () => {
  it('renders time above the smaller secondary date using monitor styling', () => {
    const html = renderToStaticMarkup(<TimeCell value="2026-10-03T09:08:07" />);
    expect(html).toContain('flex-col justify-center gap-1 text-left');
    expect(html).toContain('text-[13px] font-medium leading-none text-text">09:08:07');
    expect(html).toContain('text-[11px] leading-none text-text-tertiary">2026/10/3');
    expect(html).toContain('title="2026/10/3 09:08:07"');
    expect(html.indexOf('>09:08:07')).toBeLessThan(html.indexOf('>2026/10/3'));
  });

  it.each([undefined, '', 'invalid-date'])('handles missing or invalid timestamps: %s', (value) => {
    const html = renderToStaticMarkup(<TimeCell value={value} />);
    expect(html).toContain(`>${value || '-'}</span>`);
    expect(html).not.toContain('text-[11px]');
  });
});
