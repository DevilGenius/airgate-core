import type { ReactNode } from 'react';
import { Toast, ToastQueue, type ToastContentValue } from '@heroui/react';

export type ToastType = 'success' | 'error' | 'danger' | 'warning' | 'info';

export interface ToastApi {
  toast: (type: ToastType, message: ReactNode, title?: ReactNode) => string;
}

const appToastQueue = new ToastQueue<ToastContentValue>({ maxVisibleToasts: 4 });

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

function notify(type: ToastType, message: ReactNode, title?: ReactNode): string {
  return appToastQueue.add({
    description: title ? message : undefined,
    title: title ?? message,
    variant: toastVariant(type),
  });
}

const toastApi: ToastApi = { toast: notify };

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
