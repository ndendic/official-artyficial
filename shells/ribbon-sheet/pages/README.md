# Sample Pages

Sample Pages that show this shell at work. Installing the shell never installs them: a new Workspace stays blank.

| Source | Slug | What it shows |
|---|---|---|
| `home.html` | `home` | the blank Workspace's welcome, served at `/` |
| `work.html` | `work` | Operations: KPI cards, charts and a data table (demo data in page signals) |
| `work-customers.html`, `work-orders.html` | `work-customers`, `work-orders` | its two sub-pages |
| `read.html` | `read` | a long read on the reading layout: byline, lead, figures and diagrams drawn in the theme's roles, tables, a chart, callouts, a pull quote, code, an accordion, footnotes |

The first line of each names the title and the layout (`<!-- page: title="…" layout="…" -->`). A Page's own sheet
is `web/pages/<slug>.css` (`read.css`, `work.css`).

## Publish one

With the Workspace running and an administrator signed in (`rt login --user <email>`), from the Workspace's
root folder (on rt v0.20.1 a Workspace keeps its web files in `.ds/web/`):

```sh
L=path/to/official-artyficial/shells/ribbon-sheet
cp "$L/web/pages/read.css" .ds/web/pages/read.css
rt page create read -t "From mood board to theme" --canvas "$L/pages/read.html"
```
