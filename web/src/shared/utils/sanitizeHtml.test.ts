import { describe, expect, it } from 'vitest';
import { effectiveDocUrl } from './docUrl';
import { sanitizeHtml } from './sanitizeHtml';

describe('sanitizeHtml', () => {
  it('removes scripts, event handlers, styles, and unsafe links', () => {
    const html = sanitizeHtml('<p onclick="x()" style="color:red">Hi<script>alert(1)</script><a href="javascript:alert(1)" target="_self">bad</a><a href="https://docs.example.com">ok</a></p>');

    expect(html).toContain('<p>Hi');
    expect(html).not.toContain('script');
    expect(html).not.toContain('onclick');
    expect(html).not.toContain('style=');
    expect(html).not.toContain('javascript:');
    expect(html).toContain('href="https://docs.example.com"');
    expect(html).toContain('rel="noopener noreferrer"');
  });
});

describe('effectiveDocUrl', () => {
  it('falls back for unsafe documentation URLs', () => {
    expect(effectiveDocUrl('javascript:alert(1)')).toEqual({ href: '/docs', isExternal: false });
    expect(effectiveDocUrl('/docs/custom')).toEqual({ href: '/docs/custom', isExternal: false });
    expect(effectiveDocUrl('https://docs.example.com')).toEqual({ href: 'https://docs.example.com', isExternal: true });
  });
});
