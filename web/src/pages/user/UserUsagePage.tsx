import { lazy, Suspense, type ComponentType } from 'react';
import { PageLoading } from '../../shared/components/PageLoading';

type UserUsageContentModule = {
  default: ComponentType;
};

let userUsageContentPromise: Promise<UserUsageContentModule> | undefined;

function loadUserUsageContent() {
  userUsageContentPromise ??= import('./UserUsageContent').then((module) => ({ default: module.default }));
  return userUsageContentPromise;
}

export function preloadUserUsageContent() {
  return loadUserUsageContent();
}

const UserUsageContent = lazy(loadUserUsageContent);

function UserUsagePagePlaceholder() {
  return (
    <>
      <PageLoading />
      <div className="min-h-[320px]" aria-hidden="true" />
    </>
  );
}

export default function UserUsagePage() {
  return (
    <Suspense fallback={<UserUsagePagePlaceholder />}>
      <UserUsageContent />
    </Suspense>
  );
}
