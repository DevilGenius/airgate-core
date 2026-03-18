import { useState } from 'react';

export function usePagination(defaultPageSize = 20) {
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(defaultPageSize);

  const handlePageSizeChange = (size: number) => {
    setPageSize(size);
    setPage(1);
  };

  return { page, setPage, pageSize, setPageSize: handlePageSizeChange };
}
