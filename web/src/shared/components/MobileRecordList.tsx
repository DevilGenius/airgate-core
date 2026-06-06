import { memo, type ReactNode } from 'react';
import { TableEmptyState } from './TablePage';

export interface MobileRecordField {
  className?: string;
  label: ReactNode;
  value: ReactNode;
}

export interface MobileRecordItem {
  className?: string;
  id: string | number;
  title: ReactNode;
  description?: ReactNode;
  meta?: ReactNode;
  fields?: MobileRecordField[];
  actions?: ReactNode;
}

export const MobileRecordList = memo(function MobileRecordList({
  emptyDescription,
  emptyTitle,
  isLoading,
  items,
  loading,
}: {
  emptyDescription?: string;
  emptyTitle: string;
  isLoading?: boolean;
  items: MobileRecordItem[];
  loading?: ReactNode;
}) {
  if (isLoading) {
    return (
      <div className="ag-mobile-record-list">
        {loading ?? Array.from({ length: 4 }, (_, index) => (
          <div className="ag-mobile-record-card ag-mobile-record-card--loading" key={index}>
            <div className="skeleton h-4 w-2/3" />
            <div className="skeleton h-3 w-1/2" />
            <div className="skeleton h-8 w-full" />
          </div>
        ))}
      </div>
    );
  }

  if (items.length === 0) {
    return <TableEmptyState title={emptyTitle} description={emptyDescription} />;
  }

  return (
    <div className="ag-mobile-record-list">
      {items.map((item) => (
        <article className={['ag-mobile-record-card', item.className].filter(Boolean).join(' ')} key={item.id}>
          <div className="ag-mobile-record-head">
            <div className="min-w-0">
              <div className="ag-mobile-record-title">{item.title}</div>
              {item.description ? <div className="ag-mobile-record-description">{item.description}</div> : null}
            </div>
            {item.meta ? <div className="ag-mobile-record-meta">{item.meta}</div> : null}
          </div>
          {item.fields?.length ? (
            <dl className="ag-mobile-record-fields">
              {item.fields.map((field, index) => (
                <div className={['ag-mobile-record-field', field.className].filter(Boolean).join(' ')} key={index}>
                  <dt>{field.label}</dt>
                  <dd>{field.value}</dd>
                </div>
              ))}
            </dl>
          ) : null}
          {item.actions ? <div className="ag-mobile-record-actions">{item.actions}</div> : null}
        </article>
      ))}
    </div>
  );
});
