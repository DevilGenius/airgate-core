import { Table as HeroTable } from '@heroui/react';

export function TableLoadingRow({
  colSpan,
  minHeight = 220,
}: {
  colSpan: number;
  minHeight?: number;
}) {
  return (
    <HeroTable.Row id="loading">
      <HeroTable.Cell colSpan={colSpan}>
        <div aria-busy="true" aria-live="polite" className="w-full" style={{ minHeight }}>
          <span className="sr-only">Loading</span>
        </div>
      </HeroTable.Cell>
    </HeroTable.Row>
  );
}
