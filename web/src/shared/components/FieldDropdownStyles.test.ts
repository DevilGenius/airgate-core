import { createElement } from 'react';
import { render } from '@testing-library/react';
import { RefreshControl } from './RefreshControl';
import refreshCSS from './RefreshControl.module.css?raw';
import refreshStyles from './RefreshControl.module.css';
import { describe, expect, it } from 'vitest';
import fieldCSS from '../../styles/components/forms.css?raw';
import layoutCSS from '../../styles/layout.css?raw';
import overlayCSS from '../../styles/components/overlays.css?raw';
import accountModalCSS from '../../styles/page/account-modals.css?raw';
import paginationCSS from './TablePaginationFooter.module.css?raw';
import paginationStyles from './TablePaginationFooter.module.css';

describe('shared field dropdown width contract', () => {
  it.each(['fields-first', 'fields-last'])('uses one arrow size and control height across dropdown variants (%s)', (order) => {
    const pageCSS = paginationCSS.replace(/\.([A-Za-z_][\w-]*)/g, (selector, name: string) => paginationStyles[name] ? '.' + paginationStyles[name] : selector);
    const otherCSS = layoutCSS + accountModalCSS + pageCSS;
    const sheet = document.createElement('style');
    sheet.textContent = order === 'fields-first' ? fieldCSS + otherCSS : otherCSS + fieldCSS;
    document.head.append(sheet);
    const host = document.createElement('div');
    document.body.append(host);
    try {
      const examples = [
        '<div class="ag-simple-select"><button class="ag-toolbar-menu-trigger ag-simple-select-trigger select__trigger"><svg class="ag-toolbar-menu-caret"></svg></button></div>',
        '<div class="select"><button class="select__trigger"><svg class="select__indicator"></svg></button></div>',
        '<div class="ag-import-config"><div class="combo-box"><div class="combo-box__input-group"><input class="input"><button class="combo-box__trigger"><svg></svg></button></div></div></div>',
      ];
      for (const html of examples) {
        host.innerHTML = html;
        const icon = host.querySelector('svg')!;
        expect(getComputedStyle(icon).width).toBe('var(--ag-dropdown-icon-size, 1rem)');
        expect(getComputedStyle(icon).height).toBe('var(--ag-dropdown-icon-size, 1rem)');
        const control = host.querySelector('input') ?? host.querySelector('button')!;
        expect(getComputedStyle(control).height).toBe('var(--ag-dropdown-control-height, 2.5rem)');
        const arrowButton = host.querySelector('.combo-box__trigger');
        if (arrowButton) expect(getComputedStyle(arrowButton).width).toBe('var(--ag-dropdown-arrow-width, 2.25rem)');
      }
      host.innerHTML = '<div class="' + paginationStyles.pagination + '"><div class="ag-simple-select ' + paginationStyles.pageSizeControl + '"><button class="ag-toolbar-menu-trigger ag-simple-select-trigger select__trigger"><svg class="ag-toolbar-menu-caret"></svg></button></div></div>';
      expect(getComputedStyle(host.firstElementChild!).getPropertyValue('--control-height').trim()).toBe('2.125rem');
      expect(getComputedStyle(host.querySelector('.ag-simple-select')!).getPropertyValue('--ag-dropdown-control-height').trim()).toBe('var(--control-height)');
      expect(getComputedStyle(host.querySelector('button')!).height).toBe('var(--ag-dropdown-control-height, 2.5rem)');
      expect(getComputedStyle(host.querySelector('svg')!).width).toBe('var(--ag-dropdown-icon-size, 1rem)');
    } finally { host.remove(); sheet.remove(); }
  });

  it.each(['fields-first', 'fields-last'])('does not cap wide selects or depend on style order (%s)', (order) => {
    const sheet = document.createElement('style');
    sheet.textContent = order === 'fields-first' ? fieldCSS + layoutCSS + overlayCSS : overlayCSS + layoutCSS + fieldCSS;
    document.head.append(sheet);
    const host = document.createElement('div');
    document.body.append(host);
    try {
      for (const [root, panel] of [
        ['ag-simple-select', 'ag-toolbar-menu-popover'],
        ['ag-simple-select ag-simple-multi-select', 'ag-toolbar-menu-popover'],
        ['ag-simple-select ag-monitor-multi-filter', 'ag-toolbar-menu-popover'],
        ['ag-search-combobox', 'ag-search-combobox-popover'],
        ['', 'select__popover'], ['', 'combo-box__popover'],
      ]) {
        host.className = root!;
        const popup = document.createElement('div');
        popup.className = panel!;
        popup.style.setProperty('--ag-floating-trigger-width', '640.5px');
        popup.style.setProperty('--trigger-width', '640.5px');
        host.replaceChildren(popup);
        const computed = getComputedStyle(popup);
        expect(computed.boxSizing, panel).toBe('border-box');
        expect(computed.width, panel).toBe('var(--ag-floating-trigger-width, var(--trigger-width, auto))');
        expect(computed.minWidth, panel).toBe('0px');
        expect(computed.maxWidth, panel).toBe('calc(100vw - 24px)');
      }
    } finally { host.remove(); sheet.remove(); }
  });
});


describe('refresh control module styles', () => {
  it.each(['fields-first', 'fields-last'])('styles the real refresh component (%s)', (order) => {
    const moduleCSS = refreshCSS
      .replace(/\.([A-Za-z_][\w-]*)/g, (selector, name: string) => refreshStyles[name] ? '.' + refreshStyles[name] : selector)
      .replace(/:global\(([^)]+)\)/g, '$1');
    const sheet = document.createElement('style');
    sheet.textContent = order === 'fields-first' ? fieldCSS + moduleCSS : moduleCSS + fieldCSS;
    document.head.append(sheet);
    const view = render(createElement(RefreshControl, {
      value: 0, options: [0, 5, 30], ariaLabel: 'Auto refresh', refreshAriaLabel: 'Refresh',
      onChange: () => {}, onRefresh: () => {},
    }));
    try {
      const control = view.getByRole('group', { name: 'Auto refresh' });
      expect(control).toHaveClass(refreshStyles.control!);
      expect(getComputedStyle(control).display).toBe('inline-flex');
      expect(getComputedStyle(control).height).toBe('var(--ag-dropdown-control-height, 2.5rem)');
      expect(view.getByRole('button', { name: 'Refresh' })).toHaveClass(refreshStyles.refresh!);
      expect(view.getByRole('button', { name: 'Auto refresh' })).toHaveClass(refreshStyles.arrow!);
    } finally {
      view.unmount();
      sheet.remove();
    }
  });
});
