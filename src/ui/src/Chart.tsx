// A time series chart: one uPlot instance owned by this component,
// resized with its container. Values are aligned on shared times; a
// missing value (null) is drawn as a gap, never as zero.

import {useEffect, useRef} from 'preact/hooks';
import uPlot from 'uplot';
import {useTheme} from './theme';

export interface ChartSeries {
    label: string;
    /** One value (or null for a gap) per time in ChartProps.times. */
    values: (number | null)[];
}

export interface ChartProps {
    title: string;
    /** Unix seconds. */
    times: number[];
    /** The x axis range, Unix seconds: the whole requested range, so a
     *  stretch without data (a server that stopped reporting) shows. */
    from: number;
    to: number;
    series: ChartSeries[];
    format: (v: number) => string;
    /** Fixed y range, e.g. [0, 100] for percentages. */
    range?: [number, number];
    height?: number;
}

const palette = ['blue', 'green', 'yellow', 'pink', 'red', 'l-blue', 'l-green', 'l-yellow', 'l-pink', 'l-red', 'd-blue', 'd-green', 'd-yellow', 'd-pink', 'd-red'];

/** A theme color by role name, read from the CSS variables. */
function color(name: string): string {
    return getComputedStyle(document.documentElement).getPropertyValue(`--color-c-${name}`).trim() || 'gray';
}

export function Chart(props: ChartProps) {
    const box = useRef<HTMLDivElement>(null);
    const theme = useTheme();

    useEffect(() => {
        const el = box.current;
        if (!el) {
            return;
        }
        const axisColor = color('on-surface-variant');
        const gridColor = color('outline-variant');
        const axis = (values?: (u: uPlot, splits: number[]) => string[]): uPlot.Axis => ({
            stroke: axisColor,
            grid: {stroke: gridColor, width: 1},
            ticks: {stroke: gridColor, width: 1},
            values,
        });
        const opts: uPlot.Options = {
            width: el.clientWidth,
            height: props.height ?? 220,
            series: [
                {},
                ...props.series.map((s, i) => ({
                    label: s.label,
                    stroke: color(palette[i % palette.length] ?? 'blue'),
                    width: 1.5,
                    spanGaps: false,
                    value: (_u: uPlot, v: number | null) => (v == null ? '-' : props.format(v)),
                })),
            ],
            axes: [axis(), {...axis((_u, splits) => splits.map((v) => props.format(v))), size: 70}],
            scales: {x: {time: true, range: [props.from, props.to]}, ...(props.range ? {y: {range: props.range}} : {})},
            legend: {live: true},
            cursor: {drag: {x: true, y: false}},
        };
        const data: uPlot.AlignedData = [props.times, ...props.series.map((s) => s.values)];
        const plot = new uPlot(opts, data, el);
        const resize = new ResizeObserver(() => plot.setSize({width: el.clientWidth, height: props.height ?? 220}));
        resize.observe(el);
        return () => {
            resize.disconnect();
            plot.destroy();
        };
    }, [props.times, props.series, props.format, props.range, props.height, props.from, props.to, theme]);

    return (
        <div class="surface-1 flex min-w-0 flex-col gap-2">
            <h3>{props.title}</h3>
            {props.times.length === 0 ? <small>No data in this range.</small> : <div ref={box} />}
        </div>
    );
}
