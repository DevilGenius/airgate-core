import { act, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useFloatingPopover } from './useFloatingPopover';

const observers: TestResizeObserver[] = [];
class TestResizeObserver implements ResizeObserver {
  observe = vi.fn();
  unobserve = vi.fn();
  disconnect = vi.fn();
  constructor(private callback: ResizeObserverCallback) { observers.push(this); }
  notify() { this.callback([], this); }
}

function Harness({ open }: { open: boolean }) {
  const { triggerRef, popoverRef } = useFloatingPopover<HTMLDivElement>({ isOpen: open, align: 'end' });
  return <><div ref={(node) => { triggerRef.current = node; }} data-testid="trigger" />{open ? <div ref={popoverRef} data-testid="panel" /> : null}</>;
}

describe('floating dropdown sizing', () => {
  let width: number;
  let frameID: number;
  let frames: Map<number, FrameRequestCallback>;
  const flushFrames = () => act(() => {
    const pending = [...frames.values()];
    frames.clear();
    pending.forEach((callback) => callback(0));
  });

  beforeEach(() => {
    observers.length = 0;
    width = 480.5;
    frameID = 0;
    frames = new Map();
    vi.stubGlobal('ResizeObserver', TestResizeObserver);
    vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => { frames.set(++frameID, callback); return frameID; });
    vi.spyOn(window, 'cancelAnimationFrame').mockImplementation((id) => { frames.delete(id); });
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      if (this.dataset.testid === 'trigger') return new DOMRect(100.25, 100, width, 40);
      if (this.dataset.testid === 'panel') {
        // Model CSS sizing: this value must be set BEFORE panel measurement.
        return new DOMRect(0, 0, Number.parseFloat(this.style.getPropertyValue('--ag-floating-trigger-width')) || 180, 120);
      }
      return new DOMRect();
    });
  });
  afterEach(() => vi.unstubAllGlobals());

  it('publishes full fractional width before calculating alignment', () => {
    render(<Harness open />);
    const panel = screen.getByTestId('panel');
    expect(panel.style.getPropertyValue('--ag-floating-trigger-width')).toBe('480.5px');
    expect(panel.style.getPropertyValue('--ag-floating-left')).toBe('100.25px');
  });

  it('tracks element resizing only while open and coalesces notifications', () => {
    const { rerender } = render(<Harness open={false} />);
    expect(observers).toHaveLength(0);
    rerender(<Harness open />);
    flushFrames();
    const observer = observers[0]!;
    expect(observer.observe).toHaveBeenCalledTimes(2);
    width = 640.25;
    act(() => { observer.notify(); observer.notify(); });
    expect(frames.size).toBe(1);
    flushFrames();
    expect(screen.getByTestId('panel').style.getPropertyValue('--ag-floating-trigger-width')).toBe('640.25px');
    expect(screen.getByTestId('panel').style.getPropertyValue('--ag-floating-left')).toBe('100.25px');
    rerender(<Harness open={false} />);
    expect(observer.disconnect).toHaveBeenCalledOnce();
    expect(frames.size).toBe(0);
  });
});
