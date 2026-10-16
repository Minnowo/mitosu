// One host: status and problems, a range picker, charts of every metric
// family, containers and events.

import {useMemo, useState} from 'preact/hooks';
import {api, type Container, type HostEvent, type Series} from './api';
import {Chart} from './Chart';
import {bytes, dateTime, num, pct, rate} from './format';
import {status} from './Overview';
import {Problems} from './Problems';
import {hostHref} from './route';
import {useClock, useLoad} from './useLoad';

const ranges: [string, number][] = [
    ['1h', 3_600_000],
    ['6h', 6 * 3_600_000],
    ['24h', 24 * 3_600_000],
    ['7d', 7 * 86_400_000],
    ['30d', 30 * 86_400_000],
    ['1y', 365 * 86_400_000],
];

const refreshMs = 60_000;

// maxChartSeries matches the server's cap (store.MaxChartSeries).
const maxChartSeries = 64;

interface Spec {
    label: string;
    series: Series;
}

interface ChartDef {
    title: string;
    specs: Spec[];
    format: (v: number) => string;
    range?: [number, number];
}

export function HostPage(props: {host: string; range: string; onLoggedOut: () => void}) {
    const span = ranges.find(([name]) => name === props.range)?.[1] ?? 24 * 3_600_000;
    // The range ends now, moving once a refresh: a stable value between
    // refreshes (requests depend on it), and a server that stopped
    // reporting shows as an empty stretch at the right of each chart.
    const now = useClock(refreshMs);
    const to = now + 1;
    const from = to - span;
    const hosts = useLoad(() => api.hosts(), [now], props.onLoggedOut);
    const host = hosts.data?.find((h) => h.name === props.host);
    const series = useLoad(() => api.series(props.host), [props.host, now], props.onLoggedOut);
    const containers = useLoad(() => api.containers(props.host), [props.host, now], props.onLoggedOut);
    const events = useLoad(() => api.events(props.host, from, to), [props.host, from, to], props.onLoggedOut);
    const [pullMessage, setPullMessage] = useState('');
    const charts = useMemo(() => chartDefs(series.data ?? [], containers.data ?? []), [series.data, containers.data]);
    const loadErrors = [hosts.error, series.error, containers.error, events.error].filter((e) => e !== '');

    async function pullNow() {
        setPullMessage('');
        try {
            await api.pullNow(props.host);
            setPullMessage('Pull started.');
        } catch (e) {
            setPullMessage(String(e));
        }
    }

    if (hosts.data && !host) {
        return (
            <main class="flex flex-col gap-4 px-4 pb-4">
                <h1>{props.host}</h1>
                <p class="rounded-md p-3 bg-c-error-container text-c-on-error-container">This host is not in the config.</p>
            </main>
        );
    }
    return (
        <main class="flex flex-col gap-4 px-4 pb-4">
            <div class="flex flex-wrap items-center justify-between gap-2">
                <div>
                    <h1>{props.host}</h1>
                    <small>{host ? status(host) : 'Loading...'}</small>
                </div>
                <div class="flex flex-wrap items-center gap-2">
                    <nav class="flex gap-1" aria-label="Range">
                        {ranges.map(([name]) => (
                            <a class={`rounded-sm px-2 ${name === props.range ? 'bg-c-primary text-c-on-primary' : ''}`} href={hostHref(props.host, {range: name})} key={name}>
                                {name}
                            </a>
                        ))}
                    </nav>
                    <button onClick={pullNow} disabled={host?.pulling}>
                        Pull now
                    </button>
                </div>
            </div>
            {pullMessage && <small>{pullMessage}</small>}
            {loadErrors.length > 0 && <p class="rounded-md p-3 bg-c-error-container text-c-on-error-container">Loading failed: {loadErrors.join('; ')}</p>}
            {host && host.last_error && <small>Last pull failed: {host.last_error}</small>}
            {host && <Problems problems={host.problems} />}
            <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
                {charts.map((c) => (
                    <LoadedChart host={props.host} def={c} from={from} to={to} key={c.title} onLoggedOut={props.onLoggedOut} />
                ))}
            </div>
            {containers.data && containers.data.length > 0 && <Containers containers={containers.data} />}
            <Events events={events.data} />
        </main>
    );
}

function LoadedChart(props: {host: string; def: ChartDef; from: number; to: number; onLoggedOut: () => void}) {
    const series = props.def.specs.map((sp) => sp.series);
    const key = series.map((x) => `${x.kind}|${x.entity}|${x.metric}`).join(',');
    const data = useLoad(() => api.chart(props.host, series, props.from, props.to, 800), [props.host, key, props.from, props.to], props.onLoggedOut);
    const chart = useMemo(() => {
        if (!data.data) {
            return undefined;
        }
        const labels = new Map(props.def.specs.map((sp) => [`${sp.series.kind}|${sp.series.entity}|${sp.series.metric}`, sp.label]));
        return {
            times: data.data.times.map((t) => t / 1000),
            series: data.data.series.map((x, i) => ({label: labels.get(`${x.kind}|${x.entity}|${x.metric}`) ?? x.metric, values: data.data!.values[i] ?? []})),
        };
    }, [data.data]);
    if (data.error || !chart) {
        return (
            <div class="surface-1 flex min-w-0 flex-col gap-2">
                <h3>{props.def.title}</h3>
                <small>{data.error || 'Loading...'}</small>
            </div>
        );
    }
    return (
        <Chart
            title={props.def.title}
            times={chart.times}
            series={chart.series}
            format={props.def.format}
            range={props.def.range}
            from={props.from / 1000}
            to={props.to / 1000}
        />
    );
}

/** The charts to draw for the series a host has. Container charts take
 *  the known containers first and stop at maxChartSeries; the server
 *  leaves out containers without values in the range. */
function chartDefs(all: Series[], containers: Container[]): ChartDef[] {
    const has = (kind: string, metric: string) => all.some((s) => s.kind === kind && s.metric === metric);
    const entities = (kind: string, metric: string) => all.filter((s) => s.kind === kind && s.metric === metric).map((s) => s.entity);
    const host = (metric: string, label: string): Spec => ({label, series: {kind: 'host', entity: '', metric}});
    const each = (kind: string, metric: string, label: (e: string) => string): Spec[] =>
        entities(kind, metric).map((e) => ({label: label(e), series: {kind, entity: e, metric}}));
    const defs: ChartDef[] = [];

    if (has('host', 'cpu_busy_pct')) {
        defs.push({
            title: 'CPU',
            specs: [host('cpu_busy_pct', 'busy'), host('cpu_user_pct', 'user'), host('cpu_system_pct', 'system'), host('cpu_iowait_pct', 'iowait'), host('cpu_steal_pct', 'steal')],
            format: pct,
            range: [0, 100],
        });
    }
    if (has('host', 'mem_used_bytes')) {
        defs.push({
            title: 'Memory',
            specs: [host('mem_used_bytes', 'used'), host('mem_cache_bytes', 'cache'), host('mem_total_bytes', 'total'), host('swap_used_bytes', 'swap used')],
            format: bytes,
        });
    }
    if (has('host', 'load1')) {
        defs.push({title: 'Load', specs: [host('load1', '1 min'), host('load5', '5 min'), host('load15', '15 min')], format: num});
    }
    if (has('core', 'busy_pct')) {
        defs.push({title: 'CPU per core', specs: each('core', 'busy_pct', (e) => `core ${e}`), format: pct, range: [0, 100]});
    }
    for (const mount of entities('fs', 'size_bytes')) {
        defs.push({
            title: `Disk space ${mount}`,
            specs: [
                {label: 'used', series: {kind: 'fs', entity: mount, metric: 'used_bytes'}},
                {label: 'size', series: {kind: 'fs', entity: mount, metric: 'size_bytes'}},
            ],
            format: bytes,
        });
    }
    if (has('disk', 'read_bps')) {
        defs.push({
            title: 'Disk I/O',
            specs: [...each('disk', 'read_bps', (e) => `${e} read`), ...each('disk', 'write_bps', (e) => `${e} write`)],
            format: rate,
        });
        defs.push({title: 'Disk busy', specs: each('disk', 'busy_pct', (e) => e), format: pct, range: [0, 100]});
    }
    if (has('net', 'rx_bps')) {
        defs.push({title: 'Network received', specs: each('net', 'rx_bps', (e) => e), format: rate});
        defs.push({title: 'Network sent', specs: each('net', 'tx_bps', (e) => e), format: rate});
    }
    const known = new Set(containers.map((c) => c.name));
    const containerSpecs = (metric: string): Spec[] => {
        const specs = each('container', metric, (e) => e);
        specs.sort((a, b) => Number(known.has(b.label)) - Number(known.has(a.label)) || a.label.localeCompare(b.label));
        return specs.slice(0, maxChartSeries);
    };
    if (has('container', 'cpu_cores')) {
        defs.push({title: 'Container CPU (cores)', specs: containerSpecs('cpu_cores'), format: num});
    }
    if (has('container', 'mem_used_bytes')) {
        defs.push({title: 'Container memory', specs: containerSpecs('mem_used_bytes'), format: bytes});
    }
    // The server answers at most maxChartSeries series per chart.
    for (const d of defs) {
        d.specs = d.specs.slice(0, maxChartSeries);
    }
    return defs;
}

function Containers(props: {containers: Container[]}) {
    return (
        <section class="surface-1 flex flex-col gap-2">
            <h2>Containers</h2>
            <div class="overflow-x-auto">
                <table>
                    <thead>
                        <tr>
                            <th>Name</th>
                            <th>State</th>
                            <th>Health</th>
                            <th>Image</th>
                            <th>Restarts</th>
                            <th>Started</th>
                        </tr>
                    </thead>
                    <tbody>
                        {props.containers.map((c) => (
                            <tr key={c.name}>
                                <td>{c.name}</td>
                                <td>
                                    {c.state}
                                    {c.state === 'exited' ? ` (${c.exit_code})` : ''}
                                    {c.oom_killed ? ', OOM killed' : ''}
                                </td>
                                <td>{c.health ?? '-'}</td>
                                <td>
                                    <code>{c.image}</code>
                                </td>
                                <td>{c.restarts}</td>
                                <td>{c.started_at_ms ? dateTime(c.started_at_ms) : '-'}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </section>
    );
}

const eventLabels: Record<string, string> = {
    seq_gap: 'kanshi restarted',
    lost: 'samples lost',
    store_reset: 'kanshi store recreated',
    boot: 'server rebooted',
    errors: 'kanshi problems changed',
    container: 'container',
    pull_failed: 'pulling failed',
    pull_ok: 'pulling works again',
};

function Events(props: {events: HostEvent[] | undefined}) {
    const newest = [...(props.events ?? [])].reverse();
    return (
        <section class="surface-1 flex flex-col gap-2">
            <h2>Events</h2>
            {props.events === undefined && <small>Loading...</small>}
            {props.events !== undefined && newest.length === 0 && <small>No events in this range.</small>}
            <ul class="flex flex-col gap-2">
                {newest.slice(0, 200).map((e, i) => (
                    <li class="flex flex-col" key={`${e.t}-${i}`}>
                        <small>{dateTime(e.t)}</small>
                        <span>
                            <strong>{eventLabels[e.kind] ?? e.kind}</strong>
                            {e.subject ? ` ${e.subject}` : ''}: {eventText(e)}
                        </span>
                    </li>
                ))}
            </ul>
            {newest.length > 200 && <small>Showing the newest 200 of {newest.length}.</small>}
        </section>
    );
}

/** A short text for an event: container states are summarized. */
function eventText(e: HostEvent): string {
    if (e.kind !== 'container' || e.detail === 'gone') {
        return e.detail || '-';
    }
    try {
        const c = JSON.parse(e.detail) as Container;
        const parts = [c.state];
        if (c.health) {
            parts.push(c.health);
        }
        if (c.state === 'exited') {
            parts.push(`exit code ${c.exit_code}`);
        }
        if (c.oom_killed) {
            parts.push('OOM killed');
        }
        parts.push(c.image);
        return parts.join(', ');
    } catch {
        return e.detail;
    }
}
