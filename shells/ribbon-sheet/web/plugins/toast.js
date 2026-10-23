// toast.js — data-toast: the stack that raises, times and retires a toast.
//
//   <output id="toast-stack" class="toast-stack" role="status" aria-live="polite"
//           popover="manual" data-ignore-morph data-toast></output>
//   <output ... data-toast='{"cap": 3}'></output>
//
//   window.toast("Saved")
//   window.toast({ title: "Base deleted", line: "Undo is not available.", severity: "err" })
//   document.dispatchEvent(new CustomEvent("toast", { detail: { title: "Saved" } }))
//
// components/toast.css owns every pixel; this file owns the three things a
// stylesheet cannot: WHEN a toast leaves, WHERE focus goes when one is
// dismissed, and WHAT the live region is allowed to announce.
//
// ── WHY THERE ARE NO SIGNALS IN THIS FILE ───────────────────────────────────
//
// A toast is an EVENT, not a state. Driving one from a signal means the signal
// has to be cleared again afterwards, and every "raise the same toast twice"
// then needs a nonce to look like a change. Worse, a `data-attr` scanned before
// the `data-bind` that would have created its signal reads "" and writes a
// PRESENT attribute — the trap playground.js records. So the API is a function
// and a DOM event. A server raises one by patching a `data-on-load` or by
// executing a script; neither needs a signal to exist first.
//
// ── ANNOUNCEMENT, ONCE ──────────────────────────────────────────────────────
//
// The stack is a live region that is in the document before anything is put in
// it — a region created and filled in the same frame announces nothing at all.
// A toast node is built COMPLETE and appended ONCE: every later mutation of a
// live region is another announcement, and this file never touches a toast's
// text after it is in. Dismissal removes the node, which announces nothing.
//
// ── ONCE, AND AT THE RIGHT VOLUME ───────────────────────────────────────────
//
// The template carries role="alert", and this file STRIPS IT from every toast
// that is not an error. Two reasons, and the second is the one that matters.
//
//   1. role="alert" is aria-live="assertive" by definition. Nesting an
//      assertive region inside the stack's polite one asks two live regions to
//      describe the same insertion, and the engines do not agree on who wins:
//      the observed failure is the message read twice.
//   2. Assertive INTERRUPTS. It cuts off whatever the screen reader is saying
//      mid-word. That is correct for "Delete failed — nothing was removed" and
//      is a hostile act for "Saved", which is most of the toasts a session
//      raises. Routine confirmation does not get to interrupt.
//
// So: `.err` keeps role="alert"; .ok / .warn / .info are announced by the
// stack's own polite role="status", in their turn. `{ assertive: true }`
// overrides it either way for a caller who knows better.
//
// ── DWELL IS NOT MOTION ─────────────────────────────────────────────────────
//
// The dwell is read from --toast-dwell on the stack, which is a token OUTSIDE
// the --dur-* family on purpose: themes/reduced-motion.css collapses the motion
// tokens to zero, and a dwell in that family would retire the toast before it
// could be read for exactly the users who asked for less motion. If the token
// is absent the fallback below is used; it is never read from a --dur-*.

import { attribute } from "datastar";

const DWELL_FALLBACK_MS = 5000;
const CAP_DEFAULT = 5;
// The longest we will wait for the exit transition to report back before
// removing the node anyway. A `transitionend` that never fires (the element was
// display:none'd by a patch, the tab was backgrounded) must not leak a node.
const EXIT_GUARD_MS = 600;

const SEVERITIES = new Set(["ok", "warn", "err", "info"]);

/** "5s" / "5000ms" / "5000" -> milliseconds. Anything else -> null. */
function ms(value) {
	const s = String(value ?? "").trim();
	const m = s.match(/^(-?[\d.]+)\s*(ms|s)?$/);
	if (!m) return null;
	const n = parseFloat(m[1]);
	if (!isFinite(n) || n <= 0) return null;
	return m[2] === "s" ? n * 1000 : n;
}

attribute({
	name: "toast",
	requirement: { key: "denied", value: "allowed" },
	returnsValue: true,
	argNames: [],
	apply({ el, rx }) {
		const stack = el;
		const cfg = (() => { try { return rx() ?? {}; } catch { return {}; } })();

		if (!stack.hasAttribute("data-ignore-morph")) {
			console.warn("[toast] the stack has no data-ignore-morph — the next patch to its page will morph away every toast standing in it", stack);
		}

		// The top layer, once. `popover="manual"` never light-dismisses, so this
		// is opened at boot and not closed again. Where the API is missing the
		// ATTRIBUTE comes off, which is what turns components/toast.css's
		// `.toast-stack:not([popover])` fallback on — a stack left carrying an
		// unsupported attribute would match neither selector and never render.
		if (stack.hasAttribute("popover")) {
			if (typeof stack.showPopover !== "function") stack.removeAttribute("popover");
			else { try { stack.showPopover(); } catch { stack.removeAttribute("popover"); } }
		}

		const template = document.getElementById("toast-template");
		const cap = Number.isFinite(cfg.cap) && cfg.cap > 0 ? Math.floor(cfg.cap) : CAP_DEFAULT;

		// live = the toasts on screen, oldest first — which is also DOM order,
		// so a screen reader and the Tab ring walk them in the order they
		// arrived while column-reverse shows the newest nearest the corner.
		const live = [];
		// what is holding the timers: the nodes under the pointer, and whether
		// the keyboard is inside the stack. Both pause EVERY toast, not just
		// the one being read — retiring a toast out from under a pointer moves
		// the one being read down by its own height.
		const hovered = new Set();
		let keyboardInside = false;
		// where focus came from when it entered the stack, so dismissing the
		// last toast by keyboard does not drop focus on <body>.
		let returnTo = null;

		const held = () => hovered.size > 0 || keyboardInside;

		function dwellFor(spec) {
			const asked = ms(spec && spec.dwell) ?? ms(cfg.dwell);
			if (asked) return asked;
			const token = getComputedStyle(stack).getPropertyValue("--toast-dwell");
			return ms(token) ?? DWELL_FALLBACK_MS;
		}

		function arm(t) {
			if (t.timer || t.sticky || held()) return;
			t.since = performance.now();
			t.timer = setTimeout(() => { t.timer = null; retire(t); }, t.left);
		}
		function disarm(t) {
			if (!t.timer) return;
			clearTimeout(t.timer);
			t.timer = null;
			t.left = Math.max(0, t.left - (performance.now() - t.since));
		}
		const armAll = () => { if (!held()) for (const t of live) arm(t); };
		const disarmAll = () => { for (const t of live) disarm(t); };

		/** Take a toast off the screen: travel first, node second. */
		function retire(t) {
			if (t.leaving) return;
			t.leaving = true;
			disarm(t);
			const i = live.indexOf(t);
			if (i >= 0) live.splice(i, 1);

			// Focus is only ever inside a toast because the user PUT it there.
			// Hand it on rather than let it fall to <body>, which would send the
			// next Tab back to the top of the document.
			if (t.node.contains(document.activeElement)) {
				const next = live[live.length - 1];
				const onward = next && next.node.querySelector("button");
				if (onward) onward.focus();
				else if (returnTo && returnTo.isConnected) returnTo.focus();
				else if (document.activeElement !== document.body) document.activeElement.blur();
			}

			let done = false;
			const remove = () => {
				if (done) return;
				done = true;
				hovered.delete(t.node);
				t.node.remove();
				armAll();
			};
			t.node.addEventListener("transitionend", (ev) => { if (ev.target === t.node) remove(); });
			setTimeout(remove, EXIT_GUARD_MS);
			t.node.classList.add("leaving");
		}

		/** The node, built COMPLETE — see "announcement, once" at the head. */
		function build(spec) {
			let node;
			if (template && template.content && template.content.firstElementChild) {
				node = template.content.firstElementChild.cloneNode(true);
			} else {
				// No template in the page: the same shape, so a page that forgot
				// one still gets a correct toast rather than no toast.
				node = document.createElement("div");
				node.className = "toast";
				node.setAttribute("role", "alert");
				const content = document.createElement("div");
				content.className = "content";
				const title = document.createElement("b");
				title.className = "title";
				const line = document.createElement("span");
				line.className = "line";
				content.append(title, line);
				const close = document.createElement("button");
				close.type = "button";
				close.className = "round small";
				close.setAttribute("aria-label", "Dismiss");
				close.textContent = "×";
				node.append(content, close);
			}
			const severity = spec.severity && SEVERITIES.has(spec.severity) ? spec.severity : "";
			if (severity) node.classList.add(severity);
			// see "once, and at the right volume" at the head of this file.
			const assertive = spec.assertive ?? severity === "err";
			if (assertive) node.setAttribute("role", "alert");
			else node.removeAttribute("role");
			const title = node.querySelector(".title");
			const line = node.querySelector(".line");
			if (title) title.textContent = spec.title ?? "";
			// An empty .line is REMOVED and not left blank: components/toast.css
			// lifts the severity dot on `:not(:has(.line))`, and an empty span
			// would keep the two-line geometry for a one-line message.
			if (line) { if (spec.line) line.textContent = spec.line; else line.remove(); }
			const close = node.querySelector("button");
			if (close) close.addEventListener("click", () => retire(t));
			const t = { node, left: dwellFor(spec), sticky: !!spec.sticky, timer: null, since: 0, leaving: false };
			return t;
		}

		function raise(input) {
			const spec = typeof input === "string" ? { title: input } : (input || {});
			if (!spec.title && !spec.line) return null;
			const t = build(spec);
			live.push(t);
			stack.appendChild(t.node);
			// The cap is applied AFTER the append so the newest is never the one
			// thrown away, and the oldest leaves with its exit rather than
			// vanishing mid-sentence.
			while (live.length > cap) retire(live[0]);
			arm(t);
			return { dismiss: () => retire(t), node: t.node };
		}

		// ── what holds the clock ────────────────────────────────────────────
		// pointerenter/leave are listened for on the STACK but read from the
		// event target, because the stack itself is pointer-events:none and does
		// not match :hover — the CSS pseudo-class cannot be the source of truth
		// here, so the pointer's own events are.
		const onEnter = (ev) => { const t = ev.target.closest(".toast"); if (t) { hovered.add(t); disarmAll(); } };
		// the relatedTarget test is not optional: pointerout fires on every move
		// between two children of the SAME toast, and without it the clock is
		// released and re-taken on every pixel the pointer travels inside one.
		const onLeave = (ev) => {
			const t = ev.target.closest(".toast");
			if (!t || (ev.relatedTarget && t.contains(ev.relatedTarget))) return;
			hovered.delete(t);
			armAll();
		};
		const onFocusIn = (ev) => {
			if (!keyboardInside && ev.relatedTarget && !stack.contains(ev.relatedTarget)) returnTo = ev.relatedTarget;
			keyboardInside = true;
			disarmAll();
		};
		const onFocusOut = (ev) => {
			if (ev.relatedTarget && stack.contains(ev.relatedTarget)) return;
			keyboardInside = false;
			armAll();
		};
		// Escape retires the toast focus is standing in — and ONLY then. A
		// document-level Escape handler would take the key away from any dialog
		// open at the time, which is the one thing Escape is reserved for.
		const onKeyDown = (ev) => {
			if (ev.key !== "Escape") return;
			const node = ev.target.closest(".toast");
			const t = live.find((x) => x.node === node);
			if (!t) return;
			ev.stopPropagation();
			retire(t);
		};

		stack.addEventListener("pointerover", onEnter);
		stack.addEventListener("pointerout", onLeave);
		stack.addEventListener("focusin", onFocusIn);
		stack.addEventListener("focusout", onFocusOut);
		stack.addEventListener("keydown", onKeyDown);

		const onEvent = (ev) => raise(ev.detail);
		document.addEventListener("toast", onEvent);
		const previous = window.toast;
		window.toast = raise;

		// ── teardown ────────────────────────────────────────────────────────
		return () => {
			for (const t of live.slice()) { disarm(t); t.node.remove(); }
			live.length = 0;
			stack.removeEventListener("pointerover", onEnter);
			stack.removeEventListener("pointerout", onLeave);
			stack.removeEventListener("focusin", onFocusIn);
			stack.removeEventListener("focusout", onFocusOut);
			stack.removeEventListener("keydown", onKeyDown);
			document.removeEventListener("toast", onEvent);
			if (window.toast === raise) window.toast = previous;
		};
	},
});
