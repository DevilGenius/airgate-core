const ALLOWED_TAGS = new Set([
  'a', 'b', 'blockquote', 'br', 'code', 'em', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'hr', 'i', 'li', 'ol', 'p', 'pre', 'span', 'strong', 'table', 'tbody', 'td',
  'th', 'thead', 'tr', 'u', 'ul',
]);

const GLOBAL_ATTRS = new Set(['aria-label', 'title']);
const LINK_ATTRS = new Set(['href', 'rel', 'target']);
const RICH_HTML_URL_ATTRS = new Set([
  'action', 'background', 'cite', 'formaction', 'href', 'poster', 'src', 'xlink:href',
]);
const DANGEROUS_RICH_HTML_TAGS = new Set([
  'applet', 'base', 'button', 'embed', 'form', 'iframe', 'input', 'link', 'math', 'meta',
  'object', 'option', 'script', 'select', 'svg', 'textarea',
]);
const DANGEROUS_CSS_PATTERN = /expression\s*\(|javascript\s*:|vbscript\s*:|@import|behavior\s*:|-moz-binding/i;
const CSS_URL_PATTERN = /url\s*\(\s*(['"]?)(.*?)\1\s*\)/gi;
const CSS_COMMENT_PATTERN = /\/\*[\s\S]*?\*\//g;
const CSS_ESCAPE_PATTERN = /\\([0-9a-f]{1,6})\s?|\\(.)/gi;

export function isSafeURL(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return false;
  if (trimmed.startsWith('/') && !trimmed.startsWith('//')) return true;
  try {
    const url = new URL(trimmed, window.location.origin);
    return url.protocol === 'https:' || url.protocol === 'http:' || url.protocol === 'mailto:';
  } catch {
    return false;
  }
}

function isSafeRichHTMLURL(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return false;
  if (trimmed.startsWith('#') || trimmed.startsWith('/') || trimmed.startsWith('./') || trimmed.startsWith('../')) {
    return true;
  }
  if (/^data:image\/(?:png|gif|jpe?g|webp|bmp);/i.test(trimmed)) return true;
  try {
    const url = new URL(trimmed, window.location.origin);
    return ['https:', 'http:', 'mailto:', 'tel:', 'cid:'].includes(url.protocol);
  } catch {
    return false;
  }
}

function hasDangerousCSS(value: string): boolean {
  const normalized = value
    .replace(CSS_COMMENT_PATTERN, '')
    .replace(CSS_ESCAPE_PATTERN, (_match, hex: string | undefined, escaped: string | undefined) => (
      hex ? String.fromCodePoint(Number.parseInt(hex, 16)) : (escaped ?? '')
    ))
    .replace(/[\u0000-\u001f\u007f]/g, '');
  if (DANGEROUS_CSS_PATTERN.test(normalized)) return true;

  CSS_URL_PATTERN.lastIndex = 0;
  for (const match of normalized.matchAll(CSS_URL_PATTERN)) {
    const url = match[2];
    if (!url || !isSafeRichHTMLURL(url)) return true;
  }
  return false;
}

function sanitizeRichHTMLStyle(value: string): string {
  return splitCSSDeclarations(value)
    .map((declaration) => declaration.trim())
    .filter((declaration) => declaration && !hasDangerousCSS(declaration))
    .join('; ');
}

function splitCSSDeclarations(value: string): string[] {
  const declarations: string[] = [];
  let start = 0;
  let depth = 0;
  let quote = '';
  let escaped = false;

  for (let index = 0; index < value.length; index += 1) {
    const character = value[index];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (character === '\\') {
      escaped = true;
      continue;
    }
    if (quote) {
      if (character === quote) quote = '';
      continue;
    }
    if (character === '"' || character === "'") {
      quote = character;
    } else if (character === '(') {
      depth += 1;
    } else if (character === ')' && depth > 0) {
      depth -= 1;
    } else if (character === ';' && depth === 0) {
      declarations.push(value.slice(start, index));
      start = index + 1;
    }
  }
  declarations.push(value.slice(start));
  return declarations;
}

function cleanElement(element: Element) {
  const tag = element.tagName.toLowerCase();
  if (!ALLOWED_TAGS.has(tag)) {
    element.replaceWith(...Array.from(element.childNodes));
    return;
  }

  for (const attr of Array.from(element.attributes)) {
    const name = attr.name.toLowerCase();
    const value = attr.value;
    const allowed = GLOBAL_ATTRS.has(name) || (tag === 'a' && LINK_ATTRS.has(name));
    if (!allowed || name.startsWith('on')) {
      element.removeAttribute(attr.name);
      continue;
    }
    if (tag === 'a' && name === 'href' && !isSafeURL(value)) {
      element.removeAttribute(attr.name);
    }
  }

  if (tag === 'a') {
    element.setAttribute('rel', mergeRelTokens(element.getAttribute('rel'), ['noopener', 'noreferrer']));
    const target = element.getAttribute('target');
    if (target) {
      const normalizedTarget = target.trim().toLowerCase();
      if (normalizedTarget === '_blank' || normalizedTarget === '_self') {
        element.setAttribute('target', normalizedTarget);
      } else {
        element.removeAttribute('target');
      }
    }
  }
}

function cleanRichHTMLElement(element: Element) {
  const tag = element.tagName.toLowerCase();
  if (DANGEROUS_RICH_HTML_TAGS.has(tag)) {
    element.remove();
    return;
  }

  if (tag === 'style') {
    if (hasDangerousCSS(element.textContent ?? '')) {
      element.remove();
      return;
    }
  }

  for (const attr of Array.from(element.attributes)) {
    const name = attr.name.toLowerCase();
    const value = attr.value;
    if (name.startsWith('on') || name === 'srcdoc') {
      element.removeAttribute(attr.name);
      continue;
    }
    if (name === 'style') {
      const style = sanitizeRichHTMLStyle(value);
      if (style) {
        element.setAttribute('style', style);
      } else {
        element.removeAttribute(attr.name);
      }
      continue;
    }
    if (RICH_HTML_URL_ATTRS.has(name) && !isSafeRichHTMLURL(value)) {
      element.removeAttribute(attr.name);
      continue;
    }
    if (name === 'srcset' && hasDangerousCSS(value)) {
      element.removeAttribute(attr.name);
    }
  }

  if (tag === 'a') {
    element.setAttribute('rel', mergeRelTokens(element.getAttribute('rel'), ['noopener', 'noreferrer']));
    const target = element.getAttribute('target');
    if (target && !['_blank', '_self'].includes(target.trim().toLowerCase())) {
      element.removeAttribute('target');
    }
  }
}

function mergeRelTokens(value: string | null, required: string[]): string {
  const tokens = (value ?? '').split(/\s+/).filter(Boolean);
  const seen = new Set(tokens.map((token) => token.toLowerCase()));
  for (const token of required) {
    if (!seen.has(token)) {
      tokens.push(token);
      seen.add(token);
    }
  }
  return tokens.join(' ');
}

function sanitize(
  html: string | undefined | null,
  cleaner: (element: Element) => void,
): string {
  if (!html || typeof document === 'undefined') return '';
  const template = document.createElement('template');
  template.innerHTML = html;
  const walker = document.createTreeWalker(template.content, NodeFilter.SHOW_ELEMENT);
  const elements: Element[] = [];
  while (walker.nextNode()) {
    elements.push(walker.currentNode as Element);
  }
  for (const element of elements.reverse()) {
    cleaner(element);
  }
  return template.innerHTML;
}

export type SanitizeHtmlOptions = {
  mode?: 'strict' | 'rich';
};

export function sanitizeHtml(
  html: string | undefined | null,
  options: SanitizeHtmlOptions = {},
): string {
  return sanitize(html, options.mode === 'rich' ? cleanRichHTMLElement : cleanElement);
}
