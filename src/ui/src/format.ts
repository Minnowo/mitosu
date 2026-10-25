// Number and time formatting for labels and axes.

const binary = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB'];

/** 1536 -> "1.5 KiB" */
export function bytes(v: number): string {
    let i = 0;
    let x = Math.abs(v);
    while (x >= 1024 && i < binary.length - 1) {
        x /= 1024;
        i++;
    }
    return `${v < 0 ? '-' : ''}${x < 10 && i > 0 ? x.toFixed(1) : Math.round(x)} ${binary[i]}`;
}

/** Bytes per second. */
export function rate(v: number): string {
    return `${bytes(v)}/s`;
}

export function pct(v: number): string {
    return `${v < 10 ? v.toFixed(1) : Math.round(v)}%`;
}

export function num(v: number): string {
    if (Math.abs(v) >= 100) {
        return String(Math.round(v));
    }
    return v.toFixed(Math.abs(v) >= 10 ? 1 : 2);
}

/** Milliseconds ago as "3 min", "2 h", "5 d". */
export function age(ms: number, now: number): string {
    const s = Math.max(0, Math.round((now - ms) / 1000));
    if (s < 90) {
        return `${s} s`;
    }
    if (s < 90 * 60) {
        return `${Math.round(s / 60)} min`;
    }
    if (s < 36 * 3600) {
        return `${Math.round(s / 3600)} h`;
    }
    return `${Math.round(s / 86400)} d`;
}

export function dateTime(ms: number): string {
    return new Date(ms).toLocaleString();
}
