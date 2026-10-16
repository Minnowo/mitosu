# mitosu web UI

Preact + TypeScript, built with Vite and Tailwind 4, charts by uPlot. The
build (web/dist) is embedded in the mitosu binary by web/embed.go; it is
not committed, so build it before `go build`:

```
scripts/web.sh        # or: make web
```

npm only runs inside a pinned Debian Node image (node:26-trixie-slim, by
digest, in scripts/web.sh); nothing is installed on the host. It installs
(with the network, install scripts off) only when package.json or the
lock file changed, and builds without the network. scripts/check-npm.sh
(run by check.sh) fails unless the direct packages are exactly
scripts/approved-npm.txt and every locked package comes from
registry.npmjs.org with an integrity hash.
`scripts/web.sh npm <args>` runs any other npm command the same way.

Only preact and uplot end up in the page; the rest are build tools.

## Files

| File               | What                                                     |
|--------------------|----------------------------------------------------------|
| src/main.tsx       | entry point; follows the system's light or dark setting  |
| src/App.tsx        | login check and routing                                  |
| src/Overview.tsx   | one card per host                                        |
| src/HostPage.tsx   | a host's charts, containers and events                   |
| src/Chart.tsx      | one uPlot chart (data aligned by /api/chart on the Go side) |
| src/useLoad.ts     | loading with refresh; a once-a-minute clock              |
| src/theme.ts       | light or dark, following the system                      |
| src/api.ts         | the JSON API (src/web on the Go side)                    |
| src/theme.css      | generated theme, see below                               |

## Theme

src/theme.css was generated once with ponkotsu-css
(github.com/Minnowo/ponkotsu-css, commit 7141d05, its default
src/seeds.json) by `npm run generate` in the same Node image, and is
committed as is. To change the colors, edit ponkotsu's seeds.json,
generate again and replace the file. Markup follows ponkotsu's SKILL.md:
elements by meaning, classes only for layout and variants.
