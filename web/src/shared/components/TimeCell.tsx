const TIME_FORMATTER = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  hour12: false,
  minute: '2-digit',
  second: '2-digit',
});
const DATE_FORMATTER = new Intl.DateTimeFormat('zh-CN');

function timeLabels(value?: string) {
  if (!value) return { dateLabel: '', fullLabel: '-', timeLabel: '-' };
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return { dateLabel: '', fullLabel: value, timeLabel: value };
  }
  const timeLabel = TIME_FORMATTER.format(date);
  const dateLabel = DATE_FORMATTER.format(date);
  return { dateLabel, fullLabel: `${dateLabel} ${timeLabel}`, timeLabel };
}

/** Shared timestamp cell: time above a smaller, secondary date. */
export function TimeCell({ value }: { value?: string }) {
  const { dateLabel, fullLabel, timeLabel } = timeLabels(value);
  return (
    <div className="flex min-w-0 flex-col justify-center gap-1 text-left" title={fullLabel}>
      <span className="truncate font-mono text-[13px] font-medium leading-none text-text">{timeLabel}</span>
      {dateLabel ? (
        <span className="truncate font-mono text-[11px] leading-none text-text-tertiary">{dateLabel}</span>
      ) : null}
    </div>
  );
}
