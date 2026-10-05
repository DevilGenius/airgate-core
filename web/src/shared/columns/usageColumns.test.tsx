import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { TFunction } from 'i18next';
import { createUsageClientColumn, formatResponseTimeMs } from './usageColumns';
import type { CustomerUsageLogResp, UsageLogResp } from '../types';

const CHROME_UA = 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36';

describe('shared usage response time formatting', () => {
  it.each([
    [0, '-'],
    [-1, '-'],
    [Number.NaN, '-'],
    [Number.POSITIVE_INFINITY, '-'],
    [1, '1ms'],
    [120, '120ms'],
    [999, '999ms'],
    [1000, '1.00s'],
    [1500, '1.50s'],
    [60_000, '1m'],
    [61_000, '1m 1s'],
    [119_600, '2m'],
  ] as const)('formats %s ms as %s for both usage pages', (value, expected) => {
    expect(formatResponseTimeMs(value)).toBe(expected);
  });
});

describe('shared usage client column', () => {
  const t = ((key: string) => key) as unknown as TFunction;
  const column = createUsageClientColumn(t);
  const renderClient = (row: UsageLogResp | CustomerUsageLogResp) => (
    renderToStaticMarkup(<div>{column.render(row)}</div>)
  );

  it('keeps the metadata the admin and the user usage table share', () => {
    expect([column.key, column.title, column.width, column.hideOnMobile])
      .toEqual(['client', 'usage.client', '168px', true]);
  });

  it('stacks the IP above the trimmed user agent and keeps the raw UA in the tooltip', () => {
    const html = renderClient({ ip_address: '203.0.113.7', user_agent: CHROME_UA } as UsageLogResp);

    expect(html).toContain('203.0.113.7');
    expect(html).toContain('>Chrome/124.0 Safari/537.36</span>');
    expect(html).not.toContain('>Mozilla');
    expect(html).toContain(CHROME_UA);
    expect(html.indexOf('203.0.113.7')).toBeLessThan(html.indexOf('Chrome/124.0'));
  });

  it('clamps the user agent to a single line', () => {
    const html = renderClient({ ip_address: '203.0.113.7', user_agent: CHROME_UA } as UsageLogResp);

    expect(html).toContain('-webkit-line-clamp:1');
    expect(html).toContain('overflow-wrap:anywhere');
  });

  it('falls back to a dash for missing values', () => {
    const html = renderClient({} as UsageLogResp);

    expect(html.match(/>-</g)).toHaveLength(2);
    expect(html).toContain('-');
  });

  it('renders the same client rows for end customer (API key login) records', () => {
    const html = renderClient({ id: 1, ip_address: '198.51.100.9', user_agent: CHROME_UA } as CustomerUsageLogResp);

    expect(html).toContain('198.51.100.9');
    expect(html).toContain('>Chrome/124.0 Safari/537.36</span>');
  });

  it('renders placeholders when the record carried no client data', () => {
    const html = renderClient({ id: 1 } as CustomerUsageLogResp);

    expect(html.match(/>-</g)).toHaveLength(2);
  });
});
