import { useState, useRef, useEffect, useCallback, useLayoutEffect } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { ChevronLeft, ChevronRight, Calendar } from 'lucide-react';

interface DatePickerProps {
  value: string;              // yyyy-MM-dd
  onChange: (v: string) => void;
  placeholder?: string;
  label?: string;
  hint?: string;
  required?: boolean;
  className?: string;
}

function fmt(y: number, m: number, d: number): string {
  return `${y}-${String(m + 1).padStart(2, '0')}-${String(d).padStart(2, '0')}`;
}

function parse(s: string): { y: number; m: number; d: number } | null {
  const parts = s.split('-');
  if (parts.length !== 3) return null;
  return { y: +(parts[0] ?? 0), m: +(parts[1] ?? 1) - 1, d: +(parts[2] ?? 1) };
}

const WEEKDAYS_ZH = ['一', '二', '三', '四', '五', '六', '日'];
const WEEKDAYS_EN = ['Mo', 'Tu', 'We', 'Th', 'Fr', 'Sa', 'Su'];

const PANEL_WIDTH = 280;

export function DatePicker({ value, onChange, placeholder, label, hint, required, className = '' }: DatePickerProps) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const [pos, setPos] = useState<{ top: number; left: number }>({ top: -9999, left: -9999 });

  const isZh = i18n.language.startsWith('zh');
  const weekdays = isZh ? WEEKDAYS_ZH : WEEKDAYS_EN;
  const defaultPlaceholder = placeholder ?? t('datepicker.placeholder');

  const parsed = parse(value);
  const now = new Date();
  const [viewYear, setViewYear] = useState(parsed?.y ?? now.getFullYear());
  const [viewMonth, setViewMonth] = useState(parsed?.m ?? now.getMonth());

  // 根据触发按钮和面板实际尺寸计算位置
  const updatePosition = useCallback(() => {
    if (!triggerRef.current || !panelRef.current) return;
    const triggerRect = triggerRef.current.getBoundingClientRect();
    const panelRect = panelRef.current.getBoundingClientRect();

    const spaceBelow = window.innerHeight - triggerRect.bottom;
    const spaceAbove = triggerRect.top;
    const dropUp = spaceBelow < panelRect.height + 8 && spaceAbove > spaceBelow;

    let left = triggerRect.left;
    if (left + PANEL_WIDTH > window.innerWidth - 8) {
      left = window.innerWidth - PANEL_WIDTH - 8;
    }
    if (left < 8) left = 8;

    setPos({
      top: dropUp ? triggerRect.top - panelRect.height - 4 : triggerRect.bottom + 4,
      left,
    });
  }, []);

  // 面板渲染后立即定位（useLayoutEffect 避免闪烁）
  useLayoutEffect(() => {
    if (open) updatePosition();
  }, [open, updatePosition, viewYear, viewMonth]);

  // 滚动/resize 时更新位置
  useEffect(() => {
    if (!open) return;
    window.addEventListener('scroll', updatePosition, true);
    window.addEventListener('resize', updatePosition);
    return () => {
      window.removeEventListener('scroll', updatePosition, true);
      window.removeEventListener('resize', updatePosition);
    };
  }, [open, updatePosition]);

  // 点击外部关闭
  useEffect(() => {
    if (!open) return;
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      if (
        triggerRef.current && !triggerRef.current.contains(target) &&
        panelRef.current && !panelRef.current.contains(target)
      ) {
        setOpen(false);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [open]);

  useEffect(() => {
    const p = parse(value);
    if (p) {
      setViewYear(p.y);
      setViewMonth(p.m);
    }
  }, [value]);

  const prevMonth = () => {
    if (viewMonth === 0) { setViewYear(viewYear - 1); setViewMonth(11); }
    else setViewMonth(viewMonth - 1);
  };
  const nextMonth = () => {
    if (viewMonth === 11) { setViewYear(viewYear + 1); setViewMonth(0); }
    else setViewMonth(viewMonth + 1);
  };

  const firstDay = new Date(viewYear, viewMonth, 1).getDay();
  const startOffset = firstDay === 0 ? 6 : firstDay - 1;
  const daysInMonth = new Date(viewYear, viewMonth + 1, 0).getDate();
  const daysInPrevMonth = new Date(viewYear, viewMonth, 0).getDate();

  const cells: { day: number; month: number; year: number; isCurrentMonth: boolean }[] = [];
  for (let i = startOffset - 1; i >= 0; i--) {
    const pm = viewMonth === 0 ? 11 : viewMonth - 1;
    const py = viewMonth === 0 ? viewYear - 1 : viewYear;
    cells.push({ day: daysInPrevMonth - i, month: pm, year: py, isCurrentMonth: false });
  }
  for (let d = 1; d <= daysInMonth; d++) {
    cells.push({ day: d, month: viewMonth, year: viewYear, isCurrentMonth: true });
  }
  const remaining = 42 - cells.length;
  for (let d = 1; d <= remaining; d++) {
    const nm = viewMonth === 11 ? 0 : viewMonth + 1;
    const ny = viewMonth === 11 ? viewYear + 1 : viewYear;
    cells.push({ day: d, month: nm, year: ny, isCurrentMonth: false });
  }

  const todayStr = fmt(now.getFullYear(), now.getMonth(), now.getDate());

  const monthTitle = isZh
    ? `${viewYear}年${String(viewMonth + 1).padStart(2, '0')}月`
    : `${new Date(viewYear, viewMonth).toLocaleString('en', { month: 'long' })} ${viewYear}`;

  return (
    <div className={`space-y-1.5 ${className}`}>
      {label && (
        <label className="block text-xs font-medium text-text-secondary uppercase tracking-wider">
          {label}
          {required && <span className="text-danger ml-0.5">*</span>}
        </label>
      )}
      <div>
        <button
          ref={triggerRef}
          type="button"
          className="flex items-center gap-2 w-full rounded-md border border-glass-border bg-surface px-3 py-2 text-sm text-left transition-all duration-200 hover:border-border-focus focus:outline-none focus:border-border-focus focus:shadow-[0_0_0_3px_var(--ag-primary-subtle)] cursor-pointer"
          onClick={() => setOpen(!open)}
        >
          <Calendar className="w-4 h-4 text-text-tertiary flex-shrink-0" />
          <span className={value ? 'text-text' : 'text-text-tertiary'}>
            {value || defaultPlaceholder}
          </span>
        </button>

        {open && createPortal(
          <div
            ref={panelRef}
            className="fixed z-[9999] rounded-lg border border-glass-border bg-bg-elevated shadow-lg p-3 select-none"
            style={{
              width: PANEL_WIDTH,
              top: pos.top,
              left: pos.left,
              animation: 'ag-scale-in 0.15s cubic-bezier(0.16, 1, 0.3, 1)',
            }}
          >
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-text">{monthTitle}</span>
              <div className="flex items-center gap-0.5">
                <button type="button" className="p-1 rounded hover:bg-bg-hover transition-colors text-text-tertiary hover:text-text cursor-pointer" onClick={prevMonth}>
                  <ChevronLeft className="w-4 h-4" />
                </button>
                <button type="button" className="p-1 rounded hover:bg-bg-hover transition-colors text-text-tertiary hover:text-text cursor-pointer" onClick={nextMonth}>
                  <ChevronRight className="w-4 h-4" />
                </button>
              </div>
            </div>

            <div className="grid grid-cols-7 mb-1">
              {weekdays.map((w) => (
                <div key={w} className="text-center text-[10px] font-medium text-text-tertiary py-1">{w}</div>
              ))}
            </div>

            <div className="grid grid-cols-7">
              {cells.map((cell, i) => {
                const cellStr = fmt(cell.year, cell.month, cell.day);
                const isSelected = cellStr === value;
                const isToday = cellStr === todayStr;
                return (
                  <button
                    key={i}
                    type="button"
                    className={`
                      h-8 text-xs rounded-md transition-all cursor-pointer
                      ${!cell.isCurrentMonth ? 'text-text-tertiary/40' : 'text-text-secondary hover:bg-bg-hover hover:text-text'}
                      ${isSelected ? '!bg-primary !text-white font-semibold' : ''}
                      ${isToday && !isSelected ? 'ring-1 ring-primary/50 font-semibold text-primary' : ''}
                    `}
                    onClick={() => { onChange(cellStr); setOpen(false); }}
                  >
                    {cell.day}
                  </button>
                );
              })}
            </div>

            <div className="flex items-center justify-between mt-2 pt-2 border-t border-border-subtle">
              <button type="button" className="text-[11px] text-text-tertiary hover:text-text transition-colors cursor-pointer" onClick={() => { onChange(''); setOpen(false); }}>
                {t('datepicker.clear')}
              </button>
              <button type="button" className="text-[11px] text-primary hover:text-primary/80 font-medium transition-colors cursor-pointer" onClick={() => { onChange(todayStr); setOpen(false); }}>
                {t('datepicker.today')}
              </button>
            </div>
          </div>,
          document.body,
        )}
      </div>
      {hint && <p className="text-xs text-text-tertiary">{hint}</p>}
    </div>
  );
}
