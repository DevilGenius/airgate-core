import { Calendar, DateField, DatePicker, Description, Label } from '@heroui/react';
import type { DateValue } from '@internationalized/date';
import { parseDate } from '@internationalized/date';
import { useMemo } from 'react';

interface CommonDatePickerProps {
  className?: string;
  description?: string;
  hideLabel?: boolean;
  isRequired?: boolean;
  label: string;
  name?: string;
  onChange: (value: string) => void;
  value?: string;
}

function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ');
}

function toDateValue(value?: string): DateValue | null {
  if (!value) return null;
  try {
    return parseDate(value);
  } catch {
    return null;
  }
}

export function CommonDatePicker({
  className = 'w-full',
  description,
  hideLabel,
  isRequired,
  label,
  name,
  onChange,
  value,
}: CommonDatePickerProps) {
  const dateValue = useMemo(() => toDateValue(value), [value]);

  return (
    <DatePicker
      className={cx('ag-common-date-picker', className)}
      isRequired={isRequired}
      name={name}
      value={dateValue}
      onChange={(nextValue) => onChange(nextValue?.toString() ?? '')}
    >
      <Label className={cx('ag-common-date-picker-label', hideLabel ? 'sr-only' : null)}>{label}</Label>
      <DateField.Group className="ag-common-date-picker-group" fullWidth>
        <DateField.Input className="ag-common-date-picker-input">
          {(segment) => <DateField.Segment segment={segment} />}
        </DateField.Input>
        <DateField.Suffix className="ag-common-date-picker-suffix">
          <DatePicker.Trigger className="ag-common-date-picker-trigger">
            <DatePicker.TriggerIndicator />
          </DatePicker.Trigger>
        </DateField.Suffix>
      </DateField.Group>
      {description ? <Description>{description}</Description> : null}
      <DatePicker.Popover className="ag-common-date-picker-popover">
        <Calendar aria-label={label}>
          <Calendar.Header>
            <Calendar.YearPickerTrigger>
              <Calendar.YearPickerTriggerHeading />
              <Calendar.YearPickerTriggerIndicator />
            </Calendar.YearPickerTrigger>
            <Calendar.NavButton slot="previous" />
            <Calendar.NavButton slot="next" />
          </Calendar.Header>
          <Calendar.Grid>
            <Calendar.GridHeader>
              {(day) => <Calendar.HeaderCell>{day}</Calendar.HeaderCell>}
            </Calendar.GridHeader>
            <Calendar.GridBody>
              {(date) => <Calendar.Cell date={date} />}
            </Calendar.GridBody>
          </Calendar.Grid>
          <Calendar.YearPickerGrid>
            <Calendar.YearPickerGridBody>
              {({ year }) => <Calendar.YearPickerCell year={year} />}
            </Calendar.YearPickerGridBody>
          </Calendar.YearPickerGrid>
        </Calendar>
      </DatePicker.Popover>
    </DatePicker>
  );
}
