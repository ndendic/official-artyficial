// code-block.js — a block of code the reader can take away, and the three
// small things every such block needs: a way to build it, a way to paint it,
// and a copy control that tells the truth when it fails.
//
// It is a MODULE and not a plugin: it registers nothing on the engine and
// owns no attribute. Two plugins need the same block — data-demo prints the
// markup and the CSS that painted a specimen, data-playground prints the
// markup a reader has just built and the overrides they set doing it — and a
// second copy of copyText() is a second place for the clipboard fallback to
// rot. This codebase has spent a rename on exactly that kind of drift; a
// shared file is cheaper than a diff nobody reads.
//
// The block it builds is components/code.css's `.code`: a <pre> with a .btn
// pinned to its top right. Nothing here knows what a colour is.

// ── highlight.js ────────────────────────────────────────────────────────────
// Pinned to 11.9.0 and served by the binary, not a CDN: one ES module (core
// plus xml, css, javascript, json, bash, go and plaintext) that the page's
// import map names "highlight.js" ({{.UI.ImportMap}} maps it to its hashed,
// immutable URL), so with core's import map that file always wins. HL_FALLBACK
// is for a shell whose own import map has no "highlight.js" entry (one
// written before core added it): the import of the bare name then fails, and
// the same file loads from its unhashed path on this server. Neither path is
// a CDN.
// The import is dynamic, so it only happens on a page that has a code block,
// and failure is caught: a missing file costs the colouring, never the
// component. If a host page has already put a build on window.hljs, that one
// is used and nothing is fetched.
const HL_FALLBACK = "/static/vendor/highlight/highlight.min.js";
let hlPromise = null;
export function highlighter() {
	if (globalThis.hljs) return Promise.resolve(globalThis.hljs);
	if (!hlPromise) {
		hlPromise = import("highlight.js")
			.catch(() => import(HL_FALLBACK))
			.then((mod) => {
				const hl = mod.default;
				globalThis.hljs = hl;
				return hl;
			}).catch((err) => {
				console.warn("[code-block] highlight.js unavailable, showing plain text:", err);
				return null;
			});
	}
	return hlPromise;
}

// ── small helpers ───────────────────────────────────────────────────────────

export function elem(tag, cls, text) {
	const n = document.createElement(tag);
	if (cls) n.className = cls;
	if (text != null) n.textContent = text;
	return n;
}

// Strip the page's own indentation off an authored block without reflowing it.
// The author's markup is the truth; all this does is move it to column zero.
export function dedent(src) {
	const lines = src.replace(/\r\n?/g, "\n").split("\n");
	while (lines.length && !lines[0].trim()) lines.shift();
	while (lines.length && !lines[lines.length - 1].trim()) lines.pop();
	let pad = Infinity;
	for (const l of lines) {
		if (!l.trim()) continue;
		pad = Math.min(pad, l.match(/^[ \t]*/)[0].length);
	}
	if (!Number.isFinite(pad)) pad = 0;
	return lines.map((l) => l.slice(pad)).join("\n");
}

// ── clipboard ───────────────────────────────────────────────────────────────

// navigator.clipboard is undefined on an insecure origin and rejects when the
// permission is denied, so there are three outcomes and the reader is told
// which one happened. Silence would be the one unacceptable answer.
export async function copyText(text, btn, live, fallbackNode) {
	const label = btn.dataset.label;
	const say = (msg, note, failed) => {
		btn.textContent = msg;
		btn.classList.toggle("failed", !!failed);
		live.textContent = note;
		clearTimeout(btn._t);
		btn._t = setTimeout(() => {
			btn.textContent = label;
			btn.classList.remove("failed");
			live.textContent = "";
		}, 2400);
	};
	try {
		if (!navigator.clipboard?.writeText) throw new Error("no clipboard api");
		await navigator.clipboard.writeText(text);
		say("Copied", "Copied to the clipboard.");
		return;
	} catch { /* fall through to the old way */ }
	try {
		const ta = elem("textarea");
		ta.value = text;
		ta.setAttribute("readonly", "");
		ta.setAttribute("aria-hidden", "true");
		Object.assign(ta.style, { position: "fixed", top: "0", left: "-9999px" });
		document.body.appendChild(ta);
		ta.select();
		const ok = document.execCommand("copy");
		ta.remove();
		if (ok) { say("Copied", "Copied to the clipboard."); return; }
	} catch { /* fall through to telling the truth */ }
	// Nothing worked: select the text so the reader's own Ctrl+C can finish
	// the job, and say so out loud.
	try {
		const range = document.createRange();
		range.selectNodeContents(fallbackNode);
		const sel = window.getSelection();
		sel.removeAllRanges();
		sel.addRange(range);
	} catch { /* selection is a courtesy, not a requirement */ }
	say("Copy failed", "The clipboard is not available. The code is selected — press Ctrl+C or Cmd+C.", true);
}

// ── a code block with its copy control ──────────────────────────────────────

export function codeBlock(lang) {
	const wrap = elem("div", "code");
	const btn = elem("button", "btn sm code-copy", "Copy");
	btn.type = "button";
	btn.dataset.label = "Copy";
	const pre = elem("pre");
	const code = elem("code", `language-${lang}`);
	pre.appendChild(code);
	wrap.append(btn, pre);
	return { wrap, btn, pre, code };
}

// The plain text lands SYNCHRONOUSLY and the colour arrives when the grammar
// does, so a block is readable before the CDN answers and correct if it never
// does. `_want` is the guard a live readout needs: a playground knob can
// repaint this block forty times while a slider is dragged, and a highlight
// that resolves after its text has been replaced must not paint the old one
// back. `_painted` is the other half — the same text is never coloured twice.
export function paint(code, text, lang) {
	code.textContent = text;
	code._want = text;
	highlighter().then((hl) => {
		if (!hl || !code.isConnected || code._want !== text || code._painted === text) return;
		try {
			code.innerHTML = hl.highlight(text, { language: lang, ignoreIllegals: true }).value;
			code.classList.add("hljs");
			code._painted = text;
		} catch (err) {
			console.warn("[code-block] highlight failed, text left plain:", err);
		}
	});
}

// The one live region a component needs so the copy control can say what
// happened without a popup. It is `.sr-only` (utilities/utilities.css) and
// belongs to the component, appended wherever the component ends.
export function liveRegion() {
	const live = elem("span", "sr-only");
	live.setAttribute("role", "status");
	live.setAttribute("aria-live", "polite");
	return live;
}
