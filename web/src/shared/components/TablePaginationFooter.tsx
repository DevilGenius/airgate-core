import {
  memo,
  startTransition,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent,
  type FormEvent,
} from 'react';
import { flushSync } from 'react-dom';
import { Pagination } from '@heroui/react';
import { SimpleSelect } from './SimpleSelect';
import { DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS, getPaginationItems } from '../utils/pagination';
import styles from './TablePaginationFooter.module.css';

interface TablePaginationFooterProps {
  hasMore?: boolean;
  page: number;
  pageSize?: number;
  pageSizeOptions?: readonly number[];
  setPage: (page: number) => void;
  setPageSize?: (pageSize: number) => void;
  summaryTotal?: number;
  summaryTotalExact?: boolean;
  total: number;
  totalExact?: boolean;
  totalPages: number;
  enablePageJump?: boolean;
  paginationStatus?: 'preparing' | 'ready' | 'failed';
  isPaginationRefreshing?: boolean;
  snapshotAt?: string;
  onRefreshPagination?: () => void;
}

function scheduleAfterPaint(callback: () => void) {
  if (typeof window === 'undefined' || typeof window.requestAnimationFrame !== 'function') {
    callback();
    return () => {};
  }

  let timerId: number | undefined;
  const frameId = window.requestAnimationFrame(() => {
    timerId = window.setTimeout(callback, 0);
  });

  return () => {
    window.cancelAnimationFrame(frameId);
    if (timerId !== undefined) window.clearTimeout(timerId);
  };
}

export const TablePaginationFooter = memo(function TablePaginationFooter({
  hasMore,
  page,
  pageSize,
  pageSizeOptions = DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS,
  setPage,
  setPageSize,
  summaryTotal,
  summaryTotalExact,
  total,
  totalExact = true,
  totalPages,
  enablePageJump = false,
  paginationStatus,
  isPaginationRefreshing = false,
  snapshotAt,
  onRefreshPagination,
}: TablePaginationFooterProps) {
  const [displayPage, setDisplayPage] = useState(page);
  const [displayPageSize, setDisplayPageSize] = useState(pageSize);
  const [jumpPage, setJumpPage] = useState(String(page));
  const [jumpError, setJumpError] = useState('');
  const cancelPageCommitRef = useRef<(() => void) | null>(null);
  const cancelPageSizeCommitRef = useRef<(() => void) | null>(null);
  const handledPointerPageRef = useRef(false);
  const pointerClickGuardTimerRef = useRef<number | null>(null);
  const safeTotalPages = totalExact
    ? Math.max(totalPages, 1)
    : Math.max(totalPages, page + (hasMore ? 1 : 0), 1);
  const visiblePage = totalExact || displayPage !== page
    ? Math.min(Math.max(displayPage, 1), safeTotalPages)
    : page;
  const canGoNext = visiblePage < safeTotalPages;
  const showPageSize = pageSize != null && setPageSize != null;
  const selectedPageSize = displayPageSize == null ? '' : String(displayPageSize);
  const visibleTotal = summaryTotal ?? total;
  const visibleTotalExact = summaryTotalExact ?? totalExact;
  const pageSizeItems = useMemo(
    () => pageSizeOptions.map((size) => ({ id: String(size), label: String(size) })),
    [pageSizeOptions],
  );
  const paginationItems = useMemo(
    () => getPaginationItems(visiblePage, safeTotalPages),
    [visiblePage, safeTotalPages],
  );

  useEffect(() => {
    setDisplayPage(page);
    setJumpPage(String(page));
    setJumpError('');
  }, [page]);

  useEffect(() => {
    setDisplayPageSize(pageSize);
  }, [pageSize]);

  useEffect(() => () => {
    cancelPageCommitRef.current?.();
    cancelPageSizeCommitRef.current?.();
    if (pointerClickGuardTimerRef.current != null) {
      window.clearTimeout(pointerClickGuardTimerRef.current);
    }
  }, []);

  const commitPageAfterPaint = useCallback((nextPage: number) => {
    cancelPageCommitRef.current?.();
    cancelPageCommitRef.current = scheduleAfterPaint(() => {
      cancelPageCommitRef.current = null;
      startTransition(() => {
        setPage(nextPage);
      });
    });
  }, [setPage]);

  const handlePageChange = useCallback((nextPage: number) => {
    const boundedPage = Math.min(Math.max(nextPage, 1), safeTotalPages);
    if (boundedPage === page && boundedPage === visiblePage) return;

    if (totalExact || boundedPage !== page) {
      flushSync(() => {
        setDisplayPage(boundedPage);
      });
    }
    commitPageAfterPaint(boundedPage);
  }, [commitPageAfterPaint, page, safeTotalPages, totalExact, visiblePage]);

  const handlePointerPageChange = useCallback((nextPage: number) => {
    handledPointerPageRef.current = true;
    if (pointerClickGuardTimerRef.current != null) {
      window.clearTimeout(pointerClickGuardTimerRef.current);
    }
    pointerClickGuardTimerRef.current = window.setTimeout(() => {
      handledPointerPageRef.current = false;
      pointerClickGuardTimerRef.current = null;
    }, 600);
    handlePageChange(nextPage);
  }, [handlePageChange]);

  const handleClickPageChange = useCallback((nextPage: number) => {
    if (handledPointerPageRef.current) {
      handledPointerPageRef.current = false;
      if (pointerClickGuardTimerRef.current != null) {
        window.clearTimeout(pointerClickGuardTimerRef.current);
        pointerClickGuardTimerRef.current = null;
      }
      return;
    }
    handlePageChange(nextPage);
  }, [handlePageChange]);

  const handleNavPointerDown = useCallback((event: PointerEvent<HTMLButtonElement>, nextPage: number, disabled: boolean) => {
    if (disabled || event.button !== 0) return;
    handlePointerPageChange(nextPage);
  }, [handlePointerPageChange]);

  const handleNavClick = useCallback((nextPage: number, disabled: boolean) => {
    if (disabled) return;
    handleClickPageChange(nextPage);
  }, [handleClickPageChange]);

  const handleLinkPointerDown = useCallback((event: PointerEvent<HTMLButtonElement>, nextPage: number) => {
    if (event.button !== 0) return;
    handlePointerPageChange(nextPage);
  }, [handlePointerPageChange]);

  const handleLinkClick = useCallback((nextPage: number) => {
    handleClickPageChange(nextPage);
  }, [handleClickPageChange]);

  const handlePageSizeChange = useCallback((key: string) => {
    if (!setPageSize) return;
    const nextPageSize = Number(key);
    if (!Number.isFinite(nextPageSize) || nextPageSize <= 0) return;

    cancelPageCommitRef.current?.();
    cancelPageCommitRef.current = null;
    flushSync(() => {
      setDisplayPage(1);
      setDisplayPageSize(nextPageSize);
    });
    cancelPageSizeCommitRef.current?.();
    cancelPageSizeCommitRef.current = scheduleAfterPaint(() => {
      cancelPageSizeCommitRef.current = null;
      startTransition(() => {
        setPageSize(nextPageSize);
      });
    });
  }, [setPageSize]);
  const previousPage = Math.max(1, visiblePage - 1);
  const nextPage = visiblePage + 1;
  const isPreviousDisabled = visiblePage <= 1;
  const isNextDisabled = !canGoNext;
  const handleJump = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const target = Number(jumpPage.trim());
    if (!/^\d+$/.test(jumpPage.trim()) || !Number.isSafeInteger(target) || target < 1 || target > safeTotalPages) {
      setJumpError(`请输入 1 至 ${safeTotalPages} 之间的页码`);
      return;
    }
    setJumpError('');
    handlePageChange(target);
  };

  return (
    <Pagination className={`ag-table-pagination${paginationStatus ? ` ${styles.withPageIndex}` : ''}`} size="sm">
      <Pagination.Summary className="ag-table-pagination-summary">
        <span>{visibleTotalExact ? '共' : '至少'}</span>
        <span className="ag-table-pagination-number">{visibleTotal.toLocaleString()}</span>
        <span>条</span>
        <span className="ag-table-pagination-separator" aria-hidden="true" />
        <span>第</span>
        <span className="ag-table-pagination-number">{visiblePage}</span>
        <span>/</span>
        <span className="ag-table-pagination-number">{safeTotalPages}</span>
        <span>{totalExact ? '页' : '页+'}</span>
        {showPageSize ? (
          <div className="ag-table-page-size">
            <span>每页</span>
            <SimpleSelect
              ariaLabel="每页数量"
              className="ag-table-page-size-select"
              items={pageSizeItems.map((item) => ({ key: item.id, label: item.label }))}
              selectedKey={selectedPageSize}
              selectedLabel={selectedPageSize}
              triggerClassName="ag-table-page-size-trigger"
              popoverClassName="ag-table-page-size-list"
              itemClassName="ag-table-page-size-option"
              onSelectionChange={handlePageSizeChange}
            />
            <span>条</span>
          </div>
        ) : null}
        {paginationStatus === 'preparing' ? <span className={styles.status} role="status">正在统计总页数…</span> : null}
        {isPaginationRefreshing ? <span className={styles.status} role="status">正在更新页数…</span> : null}
        {paginationStatus === 'failed' ? <span className={styles.status} role="status">页数暂不可用</span> : null}
        {snapshotAt ? (
          <time className={styles.status} dateTime={snapshotAt} title="跳页按此时的记录排列，第一页显示最新记录；刷新可更新分页记录。">
            截至 {new Date(snapshotAt).toLocaleTimeString([], { hour12: false })}
          </time>
        ) : null}
        {onRefreshPagination ? <button type="button" className={styles.refresh} onClick={onRefreshPagination} disabled={paginationStatus === 'preparing' || isPaginationRefreshing}>更新页数</button> : null}
      </Pagination.Summary>

      <div className={styles.controls}>
      <Pagination.Content>
        <Pagination.Item>
          <button
            type="button"
            aria-disabled={isPreviousDisabled}
            className="pagination__link pagination__link--nav ag-table-pagination-nav"
            onClick={() => handleNavClick(previousPage, isPreviousDisabled)}
            onPointerDown={(event) => handleNavPointerDown(event, previousPage, isPreviousDisabled)}
          >
            <Pagination.PreviousIcon />
            <span>上一页</span>
          </button>
        </Pagination.Item>
        {paginationItems.map((item, index) =>
          item === '...' ? (
            <Pagination.Item key={`ellipsis-${index}`}>
              <Pagination.Ellipsis />
            </Pagination.Item>
          ) : (
            <Pagination.Item key={item}>
              <button
                type="button"
                aria-current={item === visiblePage ? 'page' : undefined}
                className="pagination__link ag-table-pagination-page-link"
                data-active={item === visiblePage ? 'true' : undefined}
                onClick={() => handleLinkClick(item)}
                onPointerDown={(event) => handleLinkPointerDown(event, item)}
              >
                {item}
              </button>
            </Pagination.Item>
          ),
        )}
        <Pagination.Item>
          <button
            type="button"
            aria-disabled={isNextDisabled}
            className="pagination__link pagination__link--nav ag-table-pagination-nav"
            onClick={() => handleNavClick(nextPage, isNextDisabled)}
            onPointerDown={(event) => handleNavPointerDown(event, nextPage, isNextDisabled)}
          >
            <span>下一页</span>
            <Pagination.NextIcon />
          </button>
        </Pagination.Item>
      </Pagination.Content>
      {enablePageJump && totalExact ? (
        <form className={styles.jump} onSubmit={handleJump}>
          <label className={styles.jumpLabel}>
            跳至
            <input className={styles.pageInput} aria-label="跳转页码" aria-invalid={Boolean(jumpError)} inputMode="numeric" value={jumpPage} onChange={(event) => { setJumpPage(event.target.value); setJumpError(''); }} />
            页
          </label>
          <button type="submit" className={styles.go}>跳转</button>
          {jumpError ? <span className={styles.error} role="alert">{jumpError}</span> : null}
        </form>
      ) : null}
      </div>
    </Pagination>
  );
});
