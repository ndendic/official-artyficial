// Datastar plugin set for this workspace — one module script loads it.
// Each plugin file registers itself on the engine the page already runs; the
// "datastar" specifier resolves through the import map in index.html (the
// binary writes it from DATASTAR_SRC) to the Datastar v1.0.4 + Rocket engine,
// so there is exactly one engine instance.
// fold.js (window.fold, used inside expressions) is a classic script in the
// head so it exists before Datastar's first scan. Provenance: LICENSES.md.
//
// LOADED ON SIGHT. A plugin file is imported the first time an element with
// its attribute is in the document — on first load or in a later patch — the
// way the UI library autoloads an rt-* element. A page that uses no plugin
// downloads none, and the home page downloads the one it uses (data-code).
// Datastar applies a plugin registered after its first scan to the whole
// document, so a late registration misses nothing. To load one up front,
// import it below the PLUGINS table (`import "./toast.js";`).
//
// IF YOUR PLUGIN BUILDS DOM, READ THIS FIRST. A Datastar attribute you set on
// an element during your own apply() is never scanned: the engine is already
// inside its application pass and will not look again. Set it one task later —
// setTimeout(…, 0) on an element that is in the document by then — and it is
// picked up normally. demo.js does this to hand its stage to data-resize-pane.
// The companion trap: a region patched over your subtree (`patch elements -s
// #main -m inner`) morphs your generated DOM away, and idiomorph REUSES
// nodes, so keeping references and re-appending them is not a fix. Put
// data-ignore-morph in the AUTHORED markup — it has to be on both sides.

// data-theme-switch — personality × mode, persisted. The UI library's copy,
// served by the binary: rt-theme-switcher imports this same module (the import
// map sends this URL to the release's versioned copy), and a second copy
// would be a second module instance registering the attribute twice. It is
// small, and the shell's theme switcher needs it on every page.
import "/static/ui/plugins/theme.js";

// attribute (without "data-" and modifiers) → the file that registers it
const PLUGINS = {
	"on-keys": "./on-keys.js",          // data-on-keys:ctrl-k / :escape.enter  (mbolli)
	"persist": "./persist.js",          // data-persist="a,b" / data-persist__session (ndendic)
	"resize": "./resize.js",            // data-resize → $resize_* window + element signals (ndendic)
	"scroll": "./scroll.js",            // data-scroll → $scroll_* signals (ndendic)
	"anchor": "./anchor.js",            // data-anchor — CSS anchor positioning (ndendic)
	"draggable": "./drag.js",           // data-draggable (ndendic)
	"split": "./split.js",              // data-split — split panes (ndendic)
	"canvas": "./canvas.js",            // data-canvas (ndendic)
	"observe-resize": "./phi.js",       // data-observe-resize, data-resize-pane,
	"resize-pane": "./phi.js",          // data-snap-drag (phi.js: rt writes it into every Workspace; not in this listing)
	"snap-drag": "./phi.js",
	"demo": "./demo.js",                // data-demo — the component demo: preview / HTML / CSS by layer (RT Coat)
	"code": "./code.js",                // data-code — a copyable block from authored <template> markup (RT Coat)
	"toast": "./toast.js",              // data-toast — the toast stack: dwell, pause, focus, cap (RT Coat)
	"chart": "./chart.js",              // data-chart — ApexCharts in the theme, from a signal; opt-in, loads ApexCharts itself
	"cmd": "/static/ui/plugins/cmd.js",   // data-cmd — send the Record in focus to a Command on a Domain Path (the UI Library's copy)
	"feed": "/static/ui/plugins/feed.js", // data-feed — read a Domain Path's Records; drives rt-data-table (the UI Library's copy)
};

// Workspace defaults, applied as the plugin loads (both read their config once,
// at apply time, which comes after registration): drag lists reorder between
// drop zones; the canvas draws no grid of its own — the theme paints the
// surface, so it follows light and dark.
const CONFIGURE = {
	"./drag.js": (m) => m.setConfig({ mode: "sortable" }),
	"./canvas.js": (m) => m.setConfig({ backgroundColor: "transparent", enableGrid: false, minZoom: 0.4, maxZoom: 3 }),
};

const loading = new Map();

// loadPlugin imports the file that registers a plugin (an attribute name from
// PLUGINS, or a file path), once.
export function loadPlugin(nameOrFile) {
	const file = PLUGINS[nameOrFile] || nameOrFile;
	if (!loading.has(file)) {
		loading.set(file, import(file).then((m) => {
			CONFIGURE[file]?.(m);
			return m;
		}).catch((err) => {
			console.warn(`[plugins] ${file} failed to load:`, err);
		}));
	}
	return loading.get(file);
}

// pluginOf is the PLUGINS key of an attribute name, or undefined:
// data-on-keys:ctrl-k → on-keys, data-persist__session → persist.
function pluginOf(attr) {
	if (!attr || !attr.startsWith("data-")) return undefined;
	const key = attr.slice(5).split(":")[0].split("__")[0];
	return Object.hasOwn(PLUGINS, key) ? key : undefined;
}

const wanted = new Set(Object.values(PLUGINS));

function scan(el) {
	for (const a of el.attributes) {
		const key = pluginOf(a.name);
		if (key) loadPlugin(key);
	}
}

function scanTree(root) {
	if (root.nodeType !== 1) return;
	scan(root);
	for (const el of root.querySelectorAll("*")) scan(el);
}

function done() {
	for (const f of wanted) if (!loading.has(f)) return false;
	return true;
}

scanTree(document.documentElement);
if (!done()) {
	const seen = new MutationObserver((records) => {
		for (const r of records) {
			if (r.type === "attributes") {
				const key = pluginOf(r.attributeName);
				if (key) loadPlugin(key);
			} else {
				for (const n of r.addedNodes) scanTree(n);
			}
		}
		if (done()) seen.disconnect();
	});
	seen.observe(document.documentElement, { subtree: true, childList: true, attributes: true });
}
