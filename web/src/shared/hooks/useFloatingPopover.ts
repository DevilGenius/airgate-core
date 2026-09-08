import { useCallback, useLayoutEffect, useRef, type RefObject } from 'react';

type FloatingAlign = 'start' | 'end';

/**
 * Keeps locally rendered menus in the document tree while promoting them to
 * the browser top layer. This prevents a menu from being clipped by a modal
 * body or a table's overflow container and keeps inherited theme selectors.
 */
export function useFloatingPopover<T extends HTMLElement>({
  align = 'end',
  isOpen,
}: {
  align?: FloatingAlign;
  isOpen: boolean;
}) {
  const triggerRef = useRef<HTMLElement | null>(null);
  const popoverRef = useRef<T | null>(null);

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current;
    const popover = popoverRef.current;
    if (!trigger || !popover) return;

    const triggerRect = trigger.getBoundingClientRect();
    const popoverWidth = popover.offsetWidth || triggerRect.width;
    const popoverHeight = popover.offsetHeight;
    const viewportPadding = 12;
    const gap = 6;
    const preferredLeft = align === 'start'
      ? triggerRect.left
      : triggerRect.right - popoverWidth;
    const left = Math.min(
      Math.max(viewportPadding, preferredLeft),
      Math.max(viewportPadding, window.innerWidth - popoverWidth - viewportPadding),
    );
    const below = triggerRect.bottom + gap;
    const top = below + popoverHeight <= window.innerHeight - viewportPadding
      || triggerRect.top < popoverHeight + gap + viewportPadding
      ? below
      : triggerRect.top - popoverHeight - gap;

    popover.style.setProperty('--ag-floating-left', `${Math.round(left)}px`);
    popover.style.setProperty('--ag-floating-top', `${Math.round(Math.max(viewportPadding, top))}px`);
    popover.style.setProperty('--ag-floating-trigger-width', `${Math.round(triggerRect.width)}px`);
  }, [align]);

  useLayoutEffect(() => {
    const popover = popoverRef.current;
    if (!popover) return undefined;

    if (isOpen) {
      if (typeof popover.showPopover === 'function' && !popover.matches(':popover-open')) {
        try {
          popover.showPopover();
        } catch {
          // Older embedded browsers expose the property but reject manual popovers.
        }
      }
      updatePosition();
      const frame = window.requestAnimationFrame(updatePosition);
      window.addEventListener('resize', updatePosition);
      window.addEventListener('scroll', updatePosition, true);
      return () => {
        window.cancelAnimationFrame(frame);
        window.removeEventListener('resize', updatePosition);
        window.removeEventListener('scroll', updatePosition, true);
      };
    }

    if (typeof popover.hidePopover === 'function' && popover.matches(':popover-open')) {
      try {
        popover.hidePopover();
      } catch {
        // Ignore a close that races with unmount.
      }
    }
    return undefined;
  }, [isOpen, updatePosition]);

  return {
    popoverRef,
    triggerRef,
  } as {
    popoverRef: RefObject<T | null>;
    triggerRef: RefObject<HTMLElement | null>;
  };
}
