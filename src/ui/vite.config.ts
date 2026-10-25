// Build for the web UI embedded in the mitosu binary (src/ui/embed.go).
import {defineConfig} from 'vite';
import preact from '@preact/preset-vite';
import tailwindcss from '@tailwindcss/vite';

export default defineConfig({
    plugins: [preact(), tailwindcss()],
    build: {
        outDir: 'dist',
        emptyOutDir: true,
        // Everything is served by mitosu itself; no inline scripts, so the
        // Content-Security-Policy can stay at default-src 'self'.
        assetsInlineLimit: 0,
        modulePreload: {polyfill: false},
    },
});
