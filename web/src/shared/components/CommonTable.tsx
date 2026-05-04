import type { ComponentProps, CSSProperties, ReactNode } from 'react';
import { Table as HeroTable } from '@heroui/react';

type HeroTableContentProps = ComponentProps<typeof HeroTable.Content>;

interface CommonTableProps {
  ariaLabel: string;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
  contentProps?: Omit<HeroTableContentProps, 'aria-label' | 'children' | 'className' | 'style'>;
  contentStyle?: CSSProperties;
  footer?: ReactNode;
  minWidth?: number | string;
  scrollClassName?: string;
}

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
  scrollClassName,
}: CommonTableProps) {
  const resolvedContentStyle = minWidth == null
    ? contentStyle
    : {
        minWidth,
        ...contentStyle,
      };

  return (
    <HeroTable className={cx('ag-resource-table', className)} variant="primary">
      <HeroTable.ScrollContainer className={cx('ag-resource-table-scroll', scrollClassName)}>
        <HeroTable.Content
          {...contentProps}
          aria-label={ariaLabel}
          className={cx('ag-resource-table-content', contentClassName)}
          style={resolvedContentStyle}
        >
          {children}
        </HeroTable.Content>
      </HeroTable.ScrollContainer>
      {footer ? <HeroTable.Footer>{footer}</HeroTable.Footer> : null}
    </HeroTable>
  );
}

export const CommonTable = Object.assign(CommonTableRoot, {
  Body: HeroTable.Body,
  Cell: HeroTable.Cell,
  Column: HeroTable.Column,
  Header: HeroTable.Header,
  Row: HeroTable.Row,
});
