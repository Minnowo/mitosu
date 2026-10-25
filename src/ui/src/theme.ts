// The light or dark theme, following the system setting.

import {useEffect, useState} from 'preact/hooks';

const light = matchMedia('(prefers-color-scheme: light)');

/** Sets data-theme on <html> from the system setting. */
export function applyTheme(): void {
    if (light.matches) {
        document.documentElement.dataset.theme = 'light';
    } else {
        delete document.documentElement.dataset.theme;
    }
}

/** The current theme; re-renders when the system setting changes. */
export function useTheme(): 'light' | 'dark' {
    const [theme, setTheme] = useState<'light' | 'dark'>(light.matches ? 'light' : 'dark');
    useEffect(() => {
        const onChange = () => {
            applyTheme();
            setTheme(light.matches ? 'light' : 'dark');
        };
        light.addEventListener('change', onChange);
        return () => light.removeEventListener('change', onChange);
    }, []);
    return theme;
}
