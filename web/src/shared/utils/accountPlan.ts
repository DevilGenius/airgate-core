export function normalizeAccountPlan(value?: string) {
  const raw = (value || '').trim();
  if (!raw) return '';
  const tokens = raw.toLowerCase().split(/[^a-z0-9]+/).filter(Boolean);
  const compact = tokens.join('');
  if (compact.endsWith('prolite')) return 'prolite';
  for (const token of tokens) {
    if (['free', 'plus', 'pro', 'team', 'k12', 'enterprise'].includes(token)) return token;
    if (token === 'professional') return 'pro';
  }
  return raw.toLowerCase();
}
