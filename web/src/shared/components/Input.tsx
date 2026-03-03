import { type InputHTMLAttributes, type TextareaHTMLAttributes, type SelectHTMLAttributes, forwardRef, type ReactNode } from 'react';

/* ==================== 共享样式 ==================== */

const inputBase =
  'block w-full rounded-[var(--ag-radius-md)] border border-[var(--ag-glass-border)] bg-[var(--ag-bg-surface)] px-3 py-2 text-sm text-[var(--ag-text)] placeholder-[var(--ag-text-tertiary)] transition-all duration-200 focus:outline-none focus:border-[var(--ag-border-focus)] focus:shadow-[0_0_0_3px_var(--ag-primary-subtle)] disabled:opacity-40 disabled:cursor-not-allowed';

const inputError =
  'border-[var(--ag-danger)] focus:border-[var(--ag-danger)] focus:shadow-[0_0_0_3px_var(--ag-danger-subtle)]';

/* ==================== Input ==================== */

interface InputProps extends InputHTMLAttributes<HTMLInputElement> {
  label?: string;
  error?: string;
  hint?: string;
  icon?: ReactNode;
}

export const Input = forwardRef<HTMLInputElement, InputProps>(
  ({ label, error, hint, icon, className = '', ...props }, ref) => {
    return (
      <div className="space-y-1.5">
        {label && (
          <label className="block text-xs font-medium text-[var(--ag-text-secondary)] uppercase tracking-wider">
            {label}
            {props.required && <span className="text-[var(--ag-danger)] ml-0.5">*</span>}
          </label>
        )}
        <div className="relative">
          {icon && (
            <div className="absolute left-3 top-1/2 -translate-y-1/2 text-[var(--ag-text-tertiary)]">
              {icon}
            </div>
          )}
          <input
            ref={ref}
            className={`${inputBase} ${error ? inputError : ''} ${icon ? 'pl-10' : ''} ${className}`}
            {...props}
          />
        </div>
        {error && <p className="text-xs text-[var(--ag-danger)]">{error}</p>}
        {hint && !error && <p className="text-xs text-[var(--ag-text-tertiary)]">{hint}</p>}
      </div>
    );
  },
);

Input.displayName = 'Input';

/* ==================== Textarea ==================== */

interface TextareaProps extends TextareaHTMLAttributes<HTMLTextAreaElement> {
  label?: string;
  error?: string;
}

export function Textarea({ label, error, className = '', ...props }: TextareaProps) {
  return (
    <div className="space-y-1.5">
      {label && (
        <label className="block text-xs font-medium text-[var(--ag-text-secondary)] uppercase tracking-wider">
          {label}
          {props.required && <span className="text-[var(--ag-danger)] ml-0.5">*</span>}
        </label>
      )}
      <textarea
        className={`${inputBase} min-h-[80px] resize-y ${error ? inputError : ''} ${className}`}
        {...props}
      />
      {error && <p className="text-xs text-[var(--ag-danger)]">{error}</p>}
    </div>
  );
}

/* ==================== Select ==================== */

interface SelectProps extends SelectHTMLAttributes<HTMLSelectElement> {
  label?: string;
  error?: string;
  options: Array<{ value: string; label: string }>;
}

export function Select({ label, error, options, className = '', ...props }: SelectProps) {
  return (
    <div className="space-y-1.5">
      {label && (
        <label className="block text-xs font-medium text-[var(--ag-text-secondary)] uppercase tracking-wider">
          {label}
          {props.required && <span className="text-[var(--ag-danger)] ml-0.5">*</span>}
        </label>
      )}
      <select
        className={`${inputBase} cursor-pointer appearance-none bg-[url('data:image/svg+xml;charset=utf-8,%3Csvg%20xmlns%3D%22http%3A//www.w3.org/2000/svg%22%20width%3D%2216%22%20height%3D%2216%22%20viewBox%3D%220%200%2024%2024%22%20fill%3D%22none%22%20stroke%3D%22%238892a8%22%20stroke-width%3D%222%22%3E%3Cpath%20d%3D%22m6%209%206%206%206-6%22/%3E%3C/svg%3E')] bg-no-repeat bg-[right_12px_center] pr-10 ${error ? inputError : ''} ${className}`}
        {...props}
      >
        {options.map((opt) => (
          <option key={opt.value} value={opt.value}>
            {opt.label}
          </option>
        ))}
      </select>
      {error && <p className="text-xs text-[var(--ag-danger)]">{error}</p>}
    </div>
  );
}
