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

## Listings

Shells and themes for RT Station. Each one depends on RT Coat, the UI library the `rt` binary carries and serves
at `/static/ui/`; a listing ships only its own files. Each folder's README says how to install it on `rt` v0.20.1
(by hand) and on the next release (with `rt package`).

| Listing | Kind | Version | Tag | Note |
|---|---|---|---|---|
| [canvas](shells/canvas/) | shell | 0.1.5 | `canvas--v0.1.5` | the classic sidebar, title bar and inspector shell |
| [ribbon-sheet](shells/ribbon-sheet/) | shell | 0.1.6 | `ribbon-sheet--v0.1.6` | the blank Ribbon & Sheet; not carried by v0.20.1, installs by hand there |
| [ribbon-deck](shells/ribbon-deck/) | shell | 0.3.5 | `ribbon-deck--v0.3.5` | Ribbon & Sheet Showcase; the AI pill talks to the herdr console Plugin when it is installed |
| [bento](themes/bento/) | theme | 1.0.4 | `bento--v1.0.4` | the default, neutral personality with Signal Coral |
| [coast](themes/coast/) | theme | 1.0.3 | `coast--v1.0.3` | warm coastal: Wheat, Onyx, Oceanic, Nectarine |
| [folio](themes/folio/) | theme | 1.0.3 | `folio--v1.0.3` | editorial: warm paper, Libron and Literata |
| [harbor](themes/harbor/) | theme | 1.0.4 | `harbor--v1.0.4` | cool and technical blue-slate |
| [ledger](themes/ledger/) | theme | 1.0.4 | `ledger--v1.0.4` | executive: stone, evergreen and gold |
| [noir](themes/noir/) | theme | 1.0.2 | `noir--v1.0.2` | one lime on near-black; its night ramp needs ribbon-sheet 0.1.5+ or ribbon-deck 0.3.4+ |
| [relay](themes/relay/) | theme | 1.0.4 | `relay--v1.0.4` | futuristic graphite and electric cyan |
| [sprout](themes/sprout/) | theme | 1.0.4 | `sprout--v1.0.4` | playful cream, grape and tangerine |

## Tags

One tag per listing: `<name>--vX.Y.Z`. A tag releases only that listing's folder.

## Licence

MIT, see `LICENSE`, unless a listing's folder says otherwise for bundled third-party material.
