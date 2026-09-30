import { StrictMode } from 'react';
import { act, render } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { EChartsOption } from 'echarts';
import { BaseChart } from './BaseChart';

const mocks = vi.hoisted(() => {
  const chart = { on: vi.fn(), resize: vi.fn(), setOption: vi.fn(), dispose: vi.fn() };
  return { chart, init: vi.fn(() => chart) };
});
vi.mock('echarts/core', () => ({ init: mocks.init, use: vi.fn() }));
vi.mock('echarts/charts', () => ({ LineChart: {} }));
vi.mock('echarts/components', () => ({ AriaComponent: {}, GridComponent: {}, LegendScrollComponent: {}, TooltipComponent: {} }));
vi.mock('echarts/renderers', () => ({ CanvasRenderer: {} }));

let width = 640;
let frameID = 0;
let frames = new Map<number, FrameRequestCallback>();
let resize: () => void;
let themeChange: () => void;
let resizeDisconnect = vi.fn<() => void>();
let themeDisconnect = vi.fn<() => void>();

function flushFrame() {
  act(() => {
    const pending = [...frames.values()];
    frames.clear();
    pending.forEach(callback => callback(0));
  });
}
const option = (): EChartsOption => ({ legend: {}, series: [{ type: 'line', name: 'input', data: [1] }] });

beforeEach(() => {
  vi.clearAllMocks();
  width = 640;
  frames = new Map();
  resizeDisconnect = vi.fn<() => void>();
  themeDisconnect = vi.fn<() => void>();
  vi.stubGlobal('matchMedia', vi.fn((media: string) => ({
    media, matches: false, onchange: null,
    addEventListener: vi.fn(), removeEventListener: vi.fn(), dispatchEvent: vi.fn(),
  })));
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockImplementation(() => width);
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(240);
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
    clearRect: vi.fn(), fillRect: vi.fn(), fillStyle: '',
    getImageData: () => ({ data: new Uint8ClampedArray([1, 2, 3, 255]) }),
  } as unknown as CanvasRenderingContext2D);
  vi.stubGlobal('requestAnimationFrame', (callback: FrameRequestCallback) => { frames.set(++frameID, callback); return frameID; });
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id));
  vi.stubGlobal('ResizeObserver', class {
    constructor(callback: () => void) { resize = callback; }
    observe() {}
    disconnect() { resizeDisconnect(); }
  });
  vi.stubGlobal('MutationObserver', class {
    constructor(callback: () => void) { themeChange = callback; }
    observe() {}
    disconnect() { themeDisconnect(); }
  });
});
afterEach(() => vi.unstubAllGlobals());

describe('BaseChart lifecycle', () => {
  it('waits for a visible container and resizes the same instance', () => {
    width = 0;
    const { unmount } = render(<BaseChart option={option} label="Trend" />);
    flushFrame();
    expect(mocks.init).not.toHaveBeenCalled();
    width = 640;
    resize();
    flushFrame();
    expect(mocks.init).toHaveBeenCalledTimes(1);
    resize();
    flushFrame();
    expect(mocks.init).toHaveBeenCalledTimes(1);
    expect(mocks.chart.resize).toHaveBeenCalledTimes(2);
    unmount();
    expect(mocks.chart.dispose).toHaveBeenCalledTimes(1);
  });

  it('updates options without losing legend selection and discards removed series state', () => {
    const { rerender, unmount } = render(<BaseChart option={option} label="Trend" />);
    flushFrame();
    const listener = mocks.chart.on.mock.calls[0]?.[1] as (event: unknown) => void;
    listener({ selected: { input: false, removed: false } });
    rerender(<BaseChart option={() => ({ ...option(), series: [{ type: 'line', name: 'input', data: [5] }] })} label="Trend" />);
    flushFrame();
    expect(mocks.chart.setOption).toHaveBeenLastCalledWith(expect.objectContaining({
      legend: { selected: { input: false } },
      series: [{ type: 'line', name: 'input', data: [5] }],
    }), { notMerge: true, lazyUpdate: true });
    expect(mocks.init).toHaveBeenCalledTimes(1);
    unmount();
  });

  it('refreshes resolved theme colors and cancels pending work on unmount', () => {
    const createOption = vi.fn(option);
    const { unmount } = render(<BaseChart option={createOption} label="Trend" />);
    flushFrame();
    themeChange();
    flushFrame();
    expect(createOption).toHaveBeenCalledTimes(2);
    expect(mocks.init).toHaveBeenCalledTimes(1);
    resize();
    unmount();
    flushFrame();
    expect(createOption).toHaveBeenCalledTimes(2);
    expect(resizeDisconnect).toHaveBeenCalledTimes(1);
    expect(themeDisconnect).toHaveBeenCalledTimes(1);
    expect(mocks.chart.dispose).toHaveBeenCalledTimes(1);
  });

  it('does not leave a duplicate instance during StrictMode effect replay', () => {
    const { unmount } = render(<StrictMode><BaseChart option={option} label="Trend" /></StrictMode>);
    flushFrame();
    expect(mocks.init).toHaveBeenCalledTimes(1);
    unmount();
    expect(mocks.chart.dispose).toHaveBeenCalledTimes(1);
  });
});
