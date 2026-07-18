import type { ReactNode } from 'react';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import { ToolbarMenu, ToolbarMenuItem } from '../../../shared/components/ToolbarMenu';
import type { SelectOption } from './types';

export type MonitorMultiFilterGroup = {
  defaultValue?: string;
  id: string;
  label: string;
  options: SelectOption[];
  selectionMode?: 'multiple' | 'single';
  selectedValues: readonly string[];
  showInSummary?: boolean;
  summaryLabel?: string;
};

export function MonitorFilterSelect({
  ariaLabel,
  className,
  label,
  onChange,
  options,
  selectedLabel,
  value,
}: {
  ariaLabel: string;
  className?: string;
  label?: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  selectedLabel?: ReactNode;
  value: string;
}) {
  const selected = options.find((item) => item.id === value)?.label ?? options[0]?.label ?? '';
  return (
    <div className={className}>
      <SimpleSelect
        ariaLabel={ariaLabel}
        fullWidth
        items={options.map((item) => ({ key: item.id, label: item.label }))}
        selectedKey={value}
        selectedLabel={selectedLabel ?? (label ? `${label}: ${selected}` : selected)}
        onSelectionChange={onChange}
      />
    </div>
  );
}

export function MonitorMultiFilterSelect({
  allLabel,
  ariaLabel,
  className,
  groups,
  label,
  onClear,
  onToggle,
}: {
  allLabel: string;
  ariaLabel: string;
  className?: string;
  groups: MonitorMultiFilterGroup[];
  label: string;
  onClear: () => void;
  onToggle: (groupID: string, value: string) => void;
}) {
  const selectedLabels = groups.flatMap((group) => {
    const selectedValues = new Set(group.selectedValues);
    const selectedOptions = group.options.filter((option) => selectedValues.has(option.id));
    const selectedOption = selectedOptions[0];
    if (group.showInSummary === false) {
      return [];
    }
    if (
      group.selectionMode === 'single'
      && group.defaultValue !== undefined
      && selectedOptions.length === 1
      && selectedOption !== undefined
      && selectedOption.id === group.defaultValue
    ) {
      return [];
    }
    if (group.summaryLabel && selectedOptions.length > 0) {
      return [group.summaryLabel];
    }
    return selectedOptions.map((option) => option.label);
  });
  const selectionSummary = selectedLabels.length > 0 ? selectedLabels.join(', ') : allLabel;

  return (
    <div className={className}>
      <ToolbarMenu
        ariaLabel={ariaLabel}
        className="ag-simple-select-trigger select__trigger"
        label={<span title={selectionSummary}>{label}: {selectionSummary}</span>}
        rootClassName="ag-simple-select ag-simple-select--full ag-monitor-multi-filter"
      >
        {(close) => (
          <>
            <ToolbarMenuItem
              isSelected={selectedLabels.length === 0}
              role="menuitemcheckbox"
              onSelect={onClear}
            >
              {allLabel}
            </ToolbarMenuItem>
            {groups.map((group) => {
              const selectedValues = new Set(group.selectedValues);
              const isSingleSelect = group.selectionMode === 'single';
              return (
                <div
                  aria-label={group.label}
                  className="ag-monitor-multi-filter-group"
                  key={group.id}
                  role="group"
                >
                  <div className="ag-monitor-multi-filter-heading">{group.label}</div>
                  {group.options.map((option) => (
                    <ToolbarMenuItem
                      isSelected={selectedValues.has(option.id)}
                      key={option.id}
                      role={isSingleSelect ? 'menuitemradio' : 'menuitemcheckbox'}
                      onSelect={() => {
                        onToggle(group.id, option.id);
                        if (isSingleSelect) close();
                      }}
                    >
                      {option.label}
                    </ToolbarMenuItem>
                  ))}
                </div>
              );
            })}
          </>
        )}
      </ToolbarMenu>
    </div>
  );
}
