import { useSyncExternalStore, type ReactNode } from 'react';
import { Toast, ToastQueue, type ToastContentValue } from '@heroui/react';

export type ToastType = 'success' | 'error' | 'danger' | 'warning' | 'info';

export interface ToastOptions {
  timeout?: number;
}

export interface ToastApi {
  toast: (type: ToastType, message: ReactNode, title?: ReactNode, options?: ToastOptions) => string;
  updateToast: (key: string, message: ReactNode, timeout?: number) => boolean;
}

const updatedToastTimeout = 4_000;
const appToastQueue = new ToastQueue<ToastContentValue>({ maxVisibleToasts: 4 });
const toastMessageStores = new Map<string, ToastMessageStore>();
const toastCloseTimers = new Map<string, ReturnType<typeof setTimeout>>();

interface ToastMessageStore {
  getSnapshot: () => ReactNode;
  subscribe: (listener: () => void) => () => void;
  update: (message: ReactNode) => void;
}

function createToastMessageStore(initialMessage: ReactNode): ToastMessageStore {
  let message = initialMessage;
  const listeners = new Set<() => void>();
  return {
    getSnapshot: () => message,
    subscribe: (listener) => {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    update: (nextMessage) => {
      message = nextMessage;
      listeners.forEach((listener) => listener());
    },
  };
}

function MutableToastMessage({ store }: { store: ToastMessageStore }) {
  return useSyncExternalStore(store.subscribe, store.getSnapshot, store.getSnapshot);
}

function toastVariant(type: ToastType): ToastContentValue['variant'] {
  switch (type) {
    case 'success':
      return 'success';
    case 'warning':
      return 'warning';
    case 'error':
    case 'danger':
      return 'danger';
    case 'info':
    default:
      return 'default';
  }
}

function toastContent(type: ToastType, store: ToastMessageStore, title?: ReactNode): ToastContentValue {
  const message = <MutableToastMessage store={store} />;
  return {
    description: title ? message : undefined,
    title: title ?? message,
    variant: toastVariant(type),
  };
}

function clearToastCloseTimer(key: string) {
  const timer = toastCloseTimers.get(key);
  if (timer !== undefined) clearTimeout(timer);
  toastCloseTimers.delete(key);
}

function notify(type: ToastType, message: ReactNode, title?: ReactNode, options?: ToastOptions): string {
  const store = createToastMessageStore(message);
  let key = '';
  key = appToastQueue.add(toastContent(type, store, title), {
    timeout: options?.timeout,
    onClose: () => {
      clearToastCloseTimer(key);
      toastMessageStores.delete(key);
    },
  });
  toastMessageStores.set(key, store);
  return key;
}

function updateToast(key: string, message: ReactNode, timeout = updatedToastTimeout): boolean {
  const store = toastMessageStores.get(key);
  if (!store) return false;
  store.update(message);
  clearToastCloseTimer(key);
  if (timeout > 0) {
    const timer = setTimeout(() => {
      toastCloseTimers.delete(key);
      appToastQueue.close(key);
    }, timeout);
    toastCloseTimers.set(key, timer);
  }
  return true;
}

const toastApi: ToastApi = { toast: notify, updateToast };

export function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <>
      {children}
      <Toast.Provider className="ag-toast-region" placement="bottom end" queue={appToastQueue} width={420} />
    </>
  );
}

export function useToast(): ToastApi {
  return toastApi;
}

export { appToastQueue as toastQueue };
