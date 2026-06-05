import { lazy, Suspense, type ComponentType } from 'react';
import { PageLoading } from '../../shared/components/PageLoading';

type AccountsPageContentModule = {
  default: ComponentType;
};

let accountsPageContentPromise: Promise<AccountsPageContentModule> | undefined;

function loadAccountsPageContent() {
  accountsPageContentPromise ??= import('./AccountsPageContent').then((module) => ({ default: module.default }));
  return accountsPageContentPromise;
}

export function preloadAccountsPageContent() {
  return loadAccountsPageContent();
}

const AccountsPageContent = lazy(loadAccountsPageContent);

function AccountsPagePlaceholder() {
  return (
    <>
      <PageLoading />
      <div className="min-h-[320px]" aria-hidden="true" />
    </>
  );
}

export default function AccountsPage() {
  return (
    <Suspense fallback={<AccountsPagePlaceholder />}>
      <AccountsPageContent />
    </Suspense>
  );
}
