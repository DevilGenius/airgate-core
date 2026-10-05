import { useCallback, useLayoutEffect, useRef, type RefObject } from 'react';

type FloatingAlign = 'start' | 'end';

/**
 * Keeps locally rendered menus in the document tree while promoting them to
 * the browser top layer. This prevents a menu from being clipped by a modal
 * body or a table's overflow container and keeps inherited theme selectors.
 */
export function useFloatingPopover<T extends HTMLElement>({
  align = 'end',
  anchorRef,
  isOpen,
}: {
  align?: FloatingAlign;
  anchorRef?: RefObject<HTMLElement | null>;
  isOpen: boolean;
}) {
  const triggerRef = useRef<HTMLElement | null>(null);
  const popoverRef = useRef<T | null>(null);

  const updatePosition = useCallback(() => {
    const trigger = anchorRef?.current ?? triggerRef.current;
    const popover = popoverRef.current;
    if (!trigger || !popover) return;

    const triggerRect = trigger.getBoundingClientRect();
    // Publish width before measuring: right-edge alignment must use the newly
    // sized panel, not its previous/default width. Preserve fractional pixels.
    popover.style.setProperty('--ag-floating-trigger-width', `${triggerRect.width}px`);
    const popoverRect = popover.getBoundingClientRect();
    const popoverWidth = popoverRect.width || triggerRect.width;
    const popoverHeight = popoverRect.height;
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

    popover.style.setProperty('--ag-floating-left', `${left}px`);
    popover.style.setProperty('--ag-floating-top', `${Math.round(Math.max(viewportPadding, top))}px`);
  }, [align, anchorRef]);

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
      let frame: number | undefined;
      const scheduleUpdate = () => {
        if (frame != null) return;
        frame = window.requestAnimationFrame(() => {
          frame = undefined;
          updatePosition();
        });
      };
      scheduleUpdate();
      const observer = typeof ResizeObserver === 'undefined' ? undefined : new ResizeObserver(scheduleUpdate);
      if (triggerRef.current) observer?.observe(triggerRef.current);
      if (anchorRef?.current) observer?.observe(anchorRef.current);
      observer?.observe(popover);
      window.addEventListener('resize', scheduleUpdate);
      window.addEventListener('scroll', scheduleUpdate, true);
      return () => {
        if (frame != null) window.cancelAnimationFrame(frame);
        observer?.disconnect();
        window.removeEventListener('resize', scheduleUpdate);
        window.removeEventListener('scroll', scheduleUpdate, true);
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
  }, [anchorRef, isOpen, updatePosition]);

  return {
    popoverRef,
    triggerRef,
  } as {
    popoverRef: RefObject<T | null>;
    triggerRef: RefObject<HTMLElement | null>;
  };
}
