import type {
  ComponentPropsWithoutRef,
  CSSProperties,
  HTMLAttributes,
  ReactElement,
  ReactNode,
  TdHTMLAttributes,
  ThHTMLAttributes,
} from 'react';
import { Children, isValidElement } from 'react';
import { MobileRecordList, type MobileRecordItem } from './MobileRecordList';

type NativeTableProps = ComponentPropsWithoutRef<'table'>;

const DEFAULT_ROW_HEADER_COLUMN_IDS = new Set(['action', 'email', 'id', 'name', 'user_id']);

interface CommonTableProps {
  ariaLabel: string;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
  contentProps?: Omit<NativeTableProps, 'aria-label' | 'children' | 'className' | 'style'>;
  contentStyle?: CSSProperties;
  footer?: ReactNode;
  minWidth?: number | string;
  mobileCards?: boolean;
  scrollClassName?: string;
  scrollOverlay?: ReactNode;
}

type CommonTableColumnProps = Omit<ThHTMLAttributes<HTMLTableCellElement>, 'id'> & {
  id?: string;
  isRowHeader?: boolean;
};
type CommonTableRowProps = Omit<HTMLAttributes<HTMLTableRowElement>, 'id'> & {
  id?: string | number;
};

function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ');
}

function CommonTableRoot({
  ariaLabel,
  children,
  className,
  contentClassName,
  contentProps,
  contentStyle,
  footer,
  minWidth,
  mobileCards = true,
  scrollClassName,
  scrollOverlay,
}: CommonTableProps) {
  const resolvedContentStyle = minWidth == null
    ? contentStyle
    : {
        minWidth,
        ...contentStyle,
      };

  const mobileItems = mobileCards ? buildMobileItems(children) : [];
  return (
    <div className={cx('ag-resource-table', className)}>
      <div
        className={cx(
          'ag-resource-table-scroll',
          mobileItems.length > 0 && 'ag-resource-table-desktop',
          scrollClassName,
        )}
        data-slot="wrapper"
      >
        {scrollOverlay}
        <table
          {...contentProps}
          aria-label={ariaLabel}
          className={cx('ag-resource-table-content', contentClassName)}
          data-slot="table"
          style={resolvedContentStyle}
        >
          {children}
        </table>
      </div>
      {mobileItems.length > 0 ? (
        <div className="ag-resource-table-mobile">
          <MobileRecordList emptyTitle="暂无数据" items={mobileItems} />
        </div>
      ) : null}
      {footer ? (
        <div className="table__footer" data-slot="table-footer">
          {footer}
        </div>
      ) : null}
    </div>
  );
}

function elementChildren(element: ReactElement<{ children?: ReactNode }>) {
  return Children.toArray(element.props.children);
}

function isComponentElement<P extends { children?: ReactNode }>(
  value: ReactNode,
  component: unknown,
): value is ReactElement<P> {
  return isValidElement(value) && value.type === component;
}

function textValue(value: ReactNode): string {
  if (typeof value === 'string' || typeof value === 'number') return String(value);
  if (Array.isArray(value)) return value.map(textValue).join('');
  if (isValidElement<{ children?: ReactNode }>(value)) return textValue(value.props.children);
  return '';
}

function buildMobileItems(children: ReactNode): MobileRecordItem[] {
  const sections = Children.toArray(children);
  const header = sections.find((child) => isComponentElement(child, CommonTableHeader));
  const body = sections.find((child) => isComponentElement(child, CommonTableBody));
  if (!header || !body) return [];

  const labels = elementChildren(header).map((column) => {
    if (!isComponentElement(column, CommonTableColumn)) return '';
    return textValue(column.props.children).trim();
  });
  if (labels.length === 0) return [];

  const items: MobileRecordItem[] = [];
  for (const row of elementChildren(body)) {
    if (!isComponentElement<CommonTableRowProps>(row, CommonTableRow)) continue;

    const cells = elementChildren(row)
      .filter((cell): cell is ReactElement<TdHTMLAttributes<HTMLTableCellElement>> => isComponentElement(cell, CommonTableCell));
    if (cells.length < 2) continue;

    const [firstCell, ...restCells] = cells;
    if (!firstCell) continue;

    items.push({
      id: row.props.id ?? textValue(firstCell.props.children),
      title: firstCell.props.children,
      fields: restCells.map((cell, index) => ({
        label: labels[index + 1] || '',
        value: cell.props.children,
      })).filter((field) => field.label),
    });
  }

  return items;
}

function CommonTableHeader({ children, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <thead data-slot="thead" {...props}>
      <tr data-slot="tr">{children}</tr>
    </thead>
  );
}

function CommonTableBody({ children, ...props }: HTMLAttributes<HTMLTableSectionElement>) {
  return (
    <tbody data-slot="tbody" {...props}>
      {children}
    </tbody>
  );
}

function CommonTableColumn({ id, isRowHeader, children, ...props }: CommonTableColumnProps) {
  const shouldMarkRowHeader =
    isRowHeader ?? (typeof id === 'string' && DEFAULT_ROW_HEADER_COLUMN_IDS.has(id));

  return (
    <th
      {...props}
      data-row-header={shouldMarkRowHeader || undefined}
      data-slot="th"
      id={id}
      scope="col"
    >
      {children}
    </th>
  );
}

function CommonTableRow({ id, children, ...props }: CommonTableRowProps) {
  return (
    <tr {...props} data-key={id == null ? undefined : String(id)} data-slot="tr">
      {children}
    </tr>
  );
}

function CommonTableCell({ children, ...props }: TdHTMLAttributes<HTMLTableCellElement>) {
  return (
    <td {...props} data-slot="td">
      {children}
    </td>
  );
}

export const CommonTable = Object.assign(CommonTableRoot, {
  Body: CommonTableBody,
  Cell: CommonTableCell,
  Column: CommonTableColumn,
  Header: CommonTableHeader,
  Row: CommonTableRow,
});
