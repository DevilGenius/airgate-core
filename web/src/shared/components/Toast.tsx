import { useState, createContext, useContext, useCallback, type ReactNode } from 'react';
import { CheckCircle, XCircle, Info, X } from 'lucide-react';

type ToastType = 'success' | 'error' | 'info';

interface ToastMessage {
  id: number;
  type: ToastType;
  message: string;
}

interface ToastContextType {
  toast: (type: ToastType, message: string) => void;
}

const ToastContext = createContext<ToastContextType>({ toast: () => {} });

let nextId = 0;

export function ToastProvider({ children }: { children: ReactNode }) {
  const [messages, setMessages] = useState<ToastMessage[]>([]);

  const toast = useCallback((type: ToastType, message: string) => {
    const id = nextId++;
    setMessages((prev) => [...prev, { id, type, message }]);
    setTimeout(() => {
      setMessages((prev) => prev.filter((m) => m.id !== id));
    }, 4000);
  }, []);

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      <div className="fixed top-5 right-5 z-[100] flex flex-col gap-2.5 pointer-events-none">
        {messages.map((msg) => (
          <ToastItem
            key={msg.id}
            {...msg}
            onClose={() => setMessages((prev) => prev.filter((m) => m.id !== msg.id))}
          />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

const typeConfig = {
  success: { icon: CheckCircle, border: 'var(--ag-success)', color: 'var(--ag-success)' },
  error: { icon: XCircle, border: 'var(--ag-danger)', color: 'var(--ag-danger)' },
  info: { icon: Info, border: 'var(--ag-primary)', color: 'var(--ag-primary)' },
};

function ToastItem({ type, message, onClose }: ToastMessage & { onClose: () => void }) {
  const config = typeConfig[type];
  const Icon = config.icon;

  return (
    <div
      className="pointer-events-auto flex items-center gap-3 px-4 py-3 rounded-[var(--ag-radius-md)] border shadow-[var(--ag-shadow-md)] min-w-[280px] max-w-[400px]"
      style={{
        borderColor: config.border,
        background: 'var(--ag-bg-elevated)',
        animation: 'ag-slide-down 0.25s ease-out',
      }}
    >
      <Icon className="w-4 h-4 flex-shrink-0" style={{ color: config.color }} />
      <span className="flex-1 text-sm text-[var(--ag-text)]">{message}</span>
      <button
        onClick={onClose}
        className="flex-shrink-0 text-[var(--ag-text-tertiary)] hover:text-[var(--ag-text)] transition-colors"
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

export function useToast() {
  return useContext(ToastContext);
}
