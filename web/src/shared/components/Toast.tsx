import { useState, createContext, useContext, useCallback, type ReactNode } from 'react';

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
    }, 3000);
  }, []);

  return (
    <ToastContext.Provider value={{ toast }}>
      {children}
      {/* Toast 容器 */}
      <div className="fixed top-4 right-4 z-[100] flex flex-col gap-2">
        {messages.map((msg) => (
          <ToastItem key={msg.id} {...msg} onClose={() => setMessages((prev) => prev.filter((m) => m.id !== msg.id))} />
        ))}
      </div>
    </ToastContext.Provider>
  );
}

const typeStyles = {
  success: 'bg-green-50 border-green-200 text-green-800',
  error: 'bg-red-50 border-red-200 text-red-800',
  info: 'bg-blue-50 border-blue-200 text-blue-800',
};

function ToastItem({ type, message, onClose }: ToastMessage & { onClose: () => void }) {
  return (
    <div className={`flex items-center gap-2 px-4 py-3 rounded-lg border shadow-sm text-sm animate-in slide-in-from-right ${typeStyles[type]}`}>
      <span className="flex-1">{message}</span>
      <button onClick={onClose} className="opacity-50 hover:opacity-100">&times;</button>
    </div>
  );
}

export function useToast() {
  return useContext(ToastContext);
}
