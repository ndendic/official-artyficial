# Sample Pages

Sample Pages that show this shell at work. Installing the shell never installs them: copy them into a Workspace
only if you want to see the shell with content.

| Source | Slug | What it shows |
|---|---|---|
| `home.html` | `home` | a blank-page welcome |
| `work.html` | `work` | Operations: KPI cards, charts and a data table (demo data in page signals) |
| `work-orders.html` | `work-orders` | Orders, a sub-page of `work` |
| `work-customers.html` | `work-customers` | Customers, a sub-page of `work` |

The first line of each source names the title and the layout (`<!-- page: title="…" layout="…" -->`). `layout`
is `doc` (the document measure), `reading` (the reading measure) or `""` (the full canvas). A Page's own sheet is
`web/pages/<slug>.css` (`home.css`, `work.css`); the shell links the current Page's sheet through `{{.PageSheet}}`.

## Publish one

With the Workspace running and an administrator signed in (`rt login --user <email>`), from the Workspace's
root folder (on rt v0.20.1 a Workspace keeps its web files in `.ds/web/`):

```sh
L=path/to/official-artyficial/shells/ribbon-deck
cp "$L/web/pages/work.css" .ds/web/pages/work.css
rt page create work -t "Operations" --canvas "$L/pages/work.html"
```

## Write your own

The Pages are built from RT Coat, the UI library the `rt` binary serves under `/static/ui/`: `rt-*` elements and
`.rt-*` classes, painted by `--rt-*` tokens. Give every top-level region an `id`, keep page state in page signals
(`data-signals`), and put arrangement in the Page's sheet, never in inline styles.
