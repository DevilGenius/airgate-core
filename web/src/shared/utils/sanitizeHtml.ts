const ALLOWED_TAGS = new Set([
  'a', 'b', 'blockquote', 'br', 'code', 'em', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
  'hr', 'i', 'li', 'ol', 'p', 'pre', 'span', 'strong', 'table', 'tbody', 'td',
  'th', 'thead', 'tr', 'u', 'ul',
]);

const GLOBAL_ATTRS = new Set(['aria-label', 'title']);
const LINK_ATTRS = new Set(['href', 'rel', 'target']);

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

export function sanitizeHtml(html: string | undefined | null): string {
  if (!html || typeof document === 'undefined') return '';
  const template = document.createElement('template');
  template.innerHTML = html;
  const walker = document.createTreeWalker(template.content, NodeFilter.SHOW_ELEMENT);
  const elements: Element[] = [];
  while (walker.nextNode()) {
    elements.push(walker.currentNode as Element);
  }
  for (const element of elements.reverse()) {
    cleanElement(element);
  }
  return template.innerHTML;
}
