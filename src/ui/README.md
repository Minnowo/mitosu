# mitosu web UI

The UI usese TypeScript Preact with Tailwind 4.
It's embedded into the final build and not in source control
so it's required to be built at least once before building the go binary.

Since I don't trust npm / Node, this project is setup to run them inside a Docker image.
If you don't care about this you can forgo the build script and run `npm build` directly.

Otherwise you can build the UI using the script:
```
scripts/web.sh        # or: make web
```

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

Theme is generated from [ponkotsu-css](github.com/Minnowo/ponkotsu-css)
