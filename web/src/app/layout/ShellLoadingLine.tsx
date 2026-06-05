import { memo } from 'react';
import { useIsFetching } from '@tanstack/react-query';
import { useRouterState } from '@tanstack/react-router';
import { TopLoadingLine } from '../../shared/components/PageLoading';

export const ShellLoadingLine = memo(function ShellLoadingLine() {
  const routerStatus = useRouterState({ select: (s) => s.status });
  const blockingFetches = useIsFetching({
    predicate: (query) => (
      query.state.fetchStatus === 'fetching'
      && (query.meta as { globalLoading?: boolean } | undefined)?.globalLoading !== false
    ),
  });

  return <TopLoadingLine active={routerStatus === 'pending' || blockingFetches > 0} />;
});
