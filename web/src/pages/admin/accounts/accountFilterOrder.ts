const PLATFORM_ORDER: Readonly<Record<string, number>> = { openai: 0, claude: 1, kiro: 2 };

// Use identifiers instead of translated labels or API response order.
export function compareAccountFilterPlatforms(a: string, b: string): number {
  const left = a.toLowerCase();
  const right = b.toLowerCase();
  const rank = (PLATFORM_ORDER[left] ?? 3) - (PLATFORM_ORDER[right] ?? 3);
  if (rank) return rank;
  if (left !== right) return left < right ? -1 : 1;
  return a === b ? 0 : a < b ? -1 : 1;
}
