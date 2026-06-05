import { createContext, useContext, type ReactNode } from 'react';
import { createPortal } from 'react-dom';

const PageFooterContext = createContext<HTMLDivElement | null>(null);

export function PageFooterProvider({
  children,
  container,
}: {
  children: ReactNode;
  container: HTMLDivElement | null;
}) {
  return (
    <PageFooterContext.Provider value={container}>
      {children}
    </PageFooterContext.Provider>
  );
}

export function PageFooterPortal({ children }: { children: ReactNode }) {
  const container = useContext(PageFooterContext);
  if (!container) return null;
  return createPortal(children, container);
}
