// code.js — data-code: a copyable, highlighted code block from AUTHORED markup.
//
//   <div class="rt-code" data-ignore-morph data-code="'html'">
//     <template>
//       <rt-tabs value="a">…</rt-tabs>
//     </template>
//   </div>
//
// It is the thin attribute over code-block.js (the module data-spec, data-demo
// and data-playground already share): the text is the <template>'s innerHTML,
// dedented. A template's content is INERT — no custom element upgrades inside
// it and Datastar never applies it — so the block shows exactly what the
// author wrote, not what a component made of it after upgrade. That is the
// difference from data-spec, which serialises the live node and is right for
// CSS-only specimens.
//
// data-ignore-morph is required on the element (both sides of a patch): the
// block is built DOM and a patch of the page would otherwise morph it back to
// the bare template.
//
// The value is the language ('html' by default, or 'css').
import { attribute } from "datastar";
import { dedent, codeBlock, paint, copyText, liveRegion } from "./code-block.js";

attribute({
	name: "code",
	requirement: { key: "denied", value: "allowed" },
	returnsValue: true,
	argNames: [],
	apply({ el, rx }) {
		if (el.querySelector(":scope > .code")) return; // already built (re-apply)
		const tpl = el.querySelector(":scope > template");
		if (!tpl) {
			console.warn("[code] data-code needs a <template> child holding the markup", el);
			return;
		}
		let lang = "xml";
		try {
			const v = rx();
			if (v === "css") lang = "css";
		} catch { /* default: html */ }
		// innerHTML re-escapes what the author wrote (" in attributes, < > & in
		// text); show the markup as authored. paint() sets textContent or
		// highlight.js output, both of which escape again for display.
		// An attribute holding a " was authored single-quoted
		// (data-signals='{"a":1}'); the serializer rewrote it as ="{&quot;a…",
		// so it gets its single quotes back.
		const text = dedent(tpl.innerHTML).replace(/=""/g, "")
			.replace(/="([^"]*&quot;[^"]*)"/g, (m, v) => (v.includes("'") ? m : `='${v.replace(/&quot;/g, '"')}'`))
			.replace(/&quot;/g, '"').replace(/&gt;/g, ">").replace(/&lt;/g, "<").replace(/&nbsp;/g, "\u00a0").replace(/&amp;/g, "&");
		const { wrap, btn, code } = codeBlock(lang);
		const live = liveRegion();
		paint(code, text, lang);
		btn.addEventListener("click", () => copyText(text, btn, live, code));
		el.append(wrap, live);
		return () => {
			wrap.remove();
			live.remove();
		};
	},
});
