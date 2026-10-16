// Loads data on mount, when its inputs change, and every refreshMs. Only
// the newest request's answer is kept: an older one arriving late is
// dropped.

import {useEffect, useRef, useState} from 'preact/hooks';
import {LoggedOut} from './api';

export interface Loaded<T> {
    data: T | undefined;
    error: string;
}

/** Runs load now, when deps change and every refreshMs (if given). A
 *  LoggedOut error calls onLoggedOut instead of showing an error. */
export function useLoad<T>(load: () => Promise<T>, deps: unknown[], onLoggedOut: () => void, refreshMs?: number): Loaded<T> {
    const [state, setState] = useState<Loaded<T>>({data: undefined, error: ''});
    const latest = useRef(0);
    useEffect(() => {
        let alive = true;
        const run = () => {
            const id = ++latest.current;
            load().then(
                (data) => {
                    if (alive && id === latest.current) {
                        setState({data, error: ''});
                    }
                },
                (err: unknown) => {
                    if (!alive || id !== latest.current) {
                        return;
                    }
                    if (err instanceof LoggedOut) {
                        onLoggedOut();
                        return;
                    }
                    setState((s) => ({data: s.data, error: String(err instanceof Error ? err.message : err)}));
                },
            );
        };
        run();
        const timer = refreshMs ? setInterval(run, refreshMs) : undefined;
        return () => {
            alive = false;
            if (timer) {
                clearInterval(timer);
            }
        };
    }, deps);
    return state;
}

/** A timestamp (ms) that moves to the current time every everyMs: a
 *  stable "now" for request parameters between refreshes. */
export function useClock(everyMs: number): number {
    const [now, setNow] = useState(() => Date.now());
    useEffect(() => {
        const timer = setInterval(() => setNow(Date.now()), everyMs);
        return () => clearInterval(timer);
    }, [everyMs]);
    return now;
}
