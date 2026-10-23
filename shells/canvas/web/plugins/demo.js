// demo.js — data-demo: one component, shown three ways.
//
//   <div class="demo" id="demo-kv" data-demo="{ w: 720 }">
//     <div class="demo-source">  … the live markup …  </div>
//   </div>
//
// Preview  the markup running inside a REAL nested viewport the reader can
//          drag, so a @media breakpoint in the component answers to the pane
//          and not to the browser window (see components/demo.css for why).
// HTML     that same markup, verbatim — there is one copy of it, in the page,
//          and both the preview and this tab are made from it.
// CSS      two things a reader can USE and nothing else: the <link> this page
//          loads, and ONE copyable file holding every rule that matches the
//          specimen, the @layer blocks separated inside it and one comment
//          line per layer. The role tokens it consumes are folded under it.
//
// The CSS tab does NOT explain the cascade. It used to, in a paragraph under
// every demo on every page plus an essay for each empty layer — the same
// lesson twenty-eight times down /components. That lesson belongs on the docs
// page that teaches it, once. What belongs here is the artefact: a file you
// can save, and the line you need in the head for it to mean anything.
//
// Why the CSS is read live rather than written beside each demo: a hand-typed
// copy of a component's CSS is a second source of truth, and the day it drifts
// is the day the page starts teaching the wrong thing. Reading it back from
// the stylesheets cannot drift — if it is not on the page, it is not in the
// tab. The limits of that choice are written down at collectLayers().
//
// Why a Datastar attribute rather than a plain script: everything else in this
// workspace arrives by `patch elements`, and a region patched in later has to
// light up on its own. An attribute plugin gets applied by the engine on every
// scan and hands back a cleanup for free; a plain script would need its own
// MutationObserver and its own teardown. It also lets the stage BORROW
// data-resize-pane (plugins/phi.js) instead of re-writing pointer dragging.
//
// No global signals are used, deliberately. A page carries dozens of demos and
// data-resize-pane's expression has `el` in scope, so the drag writes straight
// to the stage's own style — nothing to collide over. (data-resize from
// plugins/resize.js is the other half of the same story and is NOT used here:
// it mergePatches the fixed names $resize_width / $resize_current_breakpoint,
// so two demos on one page would fight over one readout. The readout below is
// a per-instance ResizeObserver reporting THIS system's breakpoints — 720 /
// 900 / 1100 — rather than resize.js's generic breakpoint ladder.)
import { attribute } from "datastar";
// The block of code, its copy control and the syntax colouring are shared
// with data-playground, which prints two of them — see plugins/code-block.js
// for why they are a module rather than a second copy of these functions.
import { elem, dedent, codeBlock, paint, copyText, liveRegion } from "./code-block.js";

// ── small helpers ───────────────────────────────────────────────────────────

let seq = 0;

// Split a comma list that may contain :is(…), [attr=","] or a quoted string.
function splitTop(str, sep) {
	const out = [];
	let depth = 0, quote = null, cur = "";
	for (const ch of str) {
		if (quote) { cur += ch; if (ch === quote) quote = null; continue; }
		if (ch === '"' || ch === "'") { quote = ch; cur += ch; continue; }
		if (ch === "(" || ch === "[") depth++;
		else if (ch === ")" || ch === "]") depth--;
		else if (ch === sep && depth === 0) { if (cur.trim()) out.push(cur.trim()); cur = ""; continue; }
		cur += ch;
	}
	if (cur.trim()) out.push(cur.trim());
	return out;
}

// A rule the reader can paste: one declaration per line, one selector per line.
function formatRule(cssText) {
	const open = cssText.indexOf("{");
	const close = cssText.lastIndexOf("}");
	if (open < 0 || close < 0) return cssText;
	const sel = splitTop(cssText.slice(0, open), ",").join(",\n");
	const decls = splitTop(cssText.slice(open + 1, close), ";");
	if (!decls.length) return `${sel} {}`;
	return `${sel} {\n${decls.map((d) => `\t${d};`).join("\n")}\n}`;
}

function indent(block, by) {
	return block.split("\n").map((l) => (l ? by + l : l)).join("\n");
}

// ── which rules touch this specimen ─────────────────────────────────────────

// The state parts of a selector cannot be tested with matches(): nothing is
// hovered while we look. They are stripped for the TEST and kept for DISPLAY,
// so `.btn:hover` is reported whenever a `.btn` is present.
const STATE = new RegExp(
	"::?(?:hover|focus-visible|focus-within|focus|active|visited|target|disabled|enabled|" +
	"checked|indeterminate|placeholder-shown|user-invalid|user-valid|invalid|valid|default|" +
	"read-only|read-write|autofill|open|before|after|placeholder|selection|marker|backdrop|" +
	"file-selector-button|first-line|first-letter|details-content|-webkit-[a-z-]+|-moz-[a-z-]+)" +
	"(\\([^()]*\\))?",
	"g",
);
// Rules that describe the document, not the component. They are real and they
// matter — they are where the tokens live — but dumping tokens.css into the
// "base" section would bury the three type roles the reader came for. The
// tokens get their own table instead, resolved.
const DOCUMENT_ONLY = /^(?:\*|html|body|:root|:where\(html\)|:where\(\*\)|::selection|::backdrop|::-webkit-[a-z-]+)$/;

function testable(sel) {
	const t = sel.replace(STATE, "").replace(/\s+/g, " ").trim();
	if (!t || /[>+~]$/.test(t) || DOCUMENT_ONLY.test(t)) return null;
	return t;
}

// How far into the specimen does this rule reach? A selector part that finds
// something INSIDE the specimen paints the component. A part that matches only
// the container the specimen sits in — #main, .reading — is the page AROUND
// it: the reason it is that size and that typeface, but not the component.
//
// The line is drawn here rather than left to the reader because it was
// measured. On /components the container's rules were 38% / 21% / 39% of the
// block by character, and being the same page wrapper every time, the SAME
// 1061 characters under every specimen on the page — twenty token
// declarations of .reading pasted into a file whose subject is a button. They
// are named in the layer comment instead, which is what a reader needs to go
// and look, without carrying the page's layout into their own stylesheet.
function reach(root, selectorText) {
	let inner = false;
	const outer = [];
	for (const part of splitTop(selectorText, ",")) {
		const t = testable(part);
		if (!t) continue;
		try {
			if (root.querySelector(t)) { inner = true; continue; }
			if (root.matches(t)) outer.push(part.trim());
		} catch { /* a selector querySelector will not take — skip it */ }
	}
	return { inner, outer };
}

const ORDER = ["base", "themes", "patterns", "components", "utilities", "pages"];

// Walk the preview document's stylesheets and keep every style rule that
// matches something in the specimen, tagged with the @layer it arrived in and
// the @media / @supports / @container it sits inside.
//
// KNOWN LIMITS, so nobody reads more into the tab than is there:
//   · it reports rules that MATCH, not rules that WIN — a declaration later
//     overridden is still shown, which is the point when the lesson is that a
//     utility beats a component by layer order;
//   · a state selector (:hover, ::before) is kept whenever its base selector
//     is present, because state cannot be tested at rest;
//   · a @media block is listed whether or not it is currently in effect —
//     that IS the reason the preview is resizable;
//   · rules naming only html / body / :root are held back and surfaced as the
//     resolved role-token table instead;
//   · a cross-origin stylesheet would throw on .cssRules and is skipped. All
//     of ours are same-origin, so today nothing is.
//
// Each kept rule carries the FILE it was authored in, which is what lets the
// one comment line per layer name real files instead of describing a layer in
// the abstract. Each top-level <link> that contributed one is remembered too:
// that set is the "what to load" half of the tab, derived rather than typed,
// so it cannot go stale the way a hand-written instruction would.
function collectLayers(win, root) {
	const found = new Map();
	const links = new Set();
	const seen = new Set();

	const keep = (layer, conds, rule, file, link, hit) => {
		const key = `${layer}|${conds.join("|")}|${rule.cssText}`;
		if (seen.has(key)) return;
		seen.add(key);
		const name = layer || "(no layer)";
		if (!found.has(name)) found.set(name, []);
		found.get(name).push({ conds, rule, file, inner: hit.inner, outer: hit.outer });
		if (link) links.add(link);
	};

	const walk = (rules, layer, conds, file, link) => {
		for (const rule of rules) {
			if (win.CSSImportRule && rule instanceof win.CSSImportRule) {
				let sheet = null;
				try { sheet = rule.styleSheet; } catch { /* cross-origin */ }
				if (!sheet) continue;
				const inner = rule.layerName ? rule.layerName : layer;
				// the imported file becomes the authoring file; the <link> that
				// pulled it in stays what the reader has to load
				try { walk(sheet.cssRules, inner, conds, sheet.href || file, link); } catch { /* cross-origin */ }
			} else if (win.CSSLayerBlockRule && rule instanceof win.CSSLayerBlockRule) {
				walk(rule.cssRules, rule.name || layer, conds, file, link);
			} else if (win.CSSMediaRule && rule instanceof win.CSSMediaRule) {
				walk(rule.cssRules, layer, conds.concat(`@media ${rule.conditionText}`), file, link);
			} else if (win.CSSContainerRule && rule instanceof win.CSSContainerRule) {
				walk(rule.cssRules, layer, conds.concat(`@container ${rule.conditionText}`), file, link);
			} else if (win.CSSSupportsRule && rule instanceof win.CSSSupportsRule) {
				walk(rule.cssRules, layer, conds.concat(`@supports ${rule.conditionText}`), file, link);
			} else if (win.CSSStyleRule && rule instanceof win.CSSStyleRule) {
				const hit = reach(root, rule.selectorText);
				if (hit.inner || hit.outer.length) keep(layer, conds, rule, file, link, hit);
			}
		}
	};

	const sheets = [...win.document.styleSheets].filter(
		// the two lines the stage itself injects are not part of the system
		(sheet) => !(sheet.ownerNode && sheet.ownerNode.hasAttribute?.("data-demo-frame")),
	);
	for (const sheet of sheets) {
		const href = sheet.href || "";
		try { walk(sheet.cssRules, sheet.ownerNode?.getAttribute?.("data-layer") || "", [], href, href); } catch { /* cross-origin */ }
	}
	// A sheet that only re-points role tokens matches NOTHING — every selector
	// it carries is html / body / :root, which testable() holds back — and yet
	// leaving it out of "what to load" would hand the reader a file whose
	// var(--primary) resolves to nothing. So a sheet that sets a role on the
	// document is listed whether or not one of its rules matched.
	for (const sheet of sheets) {
		const href = sheet.href || "";
		if (!href || links.has(href)) continue;
		if (declaresTokens(sheet)) links.add(href);
	}
	return { found, links: [...links] };
}

// Is this sheet a TOKEN sheet — does it set a custom property on the document
// itself? That is the test, not "sets a custom property anywhere": a component
// sheet setting its own --split-handle-size is not something the reader has to
// load, and showcase.css does exactly that. Bounded, because it is only asked
// of sheets nothing matched and one of those could in principle be large.
const DOC_SUBJECT = /^(?::root|html|body)(?![\w-])/;
function documentLevel(selectorText) {
	for (const part of splitTop(selectorText, ",")) {
		const t = part.trim().replace(/^:where\(\s*/, "").replace(/\s*\)$/, "");
		if (DOC_SUBJECT.test(t)) return true;
	}
	return false;
}
function declaresTokens(sheet) {
	let budget = 3000;
	const scan = (rules) => {
		for (const rule of rules) {
			if (budget-- <= 0) return false;
			if (rule.style && rule.selectorText && documentLevel(rule.selectorText)) {
				for (const prop of rule.style) if (prop.startsWith("--")) return true;
			}
			let inner = null;
			try { inner = rule.cssRules || rule.styleSheet?.cssRules || null; } catch { /* cross-origin */ }
			if (inner && scan(inner)) return true;
		}
		return false;
	};
	try { return scan(sheet.cssRules); } catch { return false; }
}

// What each layer contributes, in one line — the ONLY teaching the tab does,
// and it travels inside the file the reader copies rather than sitting on the
// page. `in` is used when the layer has rules, `out` when it has none: an
// empty layer is a finding worth one line, not a paragraph.
//
// These are SHORT on purpose, and the length was measured rather than guessed.
// A <pre> at 400px fits about 26 characters, so a 94-character note wraps to
// four lines — six of those and the reader scrolls past a screen of comment
// before reaching a declaration. Everything below fits in two.
const LAYER_NOTE = {
	base:       { in: "inherited defaults", out: "nothing" },
	themes:     { in: "role tokens", out: "nothing; a theme re-points roles" },
	patterns:   { in: "shared arrangement", out: "nothing" },
	components: { in: "the component itself", out: "nothing" },
	utilities:  { in: "one declaration, later layer", out: "nothing" },
	pages:      { in: "one page's arrangement", out: "nothing" },
};

// `/static/ui/design/components/kv.css?v=2` → `kv.css`. The directory is dropped
// because the @layer above the comment already IS the directory — carrying
// `components/` in front of every name costs eleven characters a file at the
// width where characters are scarcest, and tells the reader nothing the line
// they are reading has not just said.
function shortFile(href) {
	if (!href) return "";
	let path = String(href);
	try { path = new URL(href, "http://x/").pathname; } catch { path = path.split("?")[0]; }
	return path.split("/").filter(Boolean).pop() || "";
}

// `http://host/static/ui/design/index.css?v=2` → `/static/ui/design/index.css`
// The cache-buster is ours, not the reader's; pasting it would pin them to a
// version of a file they do not own.
function barePath(href) {
	try { return new URL(href).pathname; } catch { return String(href).split("?")[0]; }
}

// Turn one layer's rules back into CSS, nesting the conditions the browser
// told us they were under.
function layerBody(entries) {
	const groups = [];
	for (const { conds, rule } of entries) {
		const key = conds.join(" ");
		const last = groups[groups.length - 1];
		if (last && last.key === key) last.rules.push(rule);
		else groups.push({ key, conds, rules: [rule] });
	}
	return groups.map(({ conds, rules }) => {
		let block = rules.map((r) => formatRule(r.cssText)).join("\n\n");
		for (let i = conds.length - 1; i >= 0; i--) block = `${conds[i]} {\n${indent(block, "\t")}\n}`;
		return block;
	}).join("\n\n");
}

// ONE file: the rules that PAINT the specimen, the @layer blocks separated
// inside it, cascade order preserved. A rule that only matches the container
// around the specimen is named on its own comment line rather than copied —
// see reach() for the measurement that settled it. An empty layer keeps its
// place as a single comment so the order still reads top to bottom.
function fileText(order, found, label) {
	const parts = [
		`/* ${label} — the rules on this page that paint this specimen, in`,
		`   cascade order. Keep the @layer wrappers: unlayered, these rules`,
		`   would outrank the system that owns them. */`,
	];
	for (const name of order) {
		const entries = found.get(name) || [];
		const note = LAYER_NOTE[name];
		const inside = entries.filter((e) => e.inner);
		// the page around it: one line, deduped, selector and the file it is in
		const around = [];
		for (const e of entries) {
			if (e.inner) continue;
			for (const sel of e.outer) {
				const tag = `${sel} (${shortFile(e.file)})`;
				if (!around.includes(tag)) around.push(tag);
			}
		}
		const aroundLine = around.length ? `/* around it: ${around.join(", ")} */` : "";
		if (!inside.length) {
			parts.push(around.length
				? `\n/* @layer ${name} — around it only: ${around.join(", ")} */`
				: `\n/* @layer ${name} — ${note ? note.out : "nothing"} */`);
			continue;
		}
		const files = [...new Set(inside.map((e) => shortFile(e.file)).filter(Boolean))].join(", ");
		const body = layerBody(inside);
		if (name === "(no layer)") {
			parts.push(`\n/* outside every layer${files ? ` — ${files}` : ""} */\n${body}`);
			continue;
		}
		const head = [note ? note.in : name, files].filter(Boolean).join(" — ");
		const comments = [`/* ${head} */`].concat(aroundLine ? [aroundLine] : []).join("\n");
		parts.push(`\n@layer ${name} {\n${indent(comments, "\t")}\n\n${indent(body, "\t")}\n}`);
	}
	return parts.join("\n");
}

// Every role token the kept rules ask for, resolved against the specimen under
// the theme that is on screen. This is the other half of "from different
// levels": the rules say var(--primary), the table says what --primary IS here.
function rolesUsed(win, root, found) {
	const names = new Set();
	for (const entries of found.values()) {
		for (const { rule } of entries) {
			for (const m of rule.cssText.matchAll(/var\(\s*(--[A-Za-z0-9_-]+)/g)) names.add(m[1]);
		}
	}
	const cs = win.getComputedStyle(root);
	return [...names].sort().map((n) => ({ name: n, value: cs.getPropertyValue(n).trim() }));
}

// ── the breakpoints this system actually has ────────────────────────────────
// 720 / 900 / 1100 are the three widths design/ writes @media against. Naming
// them after the ladder some other framework uses would be a lie in a tab
// whose whole job is to tell the truth about the CSS.
function band(w) {
	if (w <= 720) return "phone";
	if (w <= 900) return "small";
	if (w <= 1100) return "tablet";
	return "desktop";
}

const PRESETS = [
	{ label: "Phone", w: 380 },
	{ label: "Tablet", w: 820 },
	{ label: "Desktop", w: 1280 },
	{ label: "Fill", w: null },
];
const MIN = 240;
const MAX = 1600;

// ── the attribute ───────────────────────────────────────────────────────────

attribute({
	name: "demo",
	requirement: { key: "denied", value: "allowed" },
	returnsValue: true,
	argNames: [],
	apply({ el, rx }) {
		const source = el.querySelector(":scope > .demo-source");
		if (!source) {
			console.warn("[demo] data-demo needs a .demo-source child holding the markup", el);
			return;
		}
		// The page's regions arrive over SSE and `patch elements -s #main -m
		// inner` morphs this subtree back to the server's copy, which would take
		// the built DOM with it — and idiomorph REUSES nodes, so holding
		// references and putting them back is not safe. data-ignore-morph is the
		// engine's own hook for exactly this and it has to be on BOTH sides of
		// the morph, which means it has to be in the authored markup. Say so
		// rather than letting the demo silently fall back to flat markup.
		if (!el.hasAttribute("data-ignore-morph")) {
			console.warn("[demo] this demo has no data-ignore-morph — the next patch to its page will morph it back to plain markup", el);
		}
		const cfg = (() => { try { return rx() ?? {}; } catch { return {}; } })();
		// every demo on a page needs its own ids for aria-controls /
		// aria-labelledby; the counter is unconditional so two demos cannot
		// collide even if one of them is itself called "demo-1"
		const uid = `${el.id || "demo"}-${++seq}`;
		const markup = dedent(source.innerHTML);
		const cleanups = [];

		// ── the bar ────────────────────────────────────────────────────────
		const bar = elem("div", "demo-bar");
		const strip = elem("div", "tabbar");
		strip.setAttribute("role", "tablist");
		strip.setAttribute("aria-label", cfg.label ? `${cfg.label} — views` : "Demo views");

		const TABS = [
			{ key: "preview", label: "Preview", n: "1" },
			{ key: "html", label: "HTML", n: "2" },
			{ key: "css", label: "CSS", n: "3" },
		];
		const tabs = [];
		const panes = [];
		for (const [i, t] of TABS.entries()) {
			const b = elem("button", "tabbtn");
			b.type = "button";
			b.id = `${uid}-tab-${t.key}`;
			b.setAttribute("role", "tab");
			b.setAttribute("aria-controls", `${uid}-panel-${t.key}`);
			b.setAttribute("aria-selected", i === 0 ? "true" : "false");
			b.tabIndex = i === 0 ? 0 : -1;   // one tab stop for the whole strip
			b.append(elem("span", "num", t.n), document.createTextNode(t.label));
			strip.appendChild(b);
			tabs.push(b);

			const p = elem("div", `demo-pane ${t.key}`);
			p.id = `${uid}-panel-${t.key}`;
			p.setAttribute("role", "tabpanel");
			p.setAttribute("aria-labelledby", b.id);
			p.tabIndex = 0;                  // a scrollable panel must be reachable
			if (i !== 0) p.hidden = true;
			panes.push(p);
		}

		const tools = elem("div", "demo-tools");
		const read = elem("span", "chip demo-read");
		read.append(elem("b", null, "—"), elem("em", null, ""));
		const presets = elem("div", "actions");
		presets.setAttribute("role", "group");
		presets.setAttribute("aria-label", "Preview width");
		const presetBtns = PRESETS.map((p) => {
			const b = elem("button", "btn sm", p.label);
			b.type = "button";
			b.setAttribute("aria-pressed", p.w === null ? "true" : "false");
			b.addEventListener("click", () => setWidth(p.w));
			presets.appendChild(b);
			return b;
		});
		tools.append(read, presets);
		bar.append(strip, tools);

		// ── the stage ──────────────────────────────────────────────────────
		const stage = elem("div", "demo-stage");
		const frame = elem("iframe", "demo-frame pending");
		frame.title = cfg.label ? `${cfg.label} — live preview` : "Live preview";
		frame.setAttribute("scrolling", "no");

		// THE PLACEHOLDER, and why it is this and not a number.
		// An unmounted stage that is a fixed empty box reflows the moment its
		// document arrives — measured at +107px for the kv demo and −88px for
		// the button one — and on a page of twenty-eight demos that is the text
		// jumping under the reader's eyes all the way down. A configured height
		// would only move the problem: it is a number an author has to guess
		// and then keep in step with the markup.
		//
		// So the placeholder is the specimen itself. The authored markup is
		// already in the page (it is the no-script baseline); it moves into the
		// stage and holds it open at the height the same markup, under the same
		// stylesheets, at the same width, actually takes. The frame loads over
		// it, absolutely positioned so it contributes no height of its own, and
		// they swap on load — measured jump ≈ 0. The ghost is not a second copy
		// of anything: it is the one copy, standing in until the nested
		// viewport is ready to hold it.
		const ghost = source;
		ghost.className = "demo-ghost";
		stage.append(ghost, frame);
		panes[0].appendChild(stage);

		// ── the HTML panel ─────────────────────────────────────────────────
		const htmlCap = elem("div", "code-cap");
		htmlCap.append(elem("b", null, "markup"), elem("span", null, "the source this page renders — there is no second copy"));
		const html = codeBlock("xml");
		panes[1].append(htmlCap, html.wrap);
		paint(html.code, markup, "xml");

		const live = liveRegion();
		html.btn.addEventListener("click", () => copyText(markup, html.btn, live, html.pre));

		// ── assemble ───────────────────────────────────────────────────────
		const body = elem("div", "demo-body");
		body.append(...panes);
		el.append(bar, body, live);
		el.classList.add("demo");

		// phi.js/data-resize-pane does the pointer drag, and its expression has
		// `el` in scope — so the width is written straight to the stage's own
		// style. No signal, therefore nothing for the next demo on the page to
		// collide with. (data-resize from plugins/resize.js is the readout's
		// obvious alternative and is why this matters: it mergePatches the fixed
		// names $resize_width / $resize_current_breakpoint, so the 28 demos on
		// this page would all write one number.)
		//
		// The attribute is set a task LATER, and that is not a flourish: the
		// engine is still inside its own application pass while apply() runs, so
		// an attribute added here is not seen. On an element already in the
		// document, one task later, it is.
		const wire = setTimeout(() => {
			stage.setAttribute(
				"data-resize-pane",
				`value == null ? ({axis:'x', min:${MIN}, max:${MAX}}) : (el.style.width = value + 'px')`,
			);
		}, 0);
		cleanups.push(() => clearTimeout(wire));

		// ── tabs ───────────────────────────────────────────────────────────
		let current = 0;
		function select(i, focus) {
			// touching any tab means this demo is being read: bring the stage up
			// now rather than waiting for the observer below to notice
			mount();
			current = i;
			tabs.forEach((b, j) => {
				b.setAttribute("aria-selected", j === i ? "true" : "false");
				b.tabIndex = j === i ? 0 : -1;
				panes[j].hidden = j !== i;
			});
			tools.hidden = i !== 0;
			if (i === 2) buildCss();
			if (focus) tabs[i].focus();
		}
		tabs.forEach((b, i) => b.addEventListener("click", () => select(i, false)));
		strip.addEventListener("keydown", (ev) => {
			const n = tabs.length;
			let next = null;
			if (ev.key === "ArrowRight") next = (current + 1) % n;
			else if (ev.key === "ArrowLeft") next = (current - 1 + n) % n;
			else if (ev.key === "Home") next = 0;
			else if (ev.key === "End") next = n - 1;
			if (next === null) return;
			ev.preventDefault();
			select(next, true);
		});

		// ── the preview document ───────────────────────────────────────────
		// It links exactly the stylesheets the host page links — resolved to
		// absolute URLs, so nothing depends on how about:srcdoc inherits a base
		// — and reproduces the two wrappers a patched region really lands in,
		// .canvas-scroll > #main[data-stage]. Anything less and the CSS tab would be
		// reporting a cascade the reader is not looking at.
		const sheets = [...document.querySelectorAll('link[rel="stylesheet"]')]
			.map((l) => l.href)
			.filter(Boolean);
		// The tray: #main[data-stage] on a Stage shell, #canvas on an older one.
		const canvas = document.querySelector("#main[data-stage]") || document.getElementById("canvas");
		const canvasClass = canvas ? canvas.className : "";
		const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;").replace(/"/g, "&quot;");
		const srcdoc =
			`<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">` +
			`<meta name="viewport" content="width=device-width, initial-scale=1">` +
			sheets.map((h) => `<link rel="stylesheet" href="${esc(h)}">`).join("") +
			// The only rules the stage adds, and the reason for each one.
			// min-height:0 undoes base/tokens.css's `body { min-height: 100vh }`
			// — inside an iframe 100vh IS the frame's current height, so leaving
			// it in makes the stage a ratchet: it grows to fit its content and
			// can never shrink again, and a demo dragged from phone back to
			// desktop would keep the taller layout's height forever.
			// .canvas-scroll gives up its own scrolling because the stage is
			// sized to its content and must never scroll internally.
			// They are tagged so collectLayers() skips them: a rule this file
			// invented must never appear in a tab that reports the system.
			`<style data-demo-frame>html,body{margin:0;min-height:0;background:transparent}` +
			`.canvas-scroll{overflow:visible;padding:16px}</style>` +
			`</head><body><div class="canvas-scroll"><div id="main" data-stage class="${esc(canvasClass)}">` +
			markup +
			`</div></div></body></html>`;

		// ── theme, both axes ───────────────────────────────────────────────
		// personality is data-theme on <html>, mode is a class on <body>, and
		// the preview has to follow both or the code skin would look correct
		// while the specimen beside it did not.
		function syncTheme() {
			const doc = frame.contentDocument;
			if (!doc?.documentElement) return;
			const name = document.documentElement.getAttribute("data-theme");
			if (name) doc.documentElement.setAttribute("data-theme", name);
			else doc.documentElement.removeAttribute("data-theme");
			doc.body.classList.toggle("theme-dark", document.body.classList.contains("theme-dark"));
			doc.body.classList.toggle("theme-light", document.body.classList.contains("theme-light"));
			if (rolesBody) fillRoles();
		}
		const themeWatch = new MutationObserver(syncTheme);
		themeWatch.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
		themeWatch.observe(document.body, { attributes: true, attributeFilter: ["class"] });
		cleanups.push(() => themeWatch.disconnect());

		// ── height: the stage is as tall as what is in it ──────────────────
		let innerWatch = null;
		frame.addEventListener("load", () => {
			const doc = frame.contentDocument;
			const win = frame.contentWindow;
			if (!doc || !win) return;
			syncTheme();
			// Measure the BODY, not documentElement: <html> fills the iframe, so
			// its scrollHeight can never fall below the frame's current height —
			// which would make the stage a one-way ratchet that grows and never
			// shrinks. The body has no height of its own, so its box is exactly
			// the content's.
			const fit = () => {
				const b = doc.body;
				if (!b) return;
				const h = Math.ceil(b.getBoundingClientRect().height) || b.scrollHeight;
				frame.style.height = `${Math.max(h, 40)}px`;
			};
			fit();
			// hand the stage over: the frame is sized, so it can leave its
			// absolute position and take the height the ghost was holding
			frame.classList.remove("pending");
			ghost.remove();
			fit();
			if (win.ResizeObserver) {
				innerWatch = new win.ResizeObserver(fit);
				innerWatch.observe(doc.documentElement);
				if (doc.body) innerWatch.observe(doc.body);
			}
			// the faces land after the sheet does, and they change the height
			doc.fonts?.ready?.then(fit).catch(() => {});
			cssStale = true;
			// a reader who opened the CSS tab before the stage finished loading
			// would otherwise be looking at an empty panel: it is built from the
			// frame's stylesheets, so this is the first moment it can be built
			if (current === 2) buildCss();
		});
		cleanups.push(() => innerWatch?.disconnect());

		// ── the stage is built when it is nearly on screen ─────────────────
		// One demo is one whole extra document: its own copy of the design
		// system, its own font decode, its own layout. /components is
		// twenty-eight specimens long, so mounting them all at first paint
		// would be twenty-eight documents to open a page nobody has scrolled
		// yet. The observer's 400px margin means a demo is loaded and laid out
		// well before it reaches the viewport — and one already in view mounts
		// on the very next frame, so nothing pops on load.
		//
		// Everything the stage needs is already set up and waits for it: the
		// theme observer is running (syncTheme no-ops until there is a document
		// and the load handler calls it), the pane already has its width from
		// cfg.w, and the seam is wired by data-resize-pane against the STAGE,
		// not the frame, so it drags whether or not the document has arrived.
		let mounted = false;
		function mount() {
			if (mounted) return;
			mounted = true;
			frame.srcdoc = srcdoc;
		}
		const near = new IntersectionObserver((entries) => {
			if (!entries.some((e) => e.isIntersecting)) return;
			near.disconnect();
			mount();
		}, { rootMargin: "400px 0px" });
		near.observe(el);
		cleanups.push(() => near.disconnect());

		// ── width: the readout, the presets, the seam ──────────────────────
		let seam = null;
		function setWidth(w) {
			stage.style.width = w === null ? "" : `${Math.min(MAX, Math.max(MIN, w))}px`;
			presetBtns.forEach((b, i) => b.setAttribute("aria-pressed", PRESETS[i].w === w ? "true" : "false"));
		}
		if (Number.isFinite(cfg.w)) setWidth(cfg.w);

		const sizeWatch = new ResizeObserver(([entry]) => {
			const w = Math.round(entry.contentRect.width);
			read.firstChild.textContent = `${w}px`;
			read.lastChild.textContent = ` · ${band(w)}`;
			if (seam) seam.setAttribute("aria-valuenow", String(w));
		});
		sizeWatch.observe(frame);
		cleanups.push(() => sizeWatch.disconnect());

		// data-resize-pane appends its seam during ITS apply, which the engine
		// runs once this stage is in the DOM — so it is not here yet. Watch for
		// it, then hand it a keyboard, a name and a look. phi.js writes the
		// seam's width and opacity inline (inline beats a stylesheet), so those
		// two are cleared and re-asserted after its own hover handlers, which
		// is why the listeners below are added rather than replaced.
		const seamWatch = new MutationObserver(() => {
			const s = stage.querySelector("[data-resize-pane-seam]");
			if (!s || s === seam) return;
			seam = s;
			seamWatch.disconnect();
			s.classList.add("demo-seam");
			s.style.width = "";
			s.style.background = "";
			s.style.opacity = "";
			const hold = () => { s.style.opacity = ""; };
			s.addEventListener("pointerenter", hold);
			s.addEventListener("pointerleave", hold);
			s.addEventListener("pointerup", hold);
			s.tabIndex = 0;
			s.setAttribute("role", "separator");
			s.setAttribute("aria-orientation", "vertical");
			s.setAttribute("aria-label", "Preview width — left and right arrows resize");
			s.setAttribute("aria-valuemin", String(MIN));
			s.setAttribute("aria-valuemax", String(MAX));
			s.setAttribute("aria-valuenow", String(Math.round(stage.getBoundingClientRect().width)));
			s.addEventListener("keydown", (ev) => {
				const step = ev.shiftKey ? 64 : 16;
				const now = Math.round(stage.getBoundingClientRect().width);
				let next = null;
				if (ev.key === "ArrowLeft") next = now - step;
				else if (ev.key === "ArrowRight") next = now + step;
				else if (ev.key === "Home") next = MIN;
				else if (ev.key === "End") next = MAX;
				if (next === null) return;
				ev.preventDefault();
				setWidth(next);
			});
		});
		seamWatch.observe(stage, { childList: true });
		cleanups.push(() => seamWatch.disconnect());

		// ── the CSS panel, built on first look ─────────────────────────────
		let cssStale = true;
		let rolesBody = null;
		let rolesRoot = null;

		function fillRoles() {
			const win = frame.contentWindow;
			if (!win || !rolesRoot || !rolesBody) return;
			const cs = win.getComputedStyle(rolesRoot);
			for (const tr of rolesBody.rows) {
				const name = tr.dataset.role;
				tr.cells[1].querySelector(".txt").textContent = cs.getPropertyValue(name).trim() || "—";
			}
		}

		function buildCss() {
			if (!cssStale) return;
			const win = frame.contentWindow;
			const doc = frame.contentDocument;
			const root = doc?.getElementById("main");
			if (!win || !root) return;           // the frame has not loaded yet
			cssStale = false;
			rolesRoot = root;
			panes[2].textContent = "";

			const { found, links } = collectLayers(win, root);

			// ── 1. what to load ────────────────────────────────────────────
			// Derived from the sheets that actually reached the specimen rather
			// than typed here, so it cannot drift. index.css is the whole system
			// behind one href — which is the point of it declaring the layer
			// order at the top and importing everything under it.
			const hrefs = links.map(barePath).filter(Boolean);
			if (!hrefs.length) hrefs.push("/static/ui/design/index.css");
			const linkText = hrefs.map((h) => `<link rel="stylesheet" href="${h}">`).join("\n");
			const loadPart = elem("div", "demo-part");
			const loadCap = elem("div", "code-cap");
			loadCap.append(elem("b", null, "in the head"), elem("span", null, hrefs.length === 1
				? "one stylesheet — it declares the layer order and imports the system"
				: "these stylesheets, in this order"));
			const load = codeBlock("xml");
			load.btn.addEventListener("click", () => copyText(linkText, load.btn, live, load.pre));
			loadPart.append(loadCap, load.wrap);
			panes[2].appendChild(loadPart);
			paint(load.code, linkText, "xml");

			// ── 2. the file ────────────────────────────────────────────────
			const order = [...ORDER];
			for (const name of found.keys()) if (!order.includes(name)) order.push(name);
			const text = fileText(order, found, cfg.label || "this specimen");
			const cssPart = elem("div", "demo-part");
			const cssCap = elem("div", "code-cap");
			cssCap.append(elem("b", null, "one file"), elem("span", null, "every rule on this page that reaches the specimen, layers separated inside"));
			const blk = codeBlock("css");
			blk.btn.addEventListener("click", () => copyText(text, blk.btn, live, blk.pre));
			cssPart.append(cssCap, blk.wrap);
			panes[2].appendChild(cssPart);
			paint(blk.code, text, "css");

			// ── 3. the roles, folded away ──────────────────────────────────
			// The rules above say var(--primary); this says what --primary IS
			// under the theme on screen, and syncTheme re-resolves it when the
			// theme changes. It is a lookup, not a lesson, so it starts closed.
			const roles = rolesUsed(win, root, found);
			if (roles.length) {
				const det = elem("details", "raw demo-roles-fold");
				det.appendChild(elem("summary", null, `The ${roles.length} role tokens it names, resolved here`));
				const wrap = elem("div", "table-wrap demo-roles-wrap");
				const table = elem("table", "demo-roles");
				table.innerHTML = "<thead><tr><th>Role</th><th>Value here</th></tr></thead>";
				const tb = elem("tbody");
				for (const r of roles) {
					const tr = elem("tr");
					tr.dataset.role = r.name;
					const td1 = elem("td");
					td1.appendChild(elem("code", null, r.name));
					const td2 = elem("td");
					const val = elem("span", "val");
					if (CSS.supports("color", r.value)) {
						const sw = elem("span", "swatch");
						sw.style.setProperty("--swatch", `var(${r.name})`);
						val.appendChild(sw);
					}
					val.appendChild(elem("span", "txt", r.value || "—"));
					td2.appendChild(val);
					tr.append(td1, td2);
					tb.appendChild(tr);
				}
				table.appendChild(tb);
				rolesBody = tb;
				wrap.appendChild(table);
				det.appendChild(wrap);
				panes[2].appendChild(det);
			}
		}

		// ── teardown ───────────────────────────────────────────────────────
		return () => {
			for (const fn of cleanups) { try { fn(); } catch { /* going away anyway */ } }
			bar.remove();
			body.remove();
			live.remove();
			const back = elem("div", "demo-source");
			back.innerHTML = markup;
			el.appendChild(back);
		};
	},
});
