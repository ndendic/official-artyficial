# Plugin provenance

Every `.js` file here is an ES module that registers Datastar attribute plugins on
the page's engine: the official Datastar v1.0.4 + Rocket bundle (MIT, Star
Federation), served by the binary by default or from jsDelivr with
`DATASTAR_SRC=cdn`. The page maps the bare specifier `datastar` to that
URL in an import map, so the plugins, the UI library and the page share one
engine instance. Loaded once via `index.js`.

| File | Attribute(s) | Source | Licence |
|---|---|---|---|
| on-keys.js | `data-on-keys` | npm `@mbolli/datastar-attribute-on-keys` 1.2.0 (its `dist/index.js`, unchanged) | MIT, © 2025 Michael Bolli at zwei und eins gmbh and datastar-attribute-on-keys contributors; the full text is `LICENSE-on-keys.txt` beside it |
| persist.js | `data-persist` | Nikola Dendić's Datastar plugins, `data-persist` 0.1.0 (dist) | MIT |
| resize.js | `data-resize` | Nikola Dendić's Datastar plugins, `data-resize` 1.0.0 (dist) | MIT |
| scroll.js | `data-scroll` | Nikola Dendić's Datastar plugins, `data-scroll` 1.0.0 (dist) | MIT |
| anchor.js | `data-anchor` | Nikola Dendić's Datastar plugins, `data-anchor` 1.0.0 (dist) | MIT |
| drag.js | `data-draggable` | Nikola Dendić's Datastar plugins, `data-drag` 1.0.0 (dist) | MIT |
| split.js | `data-split` | Nikola Dendić's Datastar plugins, `data-split` 1.0.0 (dist) | MIT |
| canvas.js | `data-canvas` | Nikola Dendić's Datastar plugins, `data-canvas` 1.0.0 (dist) | MIT |
| chart.js | `data-chart` | RT Coat; wraps ApexCharts 3.54.1 (MIT), served by the binary at `/static/vendor/apexcharts/` (the import map's `apexcharts`, SRI-pinned) and added on first use | MIT; ApexCharts is MIT, its `LICENSE` is served beside it |
| phi.js | `data-observe-resize`, `data-resize-pane`, `data-snap-drag` | not in this listing: `rt init` writes it into every Workspace's `plugins/`, and `index.js` loads it from there | (provided by `rt`) |
| code.js | `data-code` | RT Coat; a copyable block from authored `<template>` markup, built on code-block.js | MIT |
| toast.js | `data-toast` | RT Coat; the toast stack behind `window.toast()` | MIT |
| demo.js | `data-demo` | RT Coat; reads the page's own stylesheets back to build the CSS tab | MIT |
| code-block.js | — (a module, not a plugin) | RT Coat; lazily imports highlight.js 11.9.0 (core + xml, css, javascript, json, bash, go, plaintext), served by the binary at `/static/vendor/highlight/` (the import map's `highlight.js`) | MIT; highlight.js is BSD-3-Clause, its `LICENSE` is served beside it |

`on-keys.js` is third-party code: it is © 2025 Michael Bolli at zwei und eins
gmbh and datastar-attribute-on-keys contributors, under MIT, and
`LICENSE-on-keys.txt` carries its copyright line and permission notice. Every
other file here is © 2026 Nikola Dendić, under MIT (the listing's `LICENSE`).

`code-block.js` registers no attribute and owns no element: it is the shared
block-of-code builder `demo.js` and `code.js` use. Its highlight.js import is dynamic and its
failure is caught: a missing file leaves the code unpainted and still
copyable, so no page depends on it to work.

Nothing here reaches a CDN. The two libraries the plugins load, ApexCharts
and highlight.js, are served by the binary; each plugin finds its hashed URL
through the page's import map, and falls back to the same file at its
unhashed `/static/vendor/…` path on a shell whose import map predates the
entry.

Not here on purpose: `data-theme-switch`. It is the UI library's
`/static/ui/plugins/theme.js`, served by the binary, and `index.js` imports
that URL. `rt-theme-switcher` imports the same URL, so there is one module
instance; a copy in this directory would register the attribute twice.

No plugin here needs Datastar Pro, and the engine contains none of it. Every
attribute above comes from these open-source files or from the engine itself.

The dists were built against Datastar 1.0.0-RC.6/RC.7; they use only `attribute`,
`effect`, `getPath`, `mergePatch`, `beginBatch`, `endBatch`, all of which the
v1.0.4 bundle still exports. Source maps were dropped (`.map` files not copied).
