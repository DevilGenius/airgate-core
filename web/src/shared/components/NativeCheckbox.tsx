import { useLayoutEffect, useRef, type CSSProperties, type ReactNode } from 'react';

interface NativeCheckboxProps {
  ariaLabel?: string;
  children?: ReactNode;
  className?: string;
  contentClassName?: string;
  contentStyle?: CSSProperties;
  controlClassName?: string;
  isDisabled?: boolean;
  isIndeterminate?: boolean;
  isSelected: boolean;
  name?: string;
  onChange: (selected: boolean) => void;
}

export function NativeCheckbox({
  ariaLabel,
  children,
  className,
  contentClassName,
  contentStyle,
  controlClassName,
  isDisabled = false,
  isIndeterminate = false,
  isSelected,
  name,
  onChange,
}: NativeCheckboxProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const rootClassName = [
    'ag-native-checkbox',
    className,
  ].filter(Boolean).join(' ');
  const contentClasses = ['ag-native-checkbox-content', contentClassName].filter(Boolean).join(' ');
  const controlClasses = ['ag-native-checkbox-control', controlClassName].filter(Boolean).join(' ');

  useLayoutEffect(() => {
    if (!inputRef.current) return;
    inputRef.current.indeterminate = isIndeterminate;
  }, [isIndeterminate]);

  return (
    <label
      className={rootClassName}
      data-checked={isSelected ? 'true' : undefined}
      data-disabled={isDisabled ? 'true' : undefined}
      data-indeterminate={isIndeterminate ? 'true' : undefined}
    >
      <input
        ref={inputRef}
        aria-label={ariaLabel}
        checked={isSelected}
        className="ag-native-checkbox-input"
        disabled={isDisabled}
        name={name}
        type="checkbox"
        onChange={(event) => onChange(event.currentTarget.checked)}
      />
      <span
        className={contentClasses}
        style={contentStyle}
      >
        <span className={controlClasses}>
          <span className="ag-native-checkbox-indicator" aria-hidden="true">
            {isIndeterminate ? (
              <svg viewBox="0 0 12 12" focusable="false">
                <path
                  d="M2.5 6h7"
                  fill="none"
                  stroke="currentColor"
                  strokeLinecap="round"
                  strokeWidth="2"
                />
              </svg>
            ) : isSelected ? (
              <svg viewBox="0 0 12 12" focusable="false">
                <path
                  d="M2 6.2 4.7 9 10 3"
                  fill="none"
                  stroke="currentColor"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  strokeWidth="2.25"
                />
              </svg>
            ) : null}
          </span>
        </span>
        {children}
      </span>
    </label>
  );
}
