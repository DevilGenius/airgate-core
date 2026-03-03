import { type ButtonHTMLAttributes, type ReactNode } from 'react';
import { Loader2 } from 'lucide-react';

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: 'primary' | 'secondary' | 'danger' | 'ghost' | 'outline';
  size?: 'sm' | 'md' | 'lg';
  loading?: boolean;
  icon?: ReactNode;
}

const variantStyles: Record<string, string> = {
  primary:
    'bg-[var(--ag-primary)] text-white hover:bg-[var(--ag-primary-hover)] shadow-[0_0_12px_var(--ag-primary-glow)] hover:shadow-[0_0_20px_var(--ag-primary-glow)]',
  secondary:
    'bg-[var(--ag-bg-surface)] text-[var(--ag-text)] border border-[var(--ag-glass-border)] hover:bg-[var(--ag-bg-hover)] hover:border-[var(--ag-border-focus)]',
  danger:
    'bg-[var(--ag-danger)] text-white hover:brightness-110 shadow-[0_0_12px_var(--ag-danger-subtle)]',
  ghost:
    'text-[var(--ag-text-secondary)] hover:text-[var(--ag-text)] hover:bg-[var(--ag-bg-hover)]',
  outline:
    'border border-[var(--ag-primary)] text-[var(--ag-primary)] bg-transparent hover:bg-[var(--ag-primary-subtle)]',
};

const sizeStyles: Record<string, string> = {
  sm: 'h-8 px-3 text-xs gap-1.5',
  md: 'h-9 px-4 text-sm gap-2',
  lg: 'h-11 px-6 text-sm gap-2',
};

export function Button({
  variant = 'primary',
  size = 'md',
  loading = false,
  icon,
  children,
  disabled,
  className = '',
  ...props
}: ButtonProps) {
  return (
    <button
      className={`inline-flex items-center justify-center rounded-[var(--ag-radius-md)] font-medium transition-all duration-200 cursor-pointer disabled:opacity-40 disabled:cursor-not-allowed disabled:shadow-none ${variantStyles[variant]} ${sizeStyles[size]} ${className}`}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? (
        <Loader2 className="h-4 w-4 animate-spin" />
      ) : icon ? (
        <span className="flex-shrink-0">{icon}</span>
      ) : null}
      {children}
    </button>
  );
}
