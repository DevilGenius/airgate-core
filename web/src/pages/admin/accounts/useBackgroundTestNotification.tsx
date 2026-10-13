import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { toastQueue, useToast, type ToastType } from '../../../shared/ui/feedback/Toast';

export function useBackgroundTestNotification({
  open, running, finished, title, type, onRestore,
}: {
  open: boolean;
  running: boolean;
  finished: boolean;
  title: string;
  type: ToastType;
  onRestore: () => void;
}) {
  const { t } = useTranslation();
  const { toast } = useToast();
  const restoreRef = useRef(onRestore);
  restoreRef.current = onRestore;

  useEffect(() => {
    if (open) return;
    const key = toast(finished ? type : 'info', (
      <button className="w-full text-left underline underline-offset-4" type="button" onClick={() => restoreRef.current()}>
        {t(finished ? 'accounts.test_background_finished' : running
          ? 'accounts.test_background_running' : 'accounts.test_background_restore')}
      </button>
    ), title, { timeout: 0 });
    return () => toastQueue.close(key);
  }, [open, running, finished, title, type, t, toast]);
}
