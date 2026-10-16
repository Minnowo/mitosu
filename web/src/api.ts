// Calls to mitosu's JSON API (src/web on the Go side). Every call sends
// the X-Mitosu header the server requires; a 401 means the session ended.

export interface Problem {
    severity: 'critical' | 'warning';
    subject: string;
    message: string;
}

export interface Host {
    name: string;
    store: string;
    next_seq: number;
    last_sample_ms: number;
    pulling: boolean;
    last_attempt_ms: number;
    last_success_ms: number;
    last_error: string;
    last_samples: number;
    next_pull_ms: number;
    pull_interval_ms: number;
    problems: Problem[];
}

export interface Latest {
    kind: string;
    entity: string;
    metric: string;
    t: number;
    v: number;
}

export interface Series {
    kind: string;
    entity: string;
    metric: string;
}

export interface ChartData {
    /** Width of one point in ms (for raw samples, their usual distance). */
    step: number;
    /** Times in ms; a time where every value is null marks a gap. */
    times: number[];
    /** The requested series that have values, in request order. */
    series: Series[];
    /** values[i][j]: series i at times[j], null where it has none. */
    values: (number | null)[][];
}

export interface HostEvent {
    t: number;
    kind: string;
    store: string;
    seq_from: number;
    seq_to: number;
    subject: string;
    detail: string;
}

export interface Container {
    name: string;
    id: string;
    image: string;
    state: string;
    health?: string;
    restarts: number;
    exit_code: number;
    oom_killed: boolean;
    started_at_ms: number;
}

export interface Session {
    login: boolean;
    logged_in: boolean;
}

/** Thrown when the server answers 401: the UI shows the login form. */
export class LoggedOut extends Error {}

async function call(method: string, path: string, body?: unknown): Promise<Response> {
    const resp = await fetch(path, {
        method,
        headers: {'X-Mitosu': '1', ...(body === undefined ? {} : {'Content-Type': 'application/json'})},
        body: body === undefined ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
    });
    if (resp.status === 401 && path !== '/api/login') {
        throw new LoggedOut();
    }
    return resp;
}

async function get<T>(path: string): Promise<T> {
    const resp = await call('GET', path);
    if (!resp.ok) {
        throw new Error(await errorText(resp));
    }
    return (await resp.json()) as T;
}

async function errorText(resp: Response): Promise<string> {
    try {
        const body = (await resp.json()) as {error?: string};
        return body.error ?? `HTTP ${resp.status}`;
    } catch {
        return `HTTP ${resp.status}`;
    }
}

const q = (params: Record<string, string | number>) => new URLSearchParams(Object.entries(params).map(([k, v]) => [k, String(v)])).toString();

export const api = {
    session: () => get<Session>('/api/session'),
    hosts: () => get<Host[]>('/api/hosts'),
    latest: (host: string) => get<Latest[]>(`/api/latest?${q({host})}`),
    series: (host: string) => get<Series[]>(`/api/series?${q({host})}`),
    containers: (host: string) => get<Container[]>(`/api/containers?${q({host})}`),
    events: (host: string, from: number, to: number) => get<HostEvent[]>(`/api/events?${q({host, from, to})}`),
    chart: (host: string, series: Series[], from: number, to: number, max: number) => {
        const params = new URLSearchParams({host, from: String(from), to: String(to), max: String(max)});
        for (const s of series) {
            params.append('s', `${s.kind}|${s.entity}|${s.metric}`);
        }
        return get<ChartData>(`/api/chart?${params.toString()}`);
    },

    /** Logs in; resolves to an error message, or '' on success. */
    async login(password: string): Promise<string> {
        const resp = await call('POST', '/api/login', {password});
        return resp.ok ? '' : await errorText(resp);
    },
    async logout(): Promise<void> {
        await call('POST', '/api/logout');
    },
    async pullNow(host: string): Promise<void> {
        const resp = await call('POST', `/api/pull?${q({host})}`);
        if (!resp.ok) {
            throw new Error(await errorText(resp));
        }
    },
};
