import { useState } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { NativeSwitch } from './NativeSwitch';

describe('NativeSwitch navigation restoration', () => {
  it('restores an asynchronously loaded closed state without enabling transitions', () => {
    const onChange = vi.fn();
    const { rerender } = render(<NativeSwitch isSelected onChange={onChange} />);
    rerender(<NativeSwitch isSelected={false} onChange={onChange} />);

    expect(screen.getByRole('switch')).not.toBeChecked();
    expect(screen.getByRole('switch').closest('label')).toHaveAttribute('data-animated', 'false');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('animates user toggles but starts static again when the page remounts', () => {
    function ControlledSwitch({ initial }: { initial: boolean }) {
      const [selected, setSelected] = useState(initial);
      return <NativeSwitch isSelected={selected} onChange={setSelected} />;
    }
    const { unmount } = render(<ControlledSwitch initial />);
    fireEvent.click(screen.getByRole('switch'));
    expect(screen.getByRole('switch')).not.toBeChecked();
    expect(screen.getByRole('switch').closest('label')).toHaveAttribute('data-animated', 'true');
    unmount();

    render(<ControlledSwitch initial={false} />);
    expect(screen.getByRole('switch')).not.toBeChecked();
    expect(screen.getByRole('switch').closest('label')).toHaveAttribute('data-animated', 'false');
    fireEvent.click(screen.getByRole('switch'));
    expect(screen.getByRole('switch')).toBeChecked();
    expect(screen.getByRole('switch').closest('label')).toHaveAttribute('data-animated', 'true');
  });
});
