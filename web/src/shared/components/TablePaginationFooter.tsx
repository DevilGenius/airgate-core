import {
  memo,
  startTransition,
  useCallback,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type PointerEvent,
  type FormEvent,
} from 'react';
import { flushSync } from 'react-dom';
import { ChevronDown, ChevronLeft, ChevronRight, RotateCw } from 'lucide-react';
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
  const jumpInputId = useId();
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
    <nav
      aria-label="分页"
      className={styles.pagination}
      data-long-pages={String(safeTotalPages).length > 6 || undefined}
    >
      <div className={styles.bar}>
        <div className={styles.summary}>
          <span className={styles.total}>
            {visibleTotalExact ? '共' : '至少'}
            <strong className={styles.number}>{visibleTotal.toLocaleString()}</strong>
            条
          </span>
          {showPageSize ? (
            <label className={styles.pageSize}>
              每页
              <span className={styles.selectControl}>
                <select
                  aria-label="每页数量"
                  className={styles.select}
                  value={selectedPageSize}
                  onChange={(event) => handlePageSizeChange(event.target.value)}
                >
                  {pageSizeOptions.map((size) => <option key={size} value={size}>{size}</option>)}
                </select>
                <ChevronDown aria-hidden="true" />
              </span>
              条
            </label>
          ) : null}
          {paginationStatus === 'preparing' || paginationStatus === 'failed' || isPaginationRefreshing || snapshotAt || onRefreshPagination ? (
            <div className={styles.metadata}>
              {paginationStatus === 'preparing' ? <span role="status">正在统计总页数…</span> : null}
              {isPaginationRefreshing ? <span role="status">正在更新页数…</span> : null}
              {paginationStatus === 'failed' ? <span className={styles.failed} role="status">页数暂不可用</span> : null}
              {snapshotAt || onRefreshPagination ? (
                <span className={styles.snapshot}>
                  {snapshotAt ? (
                    <time dateTime={snapshotAt} title="跳页按此时的记录排列，第一页显示最新记录；刷新可更新分页记录。">
                      截至 {new Date(snapshotAt).toLocaleTimeString([], { hour12: false })}
                    </time>
                  ) : null}
                  {onRefreshPagination ? (
                    <button
                      type="button"
                      className={styles.refresh}
                      onClick={onRefreshPagination}
                      disabled={paginationStatus === 'preparing' || isPaginationRefreshing}
                    >
                      <RotateCw aria-hidden="true" />
                      更新页数
                    </button>
                  ) : null}
                </span>
              ) : null}
            </div>
          ) : null}
        </div>

        <div className={styles.controls}>
          <div className={styles.navigation}>
            <button
              type="button"
              aria-label="上一页"
              aria-disabled={isPreviousDisabled}
              disabled={isPreviousDisabled}
              className={`${styles.button} ${styles.direction}`}
              onClick={() => handleNavClick(previousPage, isPreviousDisabled)}
              onPointerDown={(event) => handleNavPointerDown(event, previousPage, isPreviousDisabled)}
            >
              <ChevronLeft aria-hidden="true" />
              <span className={styles.directionLabel}>上一页</span>
            </button>
            <ol className={styles.pages} aria-label="页码">
              {paginationItems.map((item, index) => (
                <li key={item === '...' ? `ellipsis-${index}` : item}>
                  {item === '...' ? (
                    <span className={styles.ellipsis} aria-hidden="true">…</span>
                  ) : (
                    <button
                      type="button"
                      aria-label={`第 ${item} 页`}
                      aria-current={item === visiblePage ? 'page' : undefined}
                      className={`${styles.button} ${styles.pageButton}`}
                      onClick={() => handleLinkClick(item)}
                      onPointerDown={(event) => handleLinkPointerDown(event, item)}
                    >
                      {item}
                    </button>
                  )}
                </li>
              ))}
            </ol>
            <span className={styles.position} aria-label={`第 ${visiblePage} 页，共${totalExact ? '' : '至少'} ${safeTotalPages} 页`}>
              <strong className={styles.number}>{visiblePage}</strong>
              <span className={styles.pageCount}>/ {safeTotalPages}{totalExact ? '' : '+'}</span>
            </span>
            <button
              type="button"
              aria-label="下一页"
              aria-disabled={isNextDisabled}
              disabled={isNextDisabled}
              className={`${styles.button} ${styles.direction}`}
              onClick={() => handleNavClick(nextPage, isNextDisabled)}
              onPointerDown={(event) => handleNavPointerDown(event, nextPage, isNextDisabled)}
            >
              <span className={styles.directionLabel}>下一页</span>
              <ChevronRight aria-hidden="true" />
            </button>
          </div>
          {enablePageJump && totalExact ? (
            <form className={styles.jump} onSubmit={handleJump}>
              <label htmlFor={jumpInputId}>跳至</label>
              <input
                id={jumpInputId}
                className={styles.pageInput}
                aria-label="跳转页码"
                aria-invalid={Boolean(jumpError)}
                aria-describedby={jumpError ? `${jumpInputId}-error` : undefined}
                inputMode="numeric"
                autoComplete="off"
                size={Math.max(3, String(safeTotalPages).length)}
                value={jumpPage}
                onChange={(event) => { setJumpPage(event.target.value); setJumpError(''); }}
              />
              <span>页</span>
              <button type="submit" className={`${styles.button} ${styles.go}`}>跳转</button>
              {jumpError ? <span id={`${jumpInputId}-error`} className={styles.error} role="alert">{jumpError}</span> : null}
            </form>
          ) : null}
        </div>
      </div>

    </nav>
  );
});
