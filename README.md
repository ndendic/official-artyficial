# Official Artyficial listings

The official listings of the Artyficial Space marketplace: plugins, shells, themes and, later, apps and
integrations for RT Station. This repository is the marketplace's default starting point.

A Device installs a listing straight from this repository's GitHub release for that listing's tag. The Space only
lists it by tag; it hosts no package bytes.

## Layout

| Folder | What |
|---|---|
| `plugins/<name>/` | RT Station Plugins |
| `shells/<name>/` | Page shells |
| `themes/<name>/` | Themes |
| `apps/<name>/` | Apps (Workspaces) |
| `integrations/<name>/` | Integrations |
| `templates/<name>/` | Starter Workspaces and page sets |

Each listing folder holds `.artyficial/extension.json`, a README and at least one screenshot.

## Tags

One tag per listing: `<name>--vX.Y.Z`. A tag releases only that listing's folder.

## Licence

MIT, see `LICENSE`, unless a listing's folder says otherwise for bundled third-party material.
