# Frontend surfaces and rendering boundaries

## Shared presentation

- Theme colors come from the SDK through `web/src/styles/theme.css`. Application geometry uses `--ag-panel-*`, `--ag-control-radius`, `--ag-page-gap`, and `--ag-page-gutter`. Light and dark modes share the same layout.
- Import `Card` from `shared/components/Card`, not directly from HeroUI, for application cards. The root owns its border, background and radius; its slots own padding. Use `density="compact"` for metric cards. Avoid adding a second outer padding layer.
- Use `Panel` for chart/detail sections with a title and optional header actions. Dashboard and user overview share this implementation.
- Use `PageToolbar` for filter/action slots and `PageToolbarFrame` when controls require a custom arrangement. Toolbars use the page background, without an outer border or padding, so controls align with the table edges. `flow="stack"` leaves spacing to the parent grid/flex layout.
- `TablePage` owns table loading presentation and the footer portal. `ContentPage` provides the readable content width and spacing for settings/profile forms.
- The app shell owns navigation, mobile focus management and the main content gutter. The topbar and footer use a compact 44px height and the page background; the footer can grow when pagination wraps. The topbar does not show the current route. The sidebar has no documentation footer. The built-in documentation page and its route are removed; header/home documentation links appear only when a safe URL is configured. Full-bleed plugin pages continue to opt out through `data-full-bleed`.
- Modal backdrops retain the original frosted treatment: theme-aware layered gradients and a 10px backdrop blur, with a translucent fallback for browsers without backdrop-filter support. This is real backdrop blur, not a simulated low-cost veil. Do not animate the filter or add screenshot capture, canvas processing, or JavaScript per-frame effects. Preserve credential-page rendering isolation. Statistics and edit dialogs use the shared size tokens; do not add inline pixel width caps that silently override those sizes. The positioning container spans the viewport, while the dialog width remains responsive.
- Component styling belongs in CSS Modules. Do not use import order or `!important` to override component geometry. The existing specialized table styles remain responsible for column dimensions and compact row layout.

## Credential management performance

Keep `AccountsTableSection` separate from generic table adapters. It mounts only the active desktop table or mobile card tree.

Preserve these update boundaries:

1. Stable account/column references and memoized rows/cells isolate refreshed values.
2. `AccountSelectionStore` updates registered checkboxes/cards directly; selection counts subscribe separately. Do not move the selected-ID set into a shared React context for all rows.
3. `AccountCapacityStore` notifies subscribers by account ID. Unchanged live capacity values must not notify unrelated accounts.
4. Modal isolation and deferred rendering remain in `accountPagePerf.ts`. Do not wrap the table in page-wide animations, backdrop filters, or providers that update on every timer tick.
5. The mobile/desktop branch, usage cache, row dimensions, and server pagination retain their existing behavior.

`accountRendering.test.tsx` checks 250-row selection/toolbar isolation, selective cell refresh, and live capacity notifications. These are rendering regression checks, not browser frame-rate benchmarks.

## Verification

Run TypeScript checks and focused unit tests after behavioral changes. Build, lint and screenshot verification follow the project instruction: run only when explicitly requested or before committing.
