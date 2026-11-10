# herdr console (`artyficial.herdr`): changes

## 1.2.0

A page that cannot take Caller patches (a vouched Page) can now run the console: every call
answers with what it would have patched, and the page draws it.

- **Replies carry `views` and `agent`.** Two optional request flags: `views: true` sends nothing
  to the Caller patch path for that call and returns the patches as the reply key `views`
  (`{target, mode, tree}`, targets `console`, `agents`, `confirm`, `error`); `feed: true` (implies
  `views`) makes `follow` and `agents` a pull that answers only when the thread or the list
  changed. The chip comes back as `agent` (or `null` when it clears). The manifest's request and
  result schemas declare both, and tests check them against real replies.
- **Late draws reach every page.** The start of a session, a start error, "the session ended"
  and the chip clear reach a views page through the next reply; when a vouched and an
  unvouched tab share one browser session, each still gets its own copy.
- **A gone session ends cleanly.** A `follow` with `feed` or `views` of a pane herdr no longer
  knows answers `ok` with `following: ""` and `agent: null`, once; any other herdr error keeps
  the chip and the selection.
- **A failed start says so.** A start that fails while it waits answers `view: ""` with the
  error line as a `views` entry, never `starting`.
- **Bounded views.** A view stays under the shell's limits (3500 nodes, 224 KiB) and a reply
  under 15000 nodes and 768 KiB. A long answer is cut at its end with a line saying so; the
  confirm bar, the dialog card and the error line are never cut (a foot that alone is over the
  byte limit is kept whole), and a dialog screen keeps its end, where the prompt is.
- **Known limit:** late draws wait per browser session, not per tab (the request carries no
  tab id).

Compatible with 1.1.0: the capability `artyficial.herdr.console` stays at 1.0.0 (the new fields
are optional), the configuration is unchanged, and pages that take Caller patches (the pill of
rt v0.20.1 and of the ribbon-deck shell) work as before.

## 1.1.0

First release: drive herdr sessions from the pill of an RT Station Workspace (pick or start a
session, talk to it, apply the patch blocks it writes), the Agents panel with live status, and
`bin/release` for the Package Archive.
