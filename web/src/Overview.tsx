// The overview: one card per configured host with its status, problems
// and current values.

import {api, type Container, type Host, type Latest} from './api';
import {age, bytes, num, pct} from './format';
import {Problems} from './Problems';
import {hostHref} from './route';
import {useLoad} from './useLoad';

const refreshMs = 30_000;

export function Overview(props: {onLoggedOut: () => void}) {
    const hosts = useLoad(() => api.hosts(), [], props.onLoggedOut, refreshMs);
    return (
        <main class="flex flex-col gap-4 px-4 pb-4">
            <h1>Servers</h1>
            {hosts.error && <p class="rounded-md p-3 bg-c-error-container text-c-on-error-container">{hosts.error}</p>}
            {!hosts.data && !hosts.error && <small>Loading...</small>}
            <div class="grid gap-4 md:grid-cols-2">
                {hosts.data?.map((h) => (
                    <HostCard host={h} key={h.name} onLoggedOut={props.onLoggedOut} />
                ))}
            </div>
        </main>
    );
}

function HostCard(props: {host: Host; onLoggedOut: () => void}) {
    const h = props.host;
    const latest = useLoad(() => api.latest(h.name), [h.name, h.last_sample_ms], props.onLoggedOut);
    const containers = useLoad(() => api.containers(h.name), [h.name, h.last_sample_ms], props.onLoggedOut);
    return (
        <section class="surface-1 flex flex-col gap-4">
            <div class="flex flex-wrap items-baseline justify-between gap-2">
                <h2>
                    <a href={hostHref(h.name)}>{h.name}</a>
                </h2>
                <small>{status(h)}</small>
            </div>
            {latest.data && <Stats latest={latest.data} containers={containers.data ?? []} />}
            <Problems problems={h.problems} />
        </section>
    );
}

/** One line on how fresh the data is. */
export function status(h: Host): string {
    const now = Date.now();
    if (h.pulling) {
        return 'pulling now';
    }
    if (h.last_sample_ms === 0) {
        return h.last_error ? 'never pulled' : 'no samples yet';
    }
    return `newest sample ${age(h.last_sample_ms, now)} ago`;
}

function Stats(props: {latest: Latest[]; containers: Container[]}) {
    const v = (kind: string, entity: string, metric: string) =>
        props.latest.find((l) => l.kind === kind && l.entity === entity && l.metric === metric)?.v;
    const busy = v('host', '', 'cpu_busy_pct');
    const memUsed = v('host', '', 'mem_used_bytes');
    const memTotal = v('host', '', 'mem_total_bytes');
    // The fullest filesystem is the one worth showing.
    let disk: {mount: string; used: number} | undefined;
    for (const l of props.latest) {
        if (l.kind !== 'fs' || l.metric !== 'size_bytes' || l.v <= 0) {
            continue;
        }
        const used = v('fs', l.entity, 'used_bytes');
        if (used !== undefined && (!disk || used / l.v > disk.used)) {
            disk = {mount: l.entity, used: used / l.v};
        }
    }
    const load = v('host', '', 'load1');
    const running = props.containers.filter((c) => c.state === 'running').length;
    const tiles: [string, string][] = [
        ['CPU', busy === undefined ? '-' : pct(busy)],
        ['Memory', memUsed === undefined || !memTotal ? '-' : `${bytes(memUsed)} of ${bytes(memTotal)}`],
        [disk ? `Disk ${disk.mount}` : 'Disk', disk ? `${pct(100 * disk.used)} used` : '-'],
        ['Load', load === undefined ? '-' : num(load)],
    ];
    if (props.containers.length > 0) {
        tiles.push(['Containers', `${running} of ${props.containers.length} running`]);
    }
    return (
        <div class="grid grid-cols-2 gap-2 sm:grid-cols-3">
            {tiles.map(([label, value]) => (
                <div class="surface-2 flex flex-col" key={label}>
                    <small>{label}</small>
                    <p class="text-2xl">{value}</p>
                </div>
            ))}
        </div>
    );
}
