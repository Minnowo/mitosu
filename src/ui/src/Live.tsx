// The live views of a host: read from the server when opened (over the
// admin login), never stored. Each view loads once, on "Reload" and on
// "Load older"; nothing refreshes on its own.

import {useEffect, useRef, useState} from 'preact/hooks';
import {api, LoggedOut, type ContainerInfo, type JournalEntry, type LogLine, type Login, type SudoEntry, type SystemInfo} from './api';
import {dateTime} from './format';
import {hostHref} from './route';

const partialNote =
    'This login only sees its own journal entries. Add it to group adm or systemd-journal on the server, or set live_sudo = on in mitosu.conf.';

interface Page<T> {
    items: T[];
    older: string;
    partial: boolean;
    truncated: boolean;
}

/** Loads pages of items, newest page first; "older" loads the page before
 *  the oldest one shown, by the cursor the server returned. Answers to
 *  requests that were replaced (new filter, new container) are dropped. */
function usePages<T>(load: (before: string) => Promise<Page<T>>, deps: unknown[], onLoggedOut: () => void) {
    const [items, setItems] = useState<T[] | undefined>(undefined);
    const [older, setOlder] = useState('');
    const [partial, setPartial] = useState(false);
    const [truncated, setTruncated] = useState(false);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const latest = useRef(0);

    const fetchPage = async (before: string, replace: boolean) => {
        const id = ++latest.current;
        setBusy(true);
        setError('');
        try {
            const page = await load(before);
            if (id !== latest.current) {
                return;
            }
            setPartial(page.partial);
            setTruncated(page.truncated);
            setOlder(page.older);
            setItems((old) => (replace || !old ? page.items : [...page.items, ...old]));
        } catch (e) {
            if (id !== latest.current) {
                return;
            }
            if (e instanceof LoggedOut) {
                onLoggedOut();
                return;
            }
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            if (id === latest.current) {
                setBusy(false);
            }
        }
    };
    useEffect(() => {
        setItems(undefined);
        setOlder('');
        void fetchPage('', true);
    }, deps);
    return {items, partial, truncated, error, busy, older: older ? () => fetchPage(older, false) : undefined, reload: () => fetchPage('', true)};
}

function Status(props: {error: string; partial?: boolean; truncated?: boolean; busy: boolean; empty: boolean}) {
    return (
        <>
            {props.truncated && <small>The answer was cut off at its size limit; the oldest part of this page is missing.</small>}
            {props.error && <p class="rounded-md p-3 bg-c-error-container text-c-on-error-container">{props.error}</p>}
            {props.partial && <p class="rounded-md p-3 bg-c-tertiary-container text-c-on-tertiary-container">{partialNote}</p>}
            {props.busy && <small>Loading...</small>}
            {!props.busy && !props.error && props.empty && <small>Nothing found.</small>}
        </>
    );
}

function Older(props: {older?: () => void; busy: boolean}) {
    return props.older ? (
        <button class="self-start" onClick={props.older} disabled={props.busy}>
            Load older
        </button>
    ) : null;
}

export function LiveLogs(props: {host: string; container: string; containers: string[]; onLoggedOut: () => void}) {
    const p = usePages<LogLine>(
        async (before) => {
            const r = await api.liveLogs(props.host, props.container, 200, before);
            return {items: r.lines, older: r.older, partial: false, truncated: r.truncated};
        },
        [props.host, props.container],
        props.onLoggedOut,
    );
    return (
        <section class="surface-1 flex min-w-0 flex-col gap-2">
            <div class="flex flex-wrap items-center gap-2">
                <h2 class="flex-1">Logs</h2>
                <label class="flex items-center gap-2">
                    Container
                    <select value={props.container} onChange={(e) => (location.hash = hostHref(props.host, {view: 'logs', container: e.currentTarget.value}))}>
                        {!props.container && <option value="">Choose...</option>}
                        {props.containers.map((c) => (
                            <option key={c} value={c}>
                                {c}
                            </option>
                        ))}
                    </select>
                </label>
                <button onClick={p.reload} disabled={p.busy || !props.container}>
                    Reload
                </button>
            </div>
            {props.container ? (
                <>
                    <Older older={p.older} busy={p.busy} />
                    <Status error={p.error} truncated={p.truncated} busy={p.busy} empty={p.items?.length === 0} />
                    <pre class="overflow-x-auto whitespace-pre-wrap break-all">
                        {(p.items ?? []).map((l) => `${l.t ? dateTime(l.t) : ''}  ${l.text}`).join('\n')}
                    </pre>
                </>
            ) : (
                <small>Choose a container.</small>
            )}
        </section>
    );
}

const priorities = ['emerg', 'alert', 'crit', 'err', 'warning', 'notice', 'info', 'debug'];

export function LiveJournal(props: {host: string; onLoggedOut: () => void}) {
    const [unit, setUnit] = useState('');
    const [priority, setPriority] = useState(-1);
    const [query, setQuery] = useState({unit: '', priority: -1});
    const p = usePages<JournalEntry>(
        async (before) => {
            const r = await api.liveJournal(props.host, query.unit, query.priority, 200, before);
            return {items: r.entries, older: r.older, partial: r.partial, truncated: r.truncated};
        },
        [props.host, query.unit, query.priority],
        props.onLoggedOut,
    );
    return (
        <section class="surface-1 flex min-w-0 flex-col gap-2">
            <h2>Journal</h2>
            <form
                class="flex flex-wrap items-end gap-2"
                onSubmit={(e) => {
                    e.preventDefault();
                    setQuery({unit, priority});
                }}
            >
                <label class="flex flex-col gap-1">
                    Unit
                    <input value={unit} placeholder="all units, or e.g. ssh.service" onInput={(e) => setUnit(e.currentTarget.value)} />
                </label>
                <label class="flex flex-col gap-1">
                    Priority
                    <select value={String(priority)} onChange={(e) => setPriority(Number(e.currentTarget.value))}>
                        <option value="-1">all</option>
                        {priorities.map((name, i) => (
                            <option key={name} value={String(i)}>
                                {name} and worse
                            </option>
                        ))}
                    </select>
                </label>
                <button type="submit" class="btn-primary">
                    Show
                </button>
            </form>
            <Older older={p.older} busy={p.busy} />
            <Status error={p.error} partial={p.partial} truncated={p.truncated} busy={p.busy} empty={p.items?.length === 0} />
            <div class="overflow-x-auto">
                <table>
                    <tbody>
                        {(p.items ?? []).map((e, i) => (
                            <tr key={`${e.t}-${i}`}>
                                <td class="whitespace-nowrap">
                                    <small>{dateTime(e.t)}</small>
                                </td>
                                <td>{priorities[e.priority] ?? e.priority}</td>
                                <td>{e.unit || e.ident}</td>
                                <td class="break-all">{e.message}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </section>
    );
}

export function LiveLogins(props: {host: string; onLoggedOut: () => void}) {
    const p = usePages<Login>(
        async (before) => {
            const r = await api.liveLogins(props.host, 1000, before);
            return {items: r.logins, older: r.older, partial: r.partial, truncated: r.truncated};
        },
        [props.host],
        props.onLoggedOut,
    );
    const failed = (p.items ?? []).filter((l) => l.result !== 'accepted').length;
    return (
        <section class="surface-1 flex min-w-0 flex-col gap-2">
            <h2>SSH logins</h2>
            {p.items && <small>{`${p.items.length - failed} accepted, ${failed} failed in the lines read`}</small>}
            <Older older={p.older} busy={p.busy} />
            <Status error={p.error} partial={p.partial} truncated={p.truncated} busy={p.busy} empty={p.items?.length === 0} />
            <div class="overflow-x-auto">
                <table>
                    <thead>
                        <tr>
                            <th>Time</th>
                            <th>Result</th>
                            <th>User</th>
                            <th>From</th>
                            <th>Method</th>
                        </tr>
                    </thead>
                    <tbody>
                        {[...(p.items ?? [])].reverse().map((l, i) => (
                            <tr key={`${l.t}-${i}`}>
                                <td class="whitespace-nowrap">{dateTime(l.t)}</td>
                                <td>{l.result === 'accepted' ? 'accepted' : l.result === 'failed' ? 'failed' : 'unknown user'}</td>
                                <td>{l.user}</td>
                                <td>{l.from}</td>
                                <td>{l.method || '-'}</td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </section>
    );
}

export function LiveSudo(props: {host: string; onLoggedOut: () => void}) {
    const p = usePages<SudoEntry>(
        async (before) => {
            const r = await api.liveSudo(props.host, 1000, before);
            return {items: r.entries, older: r.older, partial: r.partial, truncated: r.truncated};
        },
        [props.host],
        props.onLoggedOut,
    );
    return (
        <section class="surface-1 flex min-w-0 flex-col gap-2">
            <h2>sudo</h2>
            <Older older={p.older} busy={p.busy} />
            <Status error={p.error} partial={p.partial} truncated={p.truncated} busy={p.busy} empty={p.items?.length === 0} />
            <div class="overflow-x-auto">
                <table>
                    <thead>
                        <tr>
                            <th>Time</th>
                            <th>User</th>
                            <th>As</th>
                            <th>Command</th>
                        </tr>
                    </thead>
                    <tbody>
                        {[...(p.items ?? [])].reverse().map((s, i) => (
                            <tr key={`${s.t}-${i}`}>
                                <td class="whitespace-nowrap">{dateTime(s.t)}</td>
                                <td>{s.user}</td>
                                <td>{s.run_as || '-'}</td>
                                <td class="break-all">
                                    {s.problem && <strong>{s.problem}: </strong>}
                                    <code>{s.command}</code>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>
            </div>
        </section>
    );
}

/** Loads one result once (and on reload). */
function useOnce<T>(load: () => Promise<T>, deps: unknown[], onLoggedOut: () => void) {
    const [data, setData] = useState<T | undefined>(undefined);
    const [error, setError] = useState('');
    const [busy, setBusy] = useState(false);
    const reload = async () => {
        setBusy(true);
        setError('');
        try {
            setData(await load());
        } catch (e) {
            if (e instanceof LoggedOut) {
                onLoggedOut();
                return;
            }
            setError(e instanceof Error ? e.message : String(e));
        } finally {
            setBusy(false);
        }
    };
    useEffect(() => {
        setData(undefined);
        void reload();
    }, deps);
    return {data, error, busy, reload};
}

export function LiveSystem(props: {host: string; onLoggedOut: () => void}) {
    const s = useOnce<SystemInfo>(() => api.liveSystem(props.host), [props.host], props.onLoggedOut);
    const d = s.data;
    return (
        <section class="surface-1 flex min-w-0 flex-col gap-4">
            <div class="flex items-center gap-2">
                <h2 class="flex-1">System</h2>
                <button onClick={s.reload} disabled={s.busy}>
                    Reload
                </button>
            </div>
            <Status error={s.error} busy={s.busy} empty={false} />
            {d && (
                <>
                    <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                        <dt>OS</dt>
                        <dd>{d.os || '-'}</dd>
                        <dt>Kernel</dt>
                        <dd>{d.kernel || '-'}</dd>
                        <dt>CPU</dt>
                        <dd>{d.cpu_model ? `${d.cpus} x ${d.cpu_model}` : d.cpus || '-'}</dd>
                        <dt>Up for</dt>
                        <dd>{d.uptime_seconds ? `${Math.floor(d.uptime_seconds / 86400)} days` : '-'}</dd>
                        <dt>Docker</dt>
                        <dd>{d.docker_version || '- (not installed, or no access)'}</dd>
                    </dl>
                    {d.reboot_required && (
                        <p class="rounded-md p-3 bg-c-tertiary-container text-c-on-tertiary-container">
                            A reboot is required{d.reboot_packages.length > 0 ? ` (for ${d.reboot_packages.join(', ')})` : ''}.
                        </p>
                    )}
                    {d.failed_units.length > 0 && (
                        <p class="rounded-md p-3 bg-c-error-container text-c-on-error-container">Failed units: {d.failed_units.join(', ')}</p>
                    )}
                    <div class="flex flex-col gap-2">
                        <h3>Upgradable packages ({d.updates.length})</h3>
                        <small>From the package lists as they are on the server; mitosu never runs apt update.</small>
                        {d.updates.length > 0 && (
                            <div class="overflow-x-auto">
                                <table>
                                    <tbody>
                                        {d.updates.map((u) => (
                                            <tr key={u.package}>
                                                <td>{u.package}</td>
                                                <td>
                                                    {u.from} to {u.version}
                                                </td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            </div>
                        )}
                    </div>
                </>
            )}
        </section>
    );
}

export function LiveInspect(props: {host: string; container: string; onLoggedOut: () => void}) {
    const s = useOnce<ContainerInfo>(() => api.liveInspect(props.host, props.container), [props.host, props.container], props.onLoggedOut);
    const d = s.data;
    return (
        <section class="surface-1 flex min-w-0 flex-col gap-4">
            <div class="flex items-center gap-2">
                <h2 class="flex-1">Container {props.container}</h2>
                <a href={hostHref(props.host, {view: 'logs', container: props.container})}>Logs</a>
            </div>
            <Status error={s.error} busy={s.busy} empty={false} />
            {d && (
                <>
                    <dl class="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
                        <dt>Image</dt>
                        <dd>
                            <code>{d.image}</code>
                        </dd>
                        <dt>State</dt>
                        <dd>
                            {d.state}
                            {d.health ? `, ${d.health}` : ''}
                            {d.state === 'exited' ? `, exit code ${d.exit_code}` : ''}
                            {d.oom_killed ? ', OOM killed' : ''}
                        </dd>
                        <dt>Restart policy</dt>
                        <dd>
                            {d.restart_policy || '-'} ({d.restart_count} restarts)
                        </dd>
                        <dt>Compose</dt>
                        <dd>{d.project ? `${d.project} / ${d.service}` : '-'}</dd>
                        <dt>Created</dt>
                        <dd>{d.created}</dd>
                    </dl>
                    <h3>Ports</h3>
                    {d.ports.length === 0 ? <small>None.</small> : (
                        <ul>
                            {d.ports.map((p) => (
                                <li key={p.container + p.host_ip + p.host_port}>
                                    {p.container} {p.host_port ? `published on ${p.host_ip || '*'}:${p.host_port}` : '(not published)'}
                                </li>
                            ))}
                        </ul>
                    )}
                    <h3>Mounts</h3>
                    {d.mounts.length === 0 ? <small>None.</small> : (
                        <ul>
                            {d.mounts.map((m) => (
                                <li key={m.destination}>
                                    <code>{m.destination}</code> from {m.type} <code>{m.source}</code> {m.rw ? '' : '(read-only)'}
                                </li>
                            ))}
                        </ul>
                    )}
                    <h3>Networks</h3>
                    <ul>
                        {d.networks.map((n) => (
                            <li key={n.name}>
                                {n.name} {n.ip}
                            </li>
                        ))}
                    </ul>
                    <small>Environment variables and command lines are never read.</small>
                </>
            )}
        </section>
    );
}
