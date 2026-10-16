// Hash routing: #/ is the overview, #/host/<name>?range=24h a host.

import {useEffect, useState} from 'preact/hooks';

export interface Route {
    page: 'overview' | 'host';
    host: string;
    params: URLSearchParams;
}

export function parseRoute(hash: string): Route {
    const [path = '', query = ''] = hash.replace(/^#/, '').split('?', 2);
    const params = new URLSearchParams(query);
    const m = /^\/host\/(.+)$/.exec(path);
    if (m && m[1]) {
        return {page: 'host', host: decodeURIComponent(m[1]), params};
    }
    return {page: 'overview', host: '', params};
}

export function hostHref(host: string, params?: Record<string, string>): string {
    const query = params ? `?${new URLSearchParams(params).toString()}` : '';
    return `#/host/${encodeURIComponent(host)}${query}`;
}

/** The current route; re-renders on navigation. */
export function useRoute(): Route {
    const [route, setRoute] = useState(() => parseRoute(location.hash));
    useEffect(() => {
        const onChange = () => setRoute(parseRoute(location.hash));
        addEventListener('hashchange', onChange);
        return () => removeEventListener('hashchange', onChange);
    }, []);
    return route;
}
