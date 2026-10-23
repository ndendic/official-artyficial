// deck.js: the Ribbon & Sheet shell's motion. Loaded once by index.html (module).
//
// Three surfaces, two fusions: the RIBBON, the PANEL it opens (the sidebar),
// and the SHEET. The active ribbon icon sits on a tab of the panel; the
// current row sits on a tab of the sheet. Small springs drive what is
// retargeted mid-flight: the desk (fold), both tabs, and drag releases.
// Everything else is a CSS transition or a WAAPI animation. A width change
// slides the chrome and reflows the content once, unseen (K2).
//
//   window.deck.toggleSidebar()        Ctrl/⌘ B, the trigger, the rail edge, the active icon
//   window.deck.openPanel(name, opts)  a ribbon icon, Alt+1…4, the palette, the account menu
//   window.deck.toggleInspector(b?)    Ctrl/⌘ ., the inspector buttons, Esc, the palette (the ONE writer of body.insp)
//   window.deck.lens(instant)          re-aim both tabs (after the tree re-renders)

const body = document.body;
const html = document.documentElement;
const sb = document.getElementById('shell-sidebar');
const nav = sb.querySelector(':scope > nav');
const ribbon = document.getElementById('ribbon');
const ribbonTab = document.getElementById('ribbon-tab');
const ribbonFlares = [...ribbon.querySelectorAll('.ribbon-flare')];
const inset = document.querySelector('.shell-inset.deck');
const tree = document.getElementById('shell-tree');
const tabEl = document.getElementById('deck-tab');
const flares = [...document.querySelectorAll('.deck-flare')];
const insp = document.getElementById('region-detail');
const aside = document.getElementById('aside');
const RM = matchMedia('(prefers-reduced-motion: reduce)');
const reduced = () => RM.matches;
const mobile = () => sb.dataset.layout === 'mobile';
const live = () => body.classList.contains('live');
const store = (k, v) => { try { v == null ? sessionStorage.removeItem(k) : sessionStorage.setItem(k, v); } catch {} };
const recall = (k) => { try { return sessionStorage.getItem(k); } catch { return null; } };
const remember = (k, v) => { try { localStorage.setItem(k, v); } catch {} };
const px = (name) => parseFloat(getComputedStyle(body).getPropertyValue(name)) || 0;
const clamp = (v, a = 0, b = 1) => Math.min(b, Math.max(a, v));
const smooth = (t) => t * t * (3 - 2 * t);
/* direct-dashboard's curves, as WAAPI easings: the glide (2% overshoot) and the tab (5%) */
const EASE_GLIDE = 'linear(0, 0.051, 0.169, 0.315, 0.464, 0.6, 0.716, 0.81, 0.882, 0.935, 0.972, 0.996, 1.01, 1.017, 1.02, 1.019, 1.017, 1.015, 1.012, 1.009, 1.006, 1.004, 1.003, 1.002, 1)';
const EASE_LEAVE = 'cubic-bezier(0.32, 0.72, 0, 1)';   /* = --ease-leave: exits ease out too */

/* ── springs: response (s) and damping ratio, Apple's parameterisation.
   deck ≈ direct-dashboard's glide (2% overshoot), lens ≈ its tab (5%) ── */
/* Width flights rest on CONTACT (`contact`; `floor` for a fold): the fade-in follows the
   visible landing, never a sub-pixel creep. tint: the ribbon tab's colour (0-100). */
const SPRING = { deck: { response: 0.34, damping: 0.86, rest: 2, contact: true }, fold: { response: 0.32, damping: 0.86, floor: 0, rest: 2, contact: true }, lens: { response: 0.30, damping: 0.69 }, panel: { response: 0.32, damping: 0.86 }, throw: { response: 0.32, damping: 0.8 }, tint: { response: 0.2, damping: 1.0 } };
class Spring {
	constructor(dims, apply) { this.x = Array(dims).fill(0); this.v = Array(dims).fill(0); this.t = Array(dims).fill(0); this.apply = apply; this.raf = 0; this.cfg = SPRING.deck; }
	set(x) { cancelAnimationFrame(this.raf); this.raf = 0; this.x = x.slice(); this.v = x.map(() => 0); this.t = x.slice(); this.done = null; this.apply(this.x); }
	to(t, cfg = SPRING.deck, done = null, v = null) {
		this.t = t.slice(); this.cfg = cfg; this.done = done;
		if (v) this.v = v.slice();
		this.side = this.x.map((x, i) => Math.sign(x - this.t[i]));   /* which side of the target it starts from */
		if (!this.raf) { this.last = -1; this.raf = requestAnimationFrame(this.tick); }
	}
	/* the first tick is 1/60 s (the work before the first frame is setup, not
	   motion) and later ones at most 1/30 s: under load it slows, never leaps */
	tick = (now) => {
		const dt = this.last < 0 ? 1 / 60 : Math.min(1 / 30, Math.max(0, (now - this.last) / 1000)); this.last = now;
		const { response, damping } = this.cfg;
		const k = (2 * Math.PI / response) ** 2, c = 4 * Math.PI * damping / response;
		const n = Math.max(1, Math.ceil(dt / 0.004)), h = dt / n;
		let rest = true;
		for (let i = 0; i < this.x.length; i++) {
			for (let j = 0; j < n; j++) { const a = -k * (this.x[i] - this.t[i]) - c * this.v[i]; this.v[i] += a * h; this.x[i] += this.v[i] * h; }
			const floored = this.cfg.floor !== undefined && this.t[i] <= this.cfg.floor && this.x[i] <= this.cfg.floor;
			const touched = this.cfg.contact && this.side?.[i] && Math.sign(this.x[i] - this.t[i]) !== this.side[i];
			if (!floored && !touched && (Math.abs(this.x[i] - this.t[i]) > (this.cfg.rest ?? 0.3) || Math.abs(this.v[i]) > 6 + 30 * ((this.cfg.rest ?? 0.3) - 0.3))) rest = false;
		}
		if (rest) {
			this.x = this.t.slice(); this.v = this.v.map(() => 0); this.raf = 0; this.apply(this.x);
			const d = this.done; this.done = null; d?.();
			return;
		}
		this.apply(this.x);
		this.raf = requestAnimationFrame(this.tick);
	};
}

/* ── FLIP the tree's rows around one layout commit (disclosure) ── */
function rowsOf() {
	return [...nav.querySelectorAll('#shell-tree li > :is(a, details > summary), #shell-tree [role=group] > h3, #navigation > *, #shell-tree .deck-pinned')];
}
function flip(mutate) {
	if (reduced() || !live()) { mutate(); return; }
	const rows = rowsOf();
	const before = new Map(rows.map(r => [r, r.getBoundingClientRect().top]));
	for (const r of rows) { r.classList.remove('deck-flip'); r.style.translate = ''; }
	mutate();
	const moved = [];
	for (const [r, top] of before) {
		if (!r.isConnected) continue;
		const now = r.getBoundingClientRect();
		if (!now.height) continue;
		const dy = top - now.top;
		if (Math.abs(dy) < 0.5) continue;
		r.style.translate = `0 ${dy}px`;
		moved.push(r);
	}
	if (!moved.length) return;
	requestAnimationFrame(() => requestAnimationFrame(() => {
		for (const r of moved) { r.classList.add('deck-flip'); r.style.translate = ''; }
	}));
}

/* ── THE ROW TAB (sheet → row): one spring over [clip-left, y, height];
   its frame also writes both flares, so the three never part ── */
let tabCtx = null;           /* the latest target: geometry the spring does not own */
const tabS = new Spring(3, ([x, y, h]) => {
	if (!tabCtx) return;
	const sc = tabCtx.sc;
	shellNav.paintTab({ ...tabCtx, x, y, h,
		clipT: sc ? Math.max(0, sc.top - y) : 0, clipB: sc ? Math.max(0, y + h - sc.bottom) : 0,
		off: sc ? (y + h <= sc.top + 2 || y >= sc.bottom - 2) : false });
	if (holding) foldFrame(deckS.x[0]);
});
function tabTarget(closing = null) {
	const t = shellNav.tabTarget(closing);
	if (!t) return null;
	const row = nav.querySelector('.panel[data-active] .panel-scroll [aria-current="page"]');
	t.sc = row?.closest('.panel-scroll')?.getBoundingClientRect() || null;
	return t;
}
function crossfadeTab() {
	for (const el of [tabEl, ...flares]) el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 160, easing: 'linear' });
}
function setTab(t, instant) {
	const was = body.classList.contains('deck-tab-on');
	if (!t) { tabCtx = null; body.classList.remove('deck-tab-on'); return; }
	const moved = Math.abs(tabS.t[0] - t.x) > 0.5 || Math.abs(tabS.t[1] - t.y) > 0.5;
	tabCtx = t;
	/* a tab that was not showing appears where it belongs (a fade, never a flight from nowhere) */
	if (instant || reduced() || !live() || !was) {
		if (reduced() && moved && was && live() && !instant) crossfadeTab();
		tabS.set([t.x, t.y, t.h]);
	} else tabS.to([t.x, t.y, t.h], SPRING.lens);
}
function lens(instant = false, closing = null) { setTab(tabTarget(closing), instant); }

/* ── THE RIBBON TAB (panel → icon): its own spring on y ── */
const ribS = new Spring(1, ([y]) => shellNav.paintRibbon({ y, h: 36 }));
{
	const t = shellNav.ribbonTarget();
	ribS.set([t ? t.y : 0]);
}
function reribbon(instant = false) {
	const t = shellNav.ribbonTarget();
	if (!t) return;
	if (instant || reduced() || !live()) ribS.set([t.y]);
	else ribS.to([t.y], SPRING.lens);
}

/* the tab leaves on PRESS, before the page loads: response on pointerdown */
const pageRow = (t) => t.closest?.('.panel-scroll a[href^="/"]:not(.inbox-list a)');
function aimAt(a) {
	const t = tabTarget();
	if (!t) return null;
	const r = a.getBoundingClientRect(), n = nav.getBoundingClientRect();
	return { ...t, x: r.left - n.left, y: r.top, h: r.height };
}
nav.addEventListener('pointerdown', (e) => {
	if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || mobile()) return;
	const a = pageRow(e.target);
	if (!a || a.getAttribute('aria-current') === 'page') return;
	const t = aimAt(a);
	if (!t) return;
	setTab(t);
	store('deck-lens-from', JSON.stringify({ x: t.x, y: t.y, at: Date.now() }));
});
addEventListener('pagehide', () => {
	const y = {};
	for (const s of nav.querySelectorAll('.panel-scroll')) y[s.closest('.panel').dataset.panel] = s.scrollTop;
	store('deck-panel-y', JSON.stringify(y));
});
/* a phone's page change from the drawer: see html.deck-drawer-swap in _shell.css */
addEventListener('pageswap', (e) => {
	if (!e.viewTransition || !mobile()) return;
	html.classList.add('deck-drawer-swap');
	store('deck-drawer-nav', '1');
});
addEventListener('pageshow', () => html.classList.remove('deck-drawer-swap', 'deck-drawer-going'));
/* a row tapped in the open drawer: the drawer holds until the page swaps (the
   library's close-on-tap left labels cut at the window's edge while it loaded) */
document.addEventListener('click', (e) => {
	if (!mobile() || e.defaultPrevented || e.button || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
	const a = e.target.closest?.('a[href^="/"]');
	if (!a || !nav.contains(a) || a.target || a.hasAttribute('download')) return;
	html.classList.add('deck-drawer-going');
	setTimeout(() => html.classList.remove('deck-drawer-going'), 3000);   /* a navigation that never happens lets go */
}, true);
/* keyboard / programmatic navigation: the tab still sets off before unload */
function go(a) {
	if (!a) return;
	const t = aimAt(a);
	if (t) { setTab(t); store('deck-lens-from', JSON.stringify({ x: t.x, y: t.y, at: Date.now() })); }
	const vt = 'onpagereveal' in window && CSS.supports('view-transition-name', 'a');
	if (vt || reduced()) location.href = a.href;          /* the VT carries the tab: no added latency */
	else setTimeout(() => { location.href = a.href; }, 90);
}

/* the FLIP fallback where cross-document View Transitions do not exist:
   start the tab where the previous page left it */
{
	const vt = 'onpagereveal' in window && CSS.supports('view-transition-name', 'a');
	const from = JSON.parse(recall('deck-lens-from') || 'null');
	store('deck-lens-from', null);
	if (!vt && from && Date.now() - from.at < 4000 && !reduced()) {
		requestAnimationFrame(() => {
			const t = tabTarget();
			if (!t) return;
			tabCtx = t; tabS.set([from.x, from.y, t.h]);
			requestAnimationFrame(() => lens());
		});
	}
}

/* ── WIDTH CHANGES (K2): THE CHROME SLIDES, THE CONTENT REFLOWS ONCE ──
   The fold, the inspector and a pin landing change the sheet's width. The
   sidebar or the inspector glides on its spring (the inspector's head rides
   it, never reflowing) and the sheet's frame follows. The CONTENT (header,
   title line, #main, the inspector's cards) is never drawn at a width in
   between: 1. it fades out (90ms) at its old layout under a soft 24px right
   edge; 2. once unseen it lays out ONCE at the final width and the charts
   re-plot there, one a frame (preland); 3. the frame after the last landing
   it fades in (140ms), settling 6px the way the edge went. A theme waiting on
   the flight takes the landing (landed/reveal): its reveal first, then this.
   Ctrl-B mid-flight reverses the springs from where they are and re-plans the
   final layout; the content stays faded until the last landing. Reduced
   motion: the layout at once. */
const flying = new Set();
const mainEl = document.getElementById('main');
const canvasEl = document.querySelector('.sheet .canvas-scroll');
const OUT_MS = 90, IN_MS = 140;
const veiled = new Map();   /* element → its fade-out */
let unseen = false, veilDone = Promise.resolve(), held = false, settleDx = 0, innerFrom0 = false;
let fadeIns = [], idleWait = [], landWait = [], landing = 0, heldBack = 0;
const inspBody = () => inspInner ? [...inspInner.querySelectorAll(':scope > :is(#aside, .pin-tab, .insp-empty)')] : [];
const contentEls = () => [...(canvasEl ? canvasEl.querySelectorAll(':scope > :is(#header, .page-head, #main, .welcome)') : []),
	...(inspFlying ? inspBody() : [])].filter(e => e.getClientRects().length);
function fadeOut() {
	const add = contentEls().filter(el => !veiled.has(el));
	/* a fade-in still playing leaves from where it is (read before cancelling) */
	const from = add.map(el => innerFrom0 && inspInner?.contains(el) ? 0 : +getComputedStyle(el).opacity);
	for (const a of fadeIns) a.cancel();
	fadeIns = [];
	/* started on the action's frame: the edge moves from the first painted frame */
	const now = document.timeline.currentTime;
	add.forEach((el, i) => { const o = el.animate([{ opacity: from[i] }, { opacity: 0 }], { duration: OUT_MS, easing: EASE_LEAVE, fill: 'forwards' }); o.startTime = now; veiled.set(el, o); });
	return add;
}
function holdAt(w, ml) {
	if (!mainEl) return;
	held = true;
	mainEl.style.inlineSize = w + 'px';   /* exact: the release must not move a chart by a rounding */ mainEl.style.flex = 'none'; mainEl.style.marginInlineStart = ml + 'px';
}
function unhold() {
	held = false;
	if (mainEl) mainEl.style.inlineSize = mainEl.style.flex = mainEl.style.marginInlineStart = '';
}
const mainBox = () => [mainEl.getBoundingClientRect().width, parseFloat(getComputedStyle(mainEl).marginInlineStart) || 0];
function startVeil() {
	cancelAnimationFrame(landing); landing = 0;   /* a flight that starts during a landing's frame re-holds as it is */
	clearTimeout(heldBack); heldBack = 0; uncloak(); laidOut = false;
	html.classList.add('deck-reflowing');
	window.shell?.hurryThemeVT?.();   /* no flight moves under a frozen theme snapshot */
	if (mainEl && !held) holdAt(...mainBox());
	if (canvasEl) canvasEl.style.mask = 'linear-gradient(to left, transparent, #000 24px)';
	const add = fadeOut();
	if (add.length) waitUnseen(veiled.get(add[0])); else unseen = true;
}
function waitUnseen(out) {
	unseen = false;
	veilDone = out.finished.catch(() => {}).then(() => { unseen = true; queuePre(); });
}
/* a plot costs ~18ms, six in the landing frame ~120ms of empty sheet: once unseen the content
   lays out at the FINAL width and each chart re-plots in a frame of its own (pinned until its turn) */
let preT = 0, pinned = [];
const queuePre = () => { cancelAnimationFrame(preT); preT = requestAnimationFrame(preland); };
function preland() {
	preT = 0;
	if (!unseen || !flying.size || !mainEl) return;
	const charts = [...mainEl.querySelectorAll('rt-chart')].filter(c => c.getClientRects().length), ws = charts.map(c => c.getBoundingClientRect().width);
	const lift = [[inset.style, 'marginLeft'], [inset.style, 'padding'], [insp.style, 'marginInlineEnd']], was = lift.map(([s, k]) => s[k]), pad = insp.style.getPropertyValue('--deck-pad');
	lift.forEach(([s, k]) => { s[k] = ''; }); insp.style.removeProperty('--deck-pad'); unhold();
	const box = mainBox();
	lift.forEach(([s, k], i) => { s[k] = was[i]; }); if (pad) insp.style.setProperty('--deck-pad', pad);
	charts.forEach((c, i) => { c.style.inlineSize = ws[i] + 'px'; }); pinned = charts;
	holdAt(...box);
	const next = () => { const c = pinned.shift(); preT = c ? requestAnimationFrame(next) : 0; if (c) c.style.inlineSize = ''; };
	preT = requestAnimationFrame(next);
}
function land() {
	if (flying.size) return;
	/* the fade-out ends first: the layout never changes in sight */
	if (!unseen) { veilDone.then(() => { if (!flying.size && veiled.size) land(); }); return; }
	if (landing) return;
	if (landWait.length) { handBack(); return; }   /* a waiting theme lays it out inside its own update */
	/* the one reflow: rt-chart re-plots on its own (its ResizeObserver, then
	   a rAF in the next frame, before that frame paints); a synchronous
	   re-plot here as well would plot every chart twice */
	relayout();
	landing = requestAnimationFrame(() => { landing = 0; if (!flying.size) fadeBack(); });
}
function relayout() {
	uncloak(); unhold(); cancelAnimationFrame(preT); preT = 0;
	for (const c of pinned.splice(0)) c.style.inlineSize = '';
	if (canvasEl) canvasEl.style.mask = '';
	dispatchEvent(new Event('deck:width'));   /* the title's path and the scroll edges re-measure, unseen */
}
/* a theme waiting on a flight takes its landing: the content stays unseen until reveal() (900ms
   at most), skipped (content-visibility at its own content box) with the veil over it out of the
   paint, so the reveal's capture and swap style and draw only the chrome (L1: ~40ms less) */
let cloaked = [];
function cloak() {
	cancelAnimationFrame(preT); preT = 0;   /* no chart plots in the capture frame (~50ms); relayout() frees the rest */
	const els = [...veiled.keys()].filter(el => el.isConnected && !cloaked.includes(el)), n = (c, a, b) => (parseFloat(c[a]) || 0) + (parseFloat(c[b]) || 0);
	const box = els.map(el => { const r = el.getBoundingClientRect(), c = getComputedStyle(el); return `${Math.max(0, r.width - n(c, 'paddingLeft', 'paddingRight') - n(c, 'borderLeftWidth', 'borderRightWidth'))}px ${Math.max(0, r.height - n(c, 'paddingTop', 'paddingBottom') - n(c, 'borderTopWidth', 'borderBottomWidth'))}px`; });
	els.forEach((el, i) => { el.style.containIntrinsicSize = box[i]; el.style.contentVisibility = 'hidden'; });
	cloaked.push(...els); html.classList.toggle('deck-cloaked', cloaked.length > 0);
	if (canvasEl) canvasEl.style.mask = '';   /* it masks nothing now: one render pass less */
}
function uncloak() {
	for (const el of cloaked.splice(0)) el.style.contentVisibility = el.style.containIntrinsicSize = '';
	html.classList.remove('deck-cloaked');
}
function handBack() {
	/* L1: the landing's reflow and the plots happen HERE, in the landing frame,
	   BEFORE the theme's startViewTransition: the content stays at its veil's
	   opacity 0, already laid out at the final width, so the update callback
	   swaps the theme only and reveal(false) just fades it in (K2's 140ms)
	   under the reveal. (cloak() stays for reference: skipping the content
	   left the sheet blank until the reveal had ended) */
	relayout(); replotNow(); laidOut = true;
	const w = landWait; landWait = [];
	clearTimeout(heldBack); heldBack = setTimeout(() => reveal(false), 900);
	for (const r of w) r(true);
}
const landed = () => !flying.size && (!veiled.size || heldBack) ? Promise.resolve(!!heldBack) : new Promise(r => {
	landWait.push(r);
	if (landing) { cancelAnimationFrame(landing); landing = 0; handBack(); }   /* landed already, its fade-in not begun */
});
let laidOut = false;   /* handBack() has laid the landing out already: reveal(false) only fades it in */
function reveal(instant) {
	if (!heldBack || flying.size) return;
	clearTimeout(heldBack); heldBack = 0;
	/* laid out already (handBack): with no theme taking it (the 900ms timer), K2's own fade-in */
	if (!instant && laidOut) { laidOut = false; cancelAnimationFrame(landing); landing = 0; fadeBack(); return; }
	/* the theme's swap (index.html, at updateCallbackDone, outside the update
	   callback): shown at once, no layout (handBack did it), so the new view
	   is painted ONCE before the reveal's first frame and the disc itself
	   brings the content in. A 140ms fade-in under the reveal re-captured the
	   live view every frame and stalled the disc (~160ms, then a jump) */
	if (instant && laidOut) {
		laidOut = false; cancelAnimationFrame(landing); landing = 0;
		for (const [, out] of veiled) out.cancel();
		veiled.clear(); unseen = false;
		html.classList.remove('deck-reflowing');
		const w = idleWait; idleWait = []; for (const r of w) r();
		return;
	}
	laidOut = false;
	/* laid out in a frame, as land() does, so rt-chart's plot lands in the fade-in's frame */
	if (!instant) { landing = requestAnimationFrame(() => { relayout(); landing = requestAnimationFrame(() => { landing = 0; if (!flying.size) fadeBack(); }); }); return; }
	/* at once, inside the theme's update: ONE restyle and layout for the new
	   width and the new theme, the plots drawn now, then shown */
	relayout(); replotNow();
	for (const [, out] of veiled) out.cancel();
	veiled.clear(); unseen = false;
	html.classList.remove('deck-reflowing');
	const w = idleWait; idleWait = []; for (const r of w) r();
}
function fadeBack() {
	unseen = false;
	/* no replotNow: rt-chart plots in its own rAF this frame, before paint (a synchronous one too doubled the frame) */
	const ease = 'cubic-bezier(0.23, 1, 0.32, 1)', dx = reduced() ? 0 : settleDx;
	/* each comes back to its OWN rest opacity, and the settle is ADDED to its own translate: the
	   .pin-tab sits on its card by translate, and replacing it put a sheet-tone block over the head */
	const els = [...veiled.keys()]; for (const out of veiled.values()) out.cancel(); const ends = els.map(el => +getComputedStyle(el).opacity), o = { duration: reduced() ? 100 : IN_MS, easing: ease };
	els.forEach((el, i) => fadeIns.push(el.animate([{ opacity: 0 }, { opacity: ends[i] }], o), el.animate([{ translate: `${dx}px 0` }, { translate: '0 0' }], { ...o, composite: 'add' })));
	veiled.clear();
	html.classList.remove('deck-reflowing');
	const these = fadeIns;
	Promise.all(these.map(a => a.finished.catch(() => {}))).then(() => {
		if (fadeIns !== these || flying.size) return;
		fadeIns = [];
		const w = idleWait; idleWait = []; for (const r of w) r();
	});
}
function reflowing(key, on) {
	const was = flying.size > 0;
	on ? flying.add(key) : flying.delete(key);
	if (flying.size && !was) startVeil();
	else if (on) { const add = fadeOut(); if (add.length) waitUnseen(veiled.get(add[0])); }   /* the inspector's column, during a fold */
	if (!flying.size && was) land();
}
const busy = () => flying.size > 0 || veiled.size > 0 || fadeIns.length > 0;
const idle = () => busy() ? new Promise(r => idleWait.push(r)) : Promise.resolve();   /* nothing flies, the content is back */
/* the theme switch cuts a long flight short: a stiff spring from where it is */
const HURRY = { response: 0.1, damping: 1.0, floor: 0, rest: 2.5, contact: true };   /* rests within 2.5px (≈ one frame of its tail): the reveal captures the landed frame, never a frame of sub-pixel creep first */
function hurry() {
	if (holding && deckS.raf) deckS.to(deckS.t, HURRY, endHold);
	if (inspFlying && inspS.raf) inspS.to(inspS.t, HURRY, inspDone);
}
/* re-plot NOW every chart whose plot no longer matches its box: one toggle
   of a trailing space in its label (rt-chart's synchronous re-plot path,
   invisible in caption and summary), so each chart draws once */
function replotNow() {
	const charts = [...document.querySelectorAll('.sheet #main rt-chart')].filter(c => c.getClientRects().length && c.querySelector('svg'));
	const stale = charts.filter(c => { const svg = c.querySelector('svg'), plot = svg.parentElement; return Math.abs((plot.clientWidth || c.clientWidth) - (+svg.getAttribute('width') || 0)) >= 0.5; });
	for (const c of stale) {
		const l = c.getAttribute('label') ?? '';
		c.setAttribute('label', l.endsWith(' ') ? l.slice(0, -1) : l + ' ');
	}
}

/* x-axis labels: rt-chart always prints the last label, so at some widths it
   lands on its neighbour ("Sep 25" over "Sep 26"). After every plot, a label
   that would come within 8px of the one kept before it hides; the last label
   always stays (the one before it gives way instead). Reads, then writes. */
function declutter(c) { declutterAll([c]); }
function declutterAll(cs) {
	const GAP = 8, plan = [];
	for (const c of cs) {
		const rows = new Map();
		for (const t of c.querySelectorAll('svg text.rt-chart-tick[text-anchor="middle"]')) {
			const y = t.getAttribute('y'); (rows.get(y) || rows.set(y, []).get(y)).push(t);
		}
		for (const row of rows.values()) {
			if (row.length < 3) continue;
			const b = row.map(t => t.getBBox()).map(r => [r.x, r.x + r.width]);
			const hide = new Set();
			let kept = 0;
			for (let i = 1; i < row.length; i++) {
				if (b[i][0] >= b[kept][1] + GAP) { kept = i; continue; }
				if (i === row.length - 1 && kept > 0) { hide.add(kept); kept = i; } else hide.add(i);
			}
			plan.push([row, hide]);
		}
	}
	for (const [row, hide] of plan) row.forEach((t, i) => { const v = hide.has(i) ? 'hidden' : ''; if (t.style.visibility !== v) t.style.visibility = v; });
}

/* ── THE FOLD: the middle layer leaves completely ──
   One value, x = the panel's visible width (side-w open, 0 folded), drives
   the sheet's left margin (gliding over the panel, laid out whole beneath),
   the row tab's flares, the sheet's left corners, and the ribbon tab's colour
   (the panel's, then the sheet's at contact, so the icon fuses into the sheet).
   Unfolding runs the same mapping backwards. */
let holding = false, foldTx = 8, G = { S: 264, rw: 52, r: 14, gap: 8 };   /* geometry, read once per flight */
const sheetEl = document.querySelector('.sheet');
const deckS = new Spring(1, ([x]) => foldFrame(x));
/* the ribbon tab's colour, 0-100: INTO contact it follows the edge exactly
   (the last 28px of a fold take about 200 ms, a legible hand-off); OUT of
   contact it eases after the edge on spring.tint, so the tab visibly turns
   from the sheet's colour back to the panel's as the panel slides in */
const qS = new Spring(1, ([q]) => { if (holding) ribbon.style.setProperty('--rtab-q', (clamp(q / 100)).toFixed(3)); });
function foldFrame(x) {
	if (!holding) return;
	const xc = Math.max(0, x);
	inset.style.marginLeft = (G.rw + xc).toFixed(2) + 'px';
	/* the desk's gutter and the sheet's radius leave with the panel; plain lengths on the two
	   boxes (an inherited custom property on the desk restyled the whole sheet every frame) */
	const fq = clamp(1 - xc / G.S), pad = (G.gap * (1 - fq)).toFixed(2) + 'px';
	inset.style.padding = `${pad} ${pad} ${pad} 0`;
	insp.style.setProperty('--deck-pad', pad);
	sheetEl.style.borderStartEndRadius = sheetEl.style.borderEndEndRadius = (G.r * (1 - fq)).toFixed(2) + 'px';
	const contact = clamp(xc / 28);                       /* 0 where the sheet touches the ribbon */
	const qT = 100 * (1 - smooth(contact));
	if (qT >= qS.x[0] - 0.05) qS.set([qT]); else if (Math.abs(qS.t[0] - qT) > 0.05 || !qS.raf) qS.to([qT], SPRING.tint);
	const r = (G.r * Math.min(1 - fq, smooth(contact))).toFixed(2) + 'px';
	sheetEl.style.borderStartStartRadius = r; sheetEl.style.borderEndStartRadius = r;
	const ro = clamp((xc - foldTx - 6) / 14).toFixed(3);
	for (const f of flares) f.style.opacity = tabCtx && (tabCtx.clipT > 0.5 || tabCtx.clipB > 0.5) ? '0' : ro;
	/* the panel (chrome) stays painted until the sheet's edge reaches it: each
	   word, icon and badge fades over the 24px before the edge would cut it
	   (navFade); the row tab and its flares leave WITH the selected row's label */
	const ao = navFade(G.rw + xc);
	tabEl.style.opacity = ao > 0.999 ? '' : ao.toFixed(3);
	if (ao < 0.999) for (const f of flares) f.style.opacity = Math.min(+f.style.opacity || 0, ao).toFixed(3);
}
/* the panel's leaves (word blocks, icons, badges), read once per flight:
   [element, right edge]; the panel does not move under the sheet */
let navLeaves = null, navRow = null;
function readNavLeaves() {
	const nr = nav.getBoundingClientRect();
	if (!nr.width) return null;
	const out = [], rg = document.createRange();
	const add = (el, r) => { if (r.width > 0.5 && r.bottom > nr.top && r.top < nr.bottom) out.push([el, r.right]); };
	const walk = (e) => {
		for (const c of e.children) {
			if (!c.getClientRects().length) continue;
			if (c.matches('svg, img, input, .rt-badge, kbd')) { add(c, c.getBoundingClientRect()); continue; }
			/* an element holding words is measured by its words alone (a row's
			   chevron or count is its own piece, faded first), then walked on */
			const words = [...c.childNodes].filter(n => n.nodeType === 3 && n.textContent.trim());
			if (words.length) { rg.setStartBefore(words[0]); rg.setEndAfter(words[words.length - 1]); add(c, rg.getBoundingClientRect()); }
			walk(c);
		}
	};
	walk(nav);
	for (const [el] of out) el.style.transition = 'none';   /* a hover or arrival transition on opacity would lag the edge */
	navRow = nav.querySelector('.panel[data-active] .panel-scroll [aria-current="page"]');
	return out;
}
function navFade(edge) {
	if (!navLeaves) navLeaves = readNavLeaves();
	if (!navLeaves) return 1;
	let row = 1;
	/* each piece fades over the 24px before the edge reaches its right end;
	   a piece nearer than that to the panel's own edge (a count, a chevron)
	   uses the room it has, so at rest every piece is whole */
	const rest = G.rw + G.S;
	for (const [el, r] of navLeaves) {
		const o = clamp((edge - r) / clamp(rest - r, 4, 24)), t = o > 0.999 ? '' : o.toFixed(3);
		if (el.style.opacity !== t) el.style.opacity = t;
		if (navRow && navRow.contains(el)) row = Math.min(row, o);
	}
	return row;
}
function navUnfade() {
	if (navLeaves) for (const [el] of navLeaves) { el.style.opacity = ''; el.style.transition = ''; }
	navLeaves = navRow = null;
}
function endHold() {
	body.classList.remove('deck-hold'); holding = false;
	cancelAnimationFrame(qS.raf); qS.raf = 0;
	inset.style.marginLeft = inset.style.padding = '';
	insp.style.removeProperty('--deck-pad');
	ribbon.style.removeProperty('--rtab-q');
	sheetEl.style.borderStartStartRadius = sheetEl.style.borderEndStartRadius = '';
	sheetEl.style.borderStartEndRadius = sheetEl.style.borderEndEndRadius = '';
	for (const f of flares) f.style.opacity = '';
	navUnfade();
	nav.style.opacity = tabEl.style.opacity = '';
	reflowing('fold', false);
	lens(true);
}
/* before rt-sidebar upgrades, sb.expanded is undefined: read the pre-paint
   attribute (else the frame a page switch captures was folded, then dropped 8px) */
const sbOpen = () => typeof sb.expanded === 'boolean' ? sb.expanded : sb.getAttribute('data-state') !== 'collapsed';
function syncFold() {
	body.classList.toggle('deck-folded', !mobile() && !sbOpen());
	shellNav.showPanel(nav.dataset.panel);
}
function saveSide() { if (!mobile()) remember('shell-sidebar', sb.expanded ? 'expanded' : 'collapsed'); }
function toggleSidebar() {
	if (typeof sb.toggle !== 'function') return;
	if (mobile()) { sb.toggle(); return; }
	if (reduced() || !live()) {
		sb.toggle(); saveSide(); syncFold(); lens(true); replotNow();   /* the new width's plots land in this frame, never one late */
		/* reduced motion: nothing travels and nothing is cross-dissolved (two
		   layouts never share a frame). The fold is a clean cut; an unfold
		   brings the panel's contents in on one short fade, the only layer
		   that changes opacity, so the eye still finds what arrived */
		if (live() && sb.expanded) activePanel()?.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 120, easing: 'linear' });
		return;
	}
	const S = px('--side-w');
	G = { S, rw: px('--ribbon-w'), r: px('--deck-radius') || 14, gap: px('--deck-gap') };
	if (!holding) {
		body.classList.add('deck-hold'); holding = true;
		reflowing('fold', true);   /* the content fades, held at the layout of NOW, before the commit below */
		deckS.set([sb.expanded ? S : 0]);
	}
	const from = deckS.x[0];
	sb.toggle();
	saveSide();
	syncFold();
	lens(true);
	foldTx = tabCtx ? tabCtx.x : 8;
	settleDx = sb.expanded ? -6 : 6;   /* the content settles the way the edge went */
	/* an inspector still in flight retargets to the open width of the new fold state */
	if (inspFlying && inspS.t[0] > 0) inspS.to([inspOpenW()], inspS.cfg, inspDone);
	deckS.to([sb.expanded ? S : 0], sb.expanded ? SPRING.deck : SPRING.fold, endHold);
	if (unseen) queuePre();   /* a reversal re-plans the final layout, unseen */
	foldFrame(deckS.x[0]);
	/* the rows cascade in only when the panel was really hidden: a reversal
	   of a fold that barely started must not blink the rows */
	if (sb.expanded && from < 0.35 * S) cascade(activePanel(), -1, 60);
}
/* the trigger and the rail edge: the desktop toggle is ours, the drawer is the library's */
document.addEventListener('click', (e) => {
	const trig = e.target.closest?.('[data-rt-sidebar-trigger="shell-sidebar"], .deck-edge');
	if (!trig || (mobile() && !trig.classList.contains('deck-edge'))) return;
	e.preventDefault(); e.stopPropagation();
	toggleSidebar();
}, true);

/* ── THE PANELS: one per ribbon icon. A swap is ONE motion: both panels move
   by icon order (12px) with their opacity, the outgoing one on the leaving
   curve (140ms), the incoming one on the arriving curve (220ms), no
   overshoot (nothing was thrown). The row cascade is kept for the unfold
   only, where the panel really appears. An interrupted swap starts from
   the live values, never from a snapped end state. ── */
const ORDER = shellNav.order || shellNav.panels;   /* the ribbon's top-to-bottom order */
const activePanel = () => nav.querySelector('.panel[data-active]');
let leaving = [];
let cascaded = [];
function cascade(panel, dir, base = 40) {
	for (const a of cascaded) a.cancel();
	cascaded = [];
	if (!panel || reduced() || !live()) return;
	const rows = [...panel.querySelectorAll('.panel-head, [role=group] > h3:not(.sr-only), li > :is(a, details > summary, button, .data-row), .set-seg, .set-who, .set-keys > li, .panel-note, .ha-tools, .ha-group-h, .ha-row')]
		.filter(r => r.getClientRects().length).slice(0, 14);
	rows.forEach((r, i) => cascaded.push(r.animate(
		[{ opacity: 0, translate: `0 ${dir * 6}px` }, { opacity: 1, translate: '0 0' }],
		{ duration: 240, delay: base + i * 28, easing: EASE_GLIDE, fill: 'backwards' })));
}
const liveOf = (el) => { const cs = getComputedStyle(el); return { opacity: cs.opacity, translate: cs.translate === 'none' ? '0px 0px' : cs.translate }; };
function switchPanel(name, { instant = false } = {}) {
	const from = activePanel(), to = nav.querySelector(`.panel[data-panel="${name}"]`);
	if (!to || from === to) return false;
	/* read where an interrupted swap left both panels BEFORE cancelling it */
	const busy = leaving.length > 0;
	const fromLive = from && busy ? liveOf(from) : null;
	const toLive = busy && to.getAnimations().length ? liveOf(to) : null;
	for (const a of leaving) a.cancel();
	leaving = [];
	for (const p of nav.querySelectorAll('.panel.panel-leaving')) p.classList.remove('panel-leaving');
	const dir = ORDER.indexOf(name) > ORDER.indexOf(from?.dataset.panel) ? 1 : -1;
	remember('deck-panel', name);
	shellNav.showPanel(name);
	reribbon(instant);
	lens(instant);
	if (instant || !live() || !from) return true;
	if (reduced()) {
		to.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 140, easing: 'linear' });
		return true;
	}
	from.classList.add('panel-leaving');
	const out = from.animate([fromLive || { opacity: 1, translate: '0 0' }, { opacity: 0, translate: `0 ${-dir * 12}px` }], { duration: 140, easing: EASE_LEAVE, fill: 'forwards' });
	out.finished.then(() => { from.classList.remove('panel-leaving'); out.cancel(); leaving = leaving.filter(a => a !== out); }, () => {});
	const inn = to.animate([toLive || { opacity: 0, translate: `0 ${dir * 12}px` }, { opacity: 1, translate: '0 0' }], { duration: 220, easing: 'cubic-bezier(0.23, 1, 0.32, 1)' });
	/* the tab was aimed at the row's resting place; aim once more on landing
	   in case the panel's scroll or a group changed under the glide */
	inn.finished.then(() => { leaving = leaving.filter(a => a !== inn); lens(); }, () => {});
	leaving.push(out, inn);
	return true;
}
function focusPanel() {
	const p = activePanel();
	const first = p?.querySelector('[aria-current="page"]') || p?.querySelector('.panel-scroll [data-ha-find]') || p?.querySelector('.panel-scroll :is(a[href], button, summary)');
	first?.focus({ preventScroll: false });
}
/* open a sidebar: from folded it unfolds onto that panel; toggle=true makes
   the active icon fold it (the ribbon's own click, Alt+n) */
function openPanel(name, { focus = false, toggle = false } = {}) {
	if (!ORDER.includes(name)) return;
	if (ribbon.querySelector(`.ribbon-btn[data-panel="${name}"]`)?.hidden) return;   /* Agents without an agent */
	if (mobile()) {
		switchPanel(name, { instant: true });
		if (!sb.expanded) sb.open?.();
		if (focus) requestAnimationFrame(focusPanel);
		return;
	}
	if (!sb.expanded) {
		switchPanel(name, { instant: true });
		toggleSidebar();
	} else if (nav.dataset.panel === name) {
		if (toggle) { toggleSidebar(); return; }
	} else switchPanel(name);
	if (focus) requestAnimationFrame(focusPanel);
}
/* ribbon tooltips: hushed from the press until the pointer leaves (the click
   already opened what the tooltip names), delayed on the first hover, then
   instant for neighbours while the pointer stays on the ribbon (a group) */
{
	let warmT = 0, coolT = 0;
	ribbon.addEventListener('pointerdown', (e) => { e.target.closest?.('[data-tooltip]')?.setAttribute('data-tip-hush', ''); });
	ribbon.addEventListener('pointerout', (e) => {
		const t = e.target.closest?.('[data-tip-hush]');
		if (t && !t.contains(e.relatedTarget)) t.removeAttribute('data-tip-hush');
	});
	ribbon.addEventListener('pointerover', (e) => {
		if (e.pointerType !== 'mouse' || !e.target.closest?.('[data-tooltip]')) return;
		clearTimeout(coolT);
		if (!ribbon.hasAttribute('data-tip-warm')) { clearTimeout(warmT); warmT = setTimeout(() => ribbon.setAttribute('data-tip-warm', ''), 420); }
	});
	ribbon.addEventListener('pointerleave', () => { clearTimeout(warmT); coolT = setTimeout(() => ribbon.removeAttribute('data-tip-warm'), 300); });
}
ribbon.addEventListener('click', (e) => {
	const b = e.target.closest('.ribbon-btn');
	if (!b) return;
	openPanel(b.dataset.panel, { toggle: true, focus: e.detail === 0 && !(nav.dataset.panel === b.dataset.panel && sb.expanded) });
});
/* the ribbon is one tab stop: ↑ ↓ Home End move, → enters the open panel */
ribbon.addEventListener('keydown', (e) => {
	const btns = [...ribbon.querySelectorAll('.ribbon-btn:not([hidden])')];
		const i = btns.indexOf(document.activeElement);
	if (i < 0) return;
	const to = (j) => { e.preventDefault(); const b = btns[(j + btns.length) % btns.length]; for (const x of btns) x.tabIndex = x === b ? 0 : -1; b.focus(); };
	if (e.key === 'ArrowDown') to(i + 1);
	else if (e.key === 'ArrowUp') to(i - 1);
	else if (e.key === 'Home') to(0);
	else if (e.key === 'End') to(btns.length - 1);
	else if (e.key === 'ArrowRight' && sb.expanded && btns[i].dataset.panel === nav.dataset.panel) { e.preventDefault(); focusPanel(); }
});
/* the other panels' rows by keyboard: ↑ ↓ Home End; ← returns to the ribbon */
nav.addEventListener('keydown', (e) => {
	if (e.metaKey || e.ctrlKey || e.altKey) return;
	const p = activePanel();
	if (!p || p.id === 'panel-pages' && e.key !== 'ArrowLeft') return;
	const rows = [...p.querySelectorAll('.panel-scroll :is(li > a, li > button, li > details > summary, .set-seg button, .ha-new, .ha-group-h, .ha-row, .ha-close)')].filter(r => r.checkVisibility());
	const i = rows.indexOf(document.activeElement);
	if (i < 0) return;
	const f = (r) => { if (r) { e.preventDefault(); r.focus(); } };
	if (e.key === 'ArrowLeft' && (p.id !== 'panel-pages' || !document.activeElement.closest('details[open] > ul, details[open] > summary'))) {
		if (p.id !== 'panel-pages' || !rows[i].closest('details')) { e.preventDefault(); ribbon.querySelector('.ribbon-btn[data-active]')?.focus(); }
		return;
	}
	if (p.id === 'panel-pages') return;
	if (e.key === 'ArrowDown') f(rows[i + 1]);
	else if (e.key === 'ArrowUp') f(rows[i - 1]);
	else if (e.key === 'Home') f(rows[0]);
	else if (e.key === 'End') f(rows.at(-1));
});
/* a panel's scroll moves the row tab at once (no spring while the list scrolls) */
for (const s of nav.querySelectorAll('.panel-scroll')) s.addEventListener('scroll', () => { if (s.closest('.panel[data-active]')) lens(true); }, { passive: true });

/* the ribbon rides in the drawer on a phone: one element, two homes */
function placeRibbon() {
	if (mobile()) { if (ribbon.parentElement !== nav) nav.prepend(ribbon); }
	else if (ribbon.parentElement !== body) body.insertBefore(ribbon, tabEl);
}
sb.addEventListener('rt-sidebar-change', () => {
	placeRibbon(); syncFold(); requestAnimationFrame(() => { lens(true); reribbon(true); });
	/* the drawer opens on the active panel's current row, not on the mark */
	if (mobile() && sb.expanded) setTimeout(() => {
		const p = activePanel();
		(p?.querySelector('.panel-scroll [aria-current="page"]') || p?.querySelector('.panel-scroll :is(a[href], button, summary)'))?.focus({ preventScroll: true });
	}, 30);
});

/* ── SUBMENUS: open condenses (CSS @starting-style), close fades then commits ── */
function setOpen(d, open) {
	clearTimeout(d._deckT);
	if (open) {
		d.classList.remove('deck-closing');
		flip(() => { d.open = true; });
		lens();
		return;
	}
	if (reduced() || !live()) { flip(() => { d.open = false; }); lens(); return; }
	d.classList.add('deck-closing');
	lens(false, d);
	d._deckT = setTimeout(() => {
		d.classList.remove('deck-closing');
		flip(() => { d.open = false; });
		lens();
	}, 120);
}
tree.addEventListener('click', (e) => {
	const s = e.target.closest('summary');
	if (!s || !tree.contains(s)) return;
	e.preventDefault();
	const d = s.parentElement;
	setOpen(d, !(d.open && !d.classList.contains('deck-closing')));
});

/* ── THE TREE BY KEYBOARD: ↑ ↓ Home End, → ← open/close/step, type-ahead ── */
const visibleRows = () => [...tree.querySelectorAll('li > a, li > details > summary')].filter(r => r.checkVisibility());
let typed = '', typedAt = 0;
tree.addEventListener('keydown', (e) => {
	if (e.metaKey || e.ctrlKey || e.altKey || e.key === '/') return;
	const rows = visibleRows(); const row = document.activeElement; const i = rows.indexOf(row);
	if (i < 0) return;
	const d = row.tagName === 'SUMMARY' ? row.parentElement : null;
	const focus = (r) => { if (r) { e.preventDefault(); r.focus(); } };
	switch (e.key) {
		case 'ArrowDown': return focus(rows[i + 1]);
		case 'ArrowUp': return focus(rows[i - 1]);
		case 'Home': return focus(rows[0]);
		case 'End': return focus(rows.at(-1));
		case 'ArrowRight':
			if (d && !d.open) { e.preventDefault(); setOpen(d, true); }
			else if (d) focus(rows[i + 1]);
			return;
		case 'ArrowLeft':
			if (d && d.open) { e.preventDefault(); setOpen(d, false); }
			else focus(row.closest('details:not(:has(> summary:focus))')?.querySelector(':scope > summary') || row.parentElement.closest('ul')?.closest('details')?.querySelector(':scope > summary'));
			return;
		case 'Enter': case ' ':
			if (d) { e.preventDefault(); setOpen(d, !d.open); }
			else if (e.key === 'Enter' && row.href) { e.preventDefault(); go(row); }
			return;
	}
	if (e.key.length === 1 && /\S/.test(e.key)) {
		const now = performance.now();
		typed = now - typedAt > 500 ? e.key.toLowerCase() : typed + e.key.toLowerCase(); typedAt = now;
		const order = [...rows.slice(i + 1), ...rows.slice(0, i + 1)];
		focus(order.find(r => r.textContent.trim().toLowerCase().startsWith(typed)));
	}
});
/* [ and ]: the previous / next page in the current group; the Lens travels */
document.addEventListener('keydown', (e) => {
	if (e.key !== '[' && e.key !== ']') return;
	if (e.metaKey || e.ctrlKey || e.altKey || e.defaultPrevented) return;
	const t = e.target;
	if (t?.isContentEditable || t?.closest?.('input, textarea, select, [contenteditable], [role=combobox], dialog')) return;
	const at = shellNav.locate(shellNav.current);
	if (!at) return;
	const i = at.siblings.findIndex(p => p.slug === shellNav.current);
	const next = at.siblings[i + (e.key === ']' ? 1 : -1)];
	if (!next) return;
	e.preventDefault();
	const a = tree.querySelector(`#page-list a[href="/${CSS.escape(next.slug)}"]`);
	if (a) { a.closest('details')?.open || a.closest('details') && setOpen(a.closest('details'), true); go(a); }
	else location.href = '/' + next.slug;
});

/* ── THE INSPECTOR: the sidebar's mirror, fused to the sheet's right edge ──
   The same technique as the fold: one spring over the column's visible width
   (0 closed, insp-w − gutter open) writes the inspector's end margin every
   frame, so the sheet's right edge glides; its contents fade as WIDTH CHANGES.
   Below 1100px it floats (CSS); on a phone it is a bottom sheet. */
const insOpen = () => body.classList.contains('insp');
let inspAnim = null, closeT = 0, inspFlying = false;
/* the open column's visible width is its width less the desk's gutter at
   REST (none while folded: the gutter leaves with the panel), so the flight
   lands exactly where the CSS rest state is and never steps 8px at an end */
const inspOpenW = () => px('--insp-w') - (body.classList.contains('deck-folded') ? 0 : px('--deck-gap'));
let IW = 320, IWo = 312;
/* --insp-v: the column's visible width, so its head stays inside the window (L2) */
const inspS = new Spring(1, ([v]) => { if (inspFlying) { insp.style.marginInlineEnd = (Math.max(0, v) - IW).toFixed(2) + 'px'; insp.style.setProperty('--insp-v', Math.min(IWo, Math.max(0, v)).toFixed(1)); } });
const inspInner = insp.querySelector('.detail-inner');
function inspDone() {
	inspFlying = false;
	body.classList.remove('insp-fly');
	insp.style.marginInlineEnd = ''; insp.style.removeProperty('--insp-v');
	reflowing('insp', false);
	dispatchEvent(new Event('deck:inspector'));
}
function slideInspector(commit) {
	const was = insOpen();
	if (innerWidth <= 1100 || !live()) {
		if (inspFlying) { cancelAnimationFrame(inspS.raf); inspS.raf = 0; inspDone(); }
		commit(); replotNow(); dispatchEvent(new Event('deck:inspector')); return;
	}
	if (reduced()) {
		/* reduced motion: the layout changes at once; the column fades (100ms) */
		inspAnim?.cancel();
		commit();
		replotNow();
		const now = insOpen();
		if (now === was) return;
		insp.classList.toggle('deck-leaving', !now);
		inspAnim = insp.animate([{ opacity: now ? 0 : 1 }, { opacity: now ? 1 : 0 }], { duration: 100, easing: 'linear' });
		const a = inspAnim;
		a.finished.then(() => { if (inspAnim === a) { insp.classList.remove('deck-leaving'); inspAnim = null; } }, () => {});
		dispatchEvent(new Event('deck:inspector'));
		return;
	}
	IW = px('--insp-w'); IWo = inspOpenW();
	const cur = inspFlying ? inspS.x[0] : (was ? inspOpenW() : 0);
	commit();
	const now = insOpen();
	if (now === was && !inspFlying) return;
	/* the start width is written in the commit's frame (never a frame at the
	   end width first); a column that was closed starts unseen */
	if (!inspFlying) { inspFlying = true; innerFrom0 = !was; body.classList.add('insp-fly'); inspS.set([cur]); reflowing('insp', true); }
	settleDx = now ? 6 : -6;
	inspS.to([now ? inspOpenW() : 0], now ? SPRING.deck : SPRING.fold, inspDone);
	if (unseen) queuePre();
	dispatchEvent(new Event('deck:inspector'));
}
/* ONE STATE OWNER of body.insp: open = user === 'open' || (user !== 'closed' && a live child in
   #aside); user is the reader's last word (localStorage 'detail', read pre-paint). Content never
   seen clears 'closed'; re-sent content (a pin by id, a card by id and words) never reopens a
   column the reader closed. An empty #aside counts only as the reader's last unpin (pill.js). */
const isCard = (c) => !c.hasAttribute('data-deck-ghost') && !c.hasAttribute('data-deck-leaving');
const hasAside = () => [...aside.children].some(isCard);
const keyOf = (c) => c.dataset.pin ? 'pin|' + c.dataset.pin : (c.id || c.tagName) + '|' + (c.textContent || '').trim().slice(0, 120);
const storedPins = () => { try { return (JSON.parse(localStorage.getItem('shell-pins') || '[]') || []).map(p => p && p.id).filter(Boolean); } catch { return []; } };
const seen = new Set([...[...aside.children].map(keyOf), ...storedPins().map(id => 'pin|' + id)]);
let inspUser = (() => { try { const v = localStorage.getItem('detail'); return v === 'open' || v === 'closed' ? v : null; } catch { return null; } })();
function setUser(v) {
	inspUser = v;
	try { v ? localStorage.setItem('detail', v) : localStorage.removeItem('detail'); } catch {}
}
const wantInsp = () => inspUser === 'open' || (inspUser !== 'closed' && hasAside());
function applyInspector() {
	clearTimeout(closeT);
	const want = wantInsp();
	body.classList.toggle('insp-hand', inspUser === 'open');
	if (want !== insOpen() || (inspFlying && inspS.t[0] !== (want ? inspOpenW() : 0))) slideInspector(() => body.classList.toggle('insp', want));
	dispatchEvent(new Event('deck:inspector-state'));
}
/* the reader: the close button, the titlebar button, Ctrl/⌘ ., Esc, the palette */
function toggleInspector(force) {
	const open = force === undefined ? !insOpen() : !!force;
	setUser(open ? (hasAside() ? null : 'open') : (hasAside() ? 'closed' : null));
	applyInspector();
	return open;
}
const pinInspector = (open) => toggleInspector(open);
/* the content: a removed card dissolves in place first (160ms ghost), then
   the column closes; a card that already left by itself (an unpinned answer
   fades out first, data-no-ghost) leaves nothing to wait for. `why` is
   'unpin' when the reader's own unpin emptied it (pill.js). */
function syncInspector(ghosted = true, why) {
	const live_ = [...aside.children].filter(isCard);
	let fresh = false;
	for (const c of live_) { const k = keyOf(c); if (!seen.has(k)) { seen.add(k); fresh = true; } }
	if (fresh && inspUser === 'closed') setUser(null);
	clearTimeout(closeT);
	if (!live_.length) {
		if (why === 'unpin') { if (inspUser !== null) setUser(null); }
		else {
			/* stored pins not in the column and not leaving: a re-send dropped
			   them and pill.js is putting them back. Hold; if they never come,
			   the column settles a moment later */
			const leaving = new Set([...aside.querySelectorAll(':scope > [data-pin][data-deck-leaving]')].map(c => c.dataset.pin));
			if (storedPins().some(id => !leaving.has(id))) { closeT = setTimeout(applyInspector, 400); return; }
		}
	}
	if (!wantInsp() && insOpen() && ghosted && live() && !reduced()) { closeT = setTimeout(applyInspector, 200); return; }
	applyInspector();
}
new MutationObserver((muts) => syncInspector(muts.some(m => [...m.removedNodes].some(n => n.nodeType === 1 && !n.hasAttribute('data-no-ghost') && !n.hasAttribute('data-deck-ghost')))))
	.observe(aside, { childList: true, characterData: true, subtree: true });

/* ── REGIONS: an emptied child dissolves where it was (a ghost clone) ── */
const REGIONS = ['rail', 'navigation', 'header', 'aside', 'footer', 'layer'].map(id => document.getElementById(id)).filter(Boolean);
const rects = new WeakMap();
const snap = (r) => { for (const c of r.children) rects.set(c, c.getBoundingClientRect()); };
for (const region of REGIONS) {
	snap(region);
	new MutationObserver((muts) => {
		const added = muts.some(m => [...m.addedNodes].some(n => n.nodeType === 1));
		if (added) for (const g of document.querySelectorAll(`[data-deck-ghost="${region.id}"]`)) g.remove();
		if (live()) for (const m of muts) for (const n of m.removedNodes) {
			if (n.nodeType !== 1) continue;
			const rc = rects.get(n);
			if (!rc || !rc.width || n.isConnected || n.classList?.contains('pill-bubble') || n.hasAttribute?.('data-no-ghost')) continue;
			const g = n.cloneNode(true);
			g.removeAttribute('id'); for (const x of g.querySelectorAll('[id]')) x.removeAttribute('id');
			g.setAttribute('data-deck-ghost', region.id); g.setAttribute('aria-hidden', 'true'); g.inert = true;
			Object.assign(g.style, { left: rc.left + 'px', top: rc.top + 'px', width: rc.width + 'px', height: rc.height + 'px' });
			(region.id === 'layer' ? document.body : inset).append(g);
			requestAnimationFrame(() => requestAnimationFrame(() => g.classList.add('gone')));
			setTimeout(() => g.remove(), 220);
		}
		requestAnimationFrame(() => snap(region));
	}).observe(region, { childList: true });
}
addEventListener('resize', () => { for (const r of REGIONS) snap(r); });
document.querySelector('.canvas-scroll')?.addEventListener('scroll', () => snap(document.getElementById('header')), { passive: true });

/* ── THE DOCK: the page's title line travels into the titlebar on scroll ──
   The sticky line reaches the top after the page scrolls its offset D; over
   0 → D a scroll-driven animation (_shell.css, dock-title) carries the h1 to the
   titlebar slot at 15px. The path (D, x/y, scale, max width) is measured at
   rest from layout offsets. Without scroll-driven animations: a transition
   flipped by an IntersectionObserver. Reduced motion: a fade swap. */
const sda = CSS.supports('animation-timeline: scroll()');
if (!sda) html.classList.add('no-sda');
var pageHead = document.querySelector('.sheet .page-head');
var pageMark = document.querySelector('.sheet .page-mark');
var dockP = 0, dockD = 60;
function measureDock() {
	if (!pageHead || !canvasEl || flying.size) return;
	const h1 = pageHead.querySelector('h1'), trig = document.querySelector('.shell-top .deck-trigger'), tools = document.querySelector('.shell-top .shell-actions');
	if (!h1 || !trig || !canvasEl.clientWidth) return;
	/* the title line starts on the content's own left edge */
	const m = mainEl.firstElementChild || mainEl, mr = m.getBoundingClientRect(), sr = canvasEl.getBoundingClientRect();
	const cx = parseFloat(getComputedStyle(canvasEl).paddingLeft) || 0;
	const tx0 = Math.max(0, mr.left - sr.left - cx);
	pageHead.style.setProperty('--head-x', tx0.toFixed(1) + 'px');
	const D = pageMark.offsetTop;
	const fs = parseFloat(getComputedStyle(h1).fontSize) || 26, k = 15 / fs;
	const t = trig.getBoundingClientRect(), tl = tools ? tools.getBoundingClientRect().left : sr.right;
	const h1x = pageHead.offsetLeft + h1.offsetLeft, h1y = h1.offsetTop, h = h1.offsetHeight;
	const x = t.right - sr.left + 6 - h1x;
	const y = t.top + t.height / 2 - sr.top - (h * k) / 2 - h1y;
	const maxw = Math.max(80, (tl - (t.right + 6) - 12) / k);
	dockD = Math.max(1, D);
	const st = pageHead.style;
	st.setProperty('--dock-d', dockD.toFixed(1) + 'px');
	st.setProperty('--dock-x', x.toFixed(2) + 'px');
	st.setProperty('--dock-y', y.toFixed(2) + 'px');
	st.setProperty('--dock-k', k.toFixed(4));
	st.setProperty('--dock-maxw', maxw.toFixed(1) + 'px');
	dockP = clamp(canvasEl.scrollTop / dockD);
	html.classList.add('dock-ready');
}
if (pageHead && canvasEl) {
	measureDock();
	/* the sheet and the titlebar settle a frame after a resize (the phone
	   layout is applied by rt-sidebar), so the path is re-measured whenever
	   either box changes size, once per frame */
	let dockRaf = 0;
	const remeasure = () => { if (!dockRaf) dockRaf = requestAnimationFrame(() => { dockRaf = 0; measureDock(); }); };
	addEventListener('resize', remeasure);
	new ResizeObserver(remeasure).observe(sheetEl);
	const topEl = document.querySelector('.shell-top'); if (topEl) new ResizeObserver(remeasure).observe(topEl);
	addEventListener('deck:width', measureDock);
	document.fonts?.ready.then(measureDock);
	/* a personality can change the title's face (Folio sets it in Libron):
	   re-measure once the new face is in */
	document.addEventListener('shell:theme', () => { remeasure(); document.fonts?.ready.then(remeasure); });
	document.fonts?.addEventListener?.('loadingdone', remeasure);
	canvasEl.addEventListener('scroll', () => { dockP = clamp(canvasEl.scrollTop / dockD); }, { passive: true });
	if (!sda && 'IntersectionObserver' in window) {
		new IntersectionObserver(([e]) => { sheetEl.classList.toggle('docked', !e.isIntersecting && e.boundingClientRect.top < e.rootBounds.top + 1); },
			{ root: canvasEl, threshold: 0 }).observe(pageMark);
	}
}

/* ── the sheet's scroll edges, the bar's width, and the tab-hidden pause ── */
{
	const sc = document.querySelector('.sheet .canvas-scroll'), sheet = document.querySelector('.sheet');
	const edge = () => {
		if (flying.size) return;   /* re-measured at rest (deck:width) */
		sheet.classList.toggle('scrolled', sc.scrollTop > 4);
		sheet.classList.toggle('overflows', sc.scrollHeight - sc.clientHeight - sc.scrollTop > 4);
	};
	sc?.addEventListener('scroll', edge, { passive: true }); edge();
	new ResizeObserver(edge).observe(sc);
	addEventListener('deck:width', edge);
	new MutationObserver(() => requestAnimationFrame(edge)).observe(document.getElementById('main'), { childList: true, subtree: true });
	const vis = () => html.classList.toggle('tab-hidden', document.hidden);
	document.addEventListener('visibilitychange', vis); vis();
}

/* ── TOUCH: the drawer follows the finger; the bottom sheet rests at detents;
   toasts swipe away. Release velocity decides; overshoot only after a throw ── */
function tracker() {
	const pts = [];
	return {
		add(x, y) { const t = performance.now(); pts.push([x, y, t]); while (pts.length > 2 && t - pts[0][2] > 100) pts.shift(); },
		v() { if (pts.length < 2) return [0, 0]; const a = pts[0], b = pts.at(-1), dt = (b[2] - a[2]) / 1000 || 1; return [(b[0] - a[0]) / dt, (b[1] - a[1]) / dt]; },
	};
}
const project = (v, rate = 0.998) => (v / 1000) * rate / (1 - rate);
const rubber = (x, lim) => (x <= 0 ? x : lim * (1 - 1 / (x / lim + 1)));
/* a press that starts in a dialog, a popover or a field belongs to it, never to a
   swipe: a sideways drag in a confirm must not carry it off. While a modal is
   open the page under it is inert, so no swipe starts there; only a toast (its
   <rt-toaster> is a manual popover in the top layer, above the modal) still
   swipes away. */
const NO_SWIPE = 'dialog, [role=dialog], [role=alertdialog], [aria-modal=true], [popover]:not(rt-toaster), input, textarea, select, [contenteditable]:not([contenteditable=false])';
const modalOpen = () => { try { return !!document.querySelector('dialog:modal'); } catch { return !!document.querySelector('dialog[open][aria-modal=true]'); } };
const noSwipe = (e) => !!e.target.closest?.(NO_SWIPE) || (modalOpen() && !e.target.closest?.('rt-toast'));

/* the drawer: an edge swipe opens it, a drag left closes it */
let dr = null;
document.addEventListener('pointerdown', (e) => {
	if (!mobile() || (e.pointerType === 'mouse' && e.button !== 0) || noSwipe(e)) return;
	const open = sb.expanded, w = nav.offsetWidth;
	if (!open && e.clientX < 20) dr = { x0: e.clientX, base: -w - 72, w, open, tr: tracker(), on: false };
	else if (open && nav.contains(e.target)) dr = { x0: e.clientX, base: 0, w, open, tr: tracker(), on: false };
});
document.addEventListener('pointermove', (e) => {
	if (!dr) return;
	if (drawerS.raf && !dr.on) { cancelAnimationFrame(drawerS.raf); drawerS.raf = 0; dr.base = parseFloat(nav.style.translate) || dr.base; }
	const dx = e.clientX - dr.x0;
	dr.tr.add(e.clientX, e.clientY);
	if (!dr.on) { if (Math.abs(dx) < 10) return; dr.on = true; nav.style.transition = 'none'; }
	let x = dr.base + dx;
	x = x > 0 ? rubber(x, 40) : Math.max(x, -dr.w - 72);
	nav.style.translate = `${x}px 0`;
});
/* release: the target comes from the PROJECTED position, and the spring
   starts at the finger's velocity (spring.throw, the only bounce we allow) */
const drawerS = new Spring(1, ([x]) => { nav.style.translate = `${x}px 0`; });
const endDrawer = (e) => {
	if (!dr) return;
	const d = dr; dr = null;
	if (!d.on) return;
	const x = parseFloat(nav.style.translate) || 0;
	const vx = d.tr.v()[0];
	const shouldOpen = x + project(vx) > -d.w * 0.4 && vx > -300;
	if (shouldOpen !== d.open) shouldOpen ? sb.open() : sb.close();
	e?.preventDefault?.();
	if (reduced()) { nav.style.transition = ''; nav.style.translate = ''; return; }
	nav.style.transition = 'none';
	drawerS.set([x]);
	drawerS.to([shouldOpen ? 0 : -d.w - 72], SPRING.throw, () => { nav.style.translate = ''; nav.style.transition = ''; }, [vx]);
};
document.addEventListener('pointerup', endDrawer);
document.addEventListener('pointercancel', endDrawer);

/* the bottom-sheet inspector (phones): detents at 40% and 88% of the screen */
{
	const grab = insp.querySelector('.deck-grab');
	let bs = null;
	const H = () => insp.offsetHeight;
	const restY = () => { const v = getComputedStyle(insp).getPropertyValue('--sheet-rest').trim(); return v === '0px' || v === '0' ? 0 : H() - innerHeight * 0.4; };
	grab?.addEventListener('pointerdown', (e) => {
		if (innerWidth > 767 || modalOpen()) return;
		bs = { y0: e.clientY, base: restY(), tr: tracker(), on: false };
		grab.setPointerCapture(e.pointerId);
	});
	grab?.addEventListener('pointermove', (e) => {
		if (!bs) return;
		const dy = e.clientY - bs.y0; bs.tr.add(e.clientX, e.clientY);
		if (!bs.on) { if (Math.abs(dy) < 4) return; bs.on = true; insp.classList.add('deck-dragging'); }
		let y = bs.base + dy;
		if (y < 0) y = -rubber(-y, 30);
		insp.style.translate = `0 ${y}px`;
	});
	const end = () => {
		if (!bs) return;
		const b = bs; bs = null;
		insp.classList.remove('deck-dragging');
		const y = parseFloat((insp.style.translate || '0 0').split(' ')[1]) || 0;
		insp.style.translate = '';
		if (!b.on) { insp.style.setProperty('--sheet-rest', b.base === 0 ? '' : '0px'); return; }
		const proj = y + project(b.tr.v()[1]);
		const detents = [0, H() - innerHeight * 0.4, H() + 24];
		const best = detents.reduce((p, c) => Math.abs(c - proj) < Math.abs(p - proj) ? c : p);
		if (best === detents[2]) { insp.style.setProperty('--sheet-rest', ''); toggleInspector(false); }
		else insp.style.setProperty('--sheet-rest', best === 0 ? '0px' : '');
	};
	grab?.addEventListener('pointerup', end);
	grab?.addEventListener('pointercancel', end);
}

/* toasts in #layer: swipe right to dismiss, spring back otherwise. Only a notice
   swipes: never a dialog or popover parked in #layer (noSwipe), and inside an
   <rt-toaster> the one toast, never the stack itself. */
{
	const layer = document.getElementById('layer');
	let tz = null;
	layer.addEventListener('pointerdown', (e) => {
		if ((e.pointerType === 'mouse' && e.button !== 0) || noSwipe(e)) return;
		const t = e.target.closest('rt-toaster > rt-toast, #layer > *');
		if (!t || t.localName === 'rt-toaster' || e.target.closest('a, button, input')) return;
		tz = { t, x0: e.clientX, tr: tracker(), on: false };
		t.setPointerCapture(e.pointerId);
	});
	layer.addEventListener('pointermove', (e) => {
		if (!tz) return;
		const dx = e.clientX - tz.x0; tz.tr.add(e.clientX, e.clientY);
		if (!tz.on) { if (Math.abs(dx) < 6) return; tz.on = true; tz.t.style.transition = 'none'; }
		tz.t.style.translate = `${dx > 0 ? dx : -rubber(-dx, 24)}px 0`;
	});
	const end = () => {
		if (!tz) return;
		const z = tz; tz = null;
		if (!z.on) return;
		const x = parseFloat(z.t.style.translate) || 0, vx = z.tr.v()[0];
		z.t.style.transition = '';
		if (x > z.t.offsetWidth * 0.4 || vx > 400) {
			z.t.animate([{ translate: `${x}px 0`, opacity: 1 }, { translate: `${z.t.offsetWidth + 40}px 0`, opacity: 0 }], { duration: 160, easing: EASE_LEAVE, fill: 'forwards' })
				.finished.then(() => z.t.remove(), () => {});
		} else {
			z.t.style.translate = '';
			z.t.animate([{ translate: `${x}px 0` }, { translate: '0 0' }], { duration: 320, easing: 'cubic-bezier(0.34, 1.3, 0.64, 1)' });
		}
	};
	layer.addEventListener('pointerup', end);
	layer.addEventListener('pointercancel', end);
}

/* keep both tabs true to layout: fonts, the sidebar's upgrade, resizes */
function settle() { placeRibbon(); syncFold(); lens(true); reribbon(true); }
/* re-aim both tabs at rest whenever anything that places them changes: the
   font swap, the tree build, a resize, the library flipping the layout
   (mobile ↔ desktop) or the state, the colour scheme. One rAF coalesces. */
let reaimT = 0;
function reaim() {
	if (reaimT) return;
	reaimT = requestAnimationFrame(() => {
		reaimT = 0;
		if (holding) return;
		placeRibbon(); syncFold(); lens(true); reribbon(true);
	});
}
document.fonts?.ready.then(reaim);
addEventListener('resize', reaim);
/* only a real change re-aims: re-setting an attribute to the value it already
   has still queues a record, and settle() itself re-sets data-panel, so an
   unguarded observer kept a 60 fps re-aim loop running at idle */
const changed = (muts) => muts.some(m => m.oldValue !== m.target.getAttribute(m.attributeName));
new MutationObserver((m) => { if (changed(m)) reaim(); }).observe(sb, { attributes: true, attributeFilter: ['data-layout', 'data-state'], attributeOldValue: true });
new ResizeObserver(reaim).observe(nav);
{
	let watched = null;
	const ro = new ResizeObserver(reaim);
	const watchRow = () => {
		const row = nav.querySelector('.panel[data-active] .panel-scroll [aria-current="page"]');
		if (row === watched) return;
		if (watched) ro.unobserve(watched);
		watched = row; if (row) ro.observe(row);
	};
	watchRow();
	new MutationObserver((m) => { if (changed(m)) { watchRow(); reaim(); } }).observe(nav, { attributes: true, attributeFilter: ['data-panel'], attributeOldValue: true });
}
matchMedia('(min-width: 768px)').addEventListener('change', reaim);
matchMedia('(prefers-color-scheme: dark)').addEventListener('change', reaim);
/* while the page arrives (and until rt-chart upgrades) charts draw their final
   shape at once; after the first second a live redraw tweens again */
Promise.all([new Promise((r) => setTimeout(r, 900)), document.querySelector('rt-chart') ? customElements.whenDefined('rt-chart') : null])
	.then(() => requestAnimationFrame(() => html.classList.add('deck-settled')));
customElements.whenDefined('rt-sidebar').then(() => requestAnimationFrame(settle));
settle();

/* charts never paint empty, never in two passes. rt-chart upgrades every chart
   in one pass several hundred ms after the data is bound. Until then each chart
   draws its FINAL picture from its `data` attribute before the first paint, with
   the library's own geometry and classes; the upgrade lands on the identical
   picture in the same task and the preview leaves before the next paint. */
if (!customElements.get('rt-chart')) {
	const r2 = (v) => Math.round(v * 100) / 100;
	const esc = (s) => String(s).replace(/[&<>"']/g, (ch) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[ch]);
	const clamp = (v, a, b) => Math.min(b, Math.max(a, v));
	const pal = (i) => `var(--rt-chart-${(((i % 5) + 5) % 5) + 1})`;
	const colourOf = (c, i) => { const m = /^(?:--rt-)?(?:chart-)?([1-5])$/.exec(String(c ?? '').trim()); return m ? pal(+m[1] - 1) : pal(i); };
	const lin = (p) => p.map(([x, y], i) => `${i ? 'L' : 'M'}${r2(x)} ${r2(y)}`).join('');
	const stepped = (p) => { let s = `M${r2(p[0][0])} ${r2(p[0][1])}`; for (let i = 1; i < p.length; i++) { const m = (p[i - 1][0] + p[i][0]) / 2; s += `H${r2(m)}V${r2(p[i][1])}H${r2(p[i][0])}`; } return s; };
	const mono = (p) => {
		const n = p.length; if (n < 3) return lin(p);
		const dx = [], sl = [];
		for (let i = 0; i < n - 1; i++) { dx[i] = p[i + 1][0] - p[i][0] || 1e-9; sl[i] = (p[i + 1][1] - p[i][1]) / dx[i]; }
		const t = [sl[0]];
		for (let i = 1; i < n - 1; i++) t[i] = sl[i - 1] * sl[i] <= 0 ? 0 : (3 * (dx[i - 1] + dx[i])) / ((2 * dx[i] + dx[i - 1]) / sl[i - 1] + (dx[i] + 2 * dx[i - 1]) / sl[i]);
		t[n - 1] = sl[n - 2];
		let s = `M${r2(p[0][0])} ${r2(p[0][1])}`;
		for (let i = 0; i < n - 1; i++) { const g = dx[i] / 3; s += `C${r2(p[i][0] + g)} ${r2(p[i][1] + t[i] * g)} ${r2(p[i + 1][0] - g)} ${r2(p[i + 1][1] - t[i + 1] * g)} ${r2(p[i + 1][0])} ${r2(p[i + 1][1])}`; }
		return s;
	};
	const curve = (k, p) => (k === 'linear' ? lin(p) : k === 'step' ? stepped(p) : mono(p));
	const niceTicks = (lo, hi, count, integer) => {
		if (lo === hi) { if (lo === 0) return integer ? { lo: 0, hi: 1, ticks: [0, 1] } : { lo: 0, hi: 1, ticks: [0, 0.25, 0.5, 0.75, 1] }; lo = Math.min(0, lo); hi = Math.max(0, hi); }
		const raw = (hi - lo) / Math.max(1, count), mag = 10 ** Math.floor(Math.log10(raw)), f = raw / mag;
		let st = (f >= 7.5 ? 10 : f >= 3.5 ? 5 : f >= 1.5 ? 2 : 1) * mag; if (integer && st < 1) st = 1;
		const a = Math.floor(lo / st + 1e-9) * st, b = Math.ceil(hi / st - 1e-9) * st, ticks = [];
		for (let v = a; v <= b + st / 2; v += st) ticks.push(Math.abs(v) < st / 1e6 ? 0 : +v.toPrecision(12));
		return { lo: a, hi: b, ticks };
	};
	const arc = (cx, cy, ro, ri, a0, a1) => {
		if (a1 - a0 <= 1e-6) return '';
		if (a1 - a0 >= Math.PI * 2 - 1e-6) a1 = a0 + Math.PI * 2 - 1e-4;
		const pt = (r, a) => `${r2(cx + r * Math.sin(a))} ${r2(cy - r * Math.cos(a))}`, big = a1 - a0 > Math.PI ? 1 : 0;
		return `M${pt(ro, a0)}A${r2(ro)} ${r2(ro)} 0 ${big} 1 ${pt(ro, a1)}` + (ri > 0 ? `L${pt(ri, a1)}A${r2(ri)} ${r2(ri)} 0 ${big} 0 ${pt(ri, a0)}Z` : `L${r2(cx)} ${r2(cy)}Z`);
	};
	const compactFmt = (c) => {
		let o = {}; try { o = JSON.parse(c.getAttribute('format') || '{}') || {}; } catch {}
		const keep = {}; for (const k of ['style', 'currency', 'currencyDisplay', 'unit', 'unitDisplay']) if (k in o) keep[k] = o[k];
		try { const f = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: o.style === 'percent' ? 0 : 1, ...keep }); return (v) => f.format(v); }
		catch { const f = new Intl.NumberFormat(undefined, { notation: 'compact', maximumFractionDigits: 1 }); return (v) => f.format(v); }
	};
	let ctx2d = null;
	const measure = (font) => (s) => { ctx2d ??= document.createElement('canvas').getContext('2d'); ctx2d.font = font; return ctx2d.measureText(String(s)).width; };
	const parse = (raw) => {
		let d; try { d = JSON.parse(raw || ''); } catch { return null; }
		let labels = [], series = [];
		if (Array.isArray(d)) series = [{ name: 'Value', values: d }];
		else if (d && typeof d === 'object') { labels = Array.isArray(d.labels) ? d.labels : []; series = Array.isArray(d.series) ? d.series : Array.isArray(d.values) ? [{ name: d.name ?? 'Value', values: d.values }] : []; }
		const seen = new Set();
		series = series.filter((s) => s && typeof s === 'object').map((s, i) => {
			let name = String(s.name ?? `Series ${i + 1}`); while (seen.has(name)) name += '′'; seen.add(name);
			return { name, values: (Array.isArray(s.values) ? s.values : []).map((v) => { const n = typeof v === 'number' ? v : parseFloat(v); return Number.isFinite(n) ? n : 0; }), color: colourOf(s.color, i) };
		});
		const n = Math.max(labels.length, ...series.map((s) => s.values.length), 0);
		labels = Array.from({ length: n }, (_, i) => (labels[i] == null ? String(i + 1) : String(labels[i])));
		for (const s of series) while (s.values.length < n) s.values.push(0);
		return { labels, series };
	};
	const flag = (c, k, dflt = false) => (c.hasAttribute(k) ? c.getAttribute(k) !== 'false' : dflt);

	/* the cartesian picture (sparkline, line, area): the library's own plot */
	const cartesian = (c, d, W, H) => {
		const type = c.getAttribute('type'), spark = type === 'sparkline', fmt = compactFmt(c);
		const stacked = flag(c, 'stacked') && type !== 'line' && type !== 'sparkline', grid = flag(c, 'grid', true), crv = c.getAttribute('curve');
		let lo = 0, hi = 0; const pos = new Array(d.labels.length).fill(0), neg = pos.slice();
		for (const s of d.series) s.values.forEach((v, i) => { if (stacked) v >= 0 ? (pos[i] += v) : (neg[i] += v); else { lo = Math.min(lo, v); hi = Math.max(hi, v); } });
		if (stacked) { lo = Math.min(0, ...neg); hi = Math.max(0, ...pos); }
		let sc;
		if (spark) { const all = d.series.flatMap((s) => s.values); lo = Math.min(...all); hi = Math.max(...all); const pad = (hi - lo || Math.abs(hi) || 1) * 0.12; sc = { lo: lo - pad, hi: hi + pad, ticks: [] }; }
		else sc = niceTicks(lo, hi, H < 160 ? 3 : 4, d.series.every((s) => s.values.every(Number.isInteger)));
		const tickTxt = sc.ticks.map(fmt), w11 = measure(`11px ${getComputedStyle(c).fontFamily}`);
		const L = spark ? 3 : Math.ceil(Math.max(0, ...tickTxt.map(w11))) + 12, R = spark ? 3 : 10, T = spark ? 4 : 10, B = spark ? 4 : 26;
		const iw = Math.max(1, W - L - R), ih = Math.max(1, H - T - B), n = d.labels.length;
		const Y = (v) => T + ih * (1 - (v - sc.lo) / (sc.hi - sc.lo || 1)), y0 = Y(clamp(0, sc.lo, sc.hi));
		const inset = spark ? 3 : 8, X = (i) => (n === 1 ? L + iw / 2 : L + inset + ((iw - 2 * inset) * i) / (n - 1));
		let axes = '';
		if (!spark) {
			sc.ticks.forEach((v, i) => {
				const y = Math.round(Y(v)) + 0.5;
				if (grid) axes += `<line class="rt-chart-grid${v === 0 ? ' is-zero' : ''}" x1="${L}" x2="${W - R}" y1="${y}" y2="${y}"/>`;
				axes += `<text class="rt-chart-tick" x="${L - 10}" y="${y}" text-anchor="end" dominant-baseline="middle">${esc(tickTxt[i])}</text>`;
			});
			if (!grid) axes += `<line class="rt-chart-grid is-zero" x1="${L}" x2="${W - R}" y1="${Math.round(y0) + 0.5}" y2="${Math.round(y0) + 0.5}"/>`;
			const lw = Math.max(1, ...d.labels.map(w11)), gap = (iw - 2 * inset) / Math.max(1, n - 1), every = Math.max(1, Math.ceil((lw + 14) / Math.max(1, gap)));
			d.labels.forEach((l, i) => {
				const tail = i === n - 1 && (n - 1) % every >= Math.ceil(every / 2);
				if (i % every && !tail) return;
				const tw = w11(l);
				axes += `<text class="rt-chart-tick" x="${clamp(X(i), tw / 2, W - tw / 2)}" y="${H - 8}" text-anchor="middle">${esc(l)}</text>`;
			});
		}
		const fill = type === 'area' || (spark && !flag(c, 'stacked') && d.series.length === 1), stackArea = stacked && type === 'area';
		const base = new Array(n).fill(0); let defs = '', marks = '', dots = '';
		d.series.forEach((s, si) => {
			const below = base.slice(), vals = s.values.map((v, i) => (stackArea ? base[i] + v : v));
			if (stackArea) vals.forEach((v, i) => (base[i] = v));
			const pts = vals.map((v, i) => [X(i), Y(v)]);
			if (fill) {
				const id = `${c.id || 'deck-chart'}-pg${si}`;
				defs += `<linearGradient id="${id}" x1="0" y1="0" x2="0" y2="1"><stop offset="0" style="stop-color:${s.color};stop-opacity:${spark ? 0.28 : 0.34}"/><stop offset="1" style="stop-color:${s.color};stop-opacity:0.02"/></linearGradient>`;
				const floor = stackArea ? below.map((v, i) => [X(i), Y(v)]) : pts.map(([x]) => [x, y0]);
				const close = stackArea ? curve(crv, [...floor].reverse()).replace(/^M/, 'L') : `L${floor.at(-1)[0]} ${floor[0][1]}L${floor[0][0]} ${floor[0][1]}`;
				marks += `<path class="rt-chart-area" d="${curve(crv, pts)}${close}Z" fill="url(#${id})" opacity="1"/>`;
			}
			marks += `<path class="rt-chart-line${spark ? ' is-spark' : ''}" style="--_c:${s.color}" d="${curve(crv, pts)}" opacity="1"/>`;
			if (spark && pts.length) dots += `<circle class="rt-chart-dot is-end" style="--_c:${s.color}" cx="${pts.at(-1)[0]}" cy="${pts.at(-1)[1]}" r="3"/>`;
		});
		return `<defs>${defs}</defs>${axes}${marks}${dots}`;
	};
	/* the radial picture (donut, pie): ring, slices, centre total */
	const radial = (c, d, W, H) => {
		const donut = c.getAttribute('type') === 'donut', fmt = compactFmt(c);
		const cx = W / 2, cy = H / 2, ro = Math.min(W, H) / 2 - 8, ri = donut ? ro * 0.64 : 0;
		const vals = d.labels.map((_, i) => Math.max(0, d.series[0]?.values[i] ?? 0)), total = vals.reduce((a, b) => a + b, 0);
		let s = `<circle class="rt-chart-track" cx="${cx}" cy="${cy}" r="${(ro + ri) / 2}" stroke-width="${ro - ri || ro}"/>`;
		if (total > 0) { let a = 0; vals.forEach((v, i) => { const k = (v / total) * Math.PI * 2, p = arc(cx, cy, ro, ri, a, a + k); if (p) s += `<path class="rt-chart-slice" data-i="${i}" style="--_c:${pal(i)}" d="${p}"/>`; a += k; }); }
		if (donut) {
			const fs = clamp(ri * 0.46, 14, 40);
			s += `<text class="rt-chart-total" x="${cx}" y="${cy - fs * 0.12}" text-anchor="middle" dominant-baseline="middle" style="font-size:${fs.toFixed(1)}px">${esc(fmt(total))}</text>`;
			s += `<text class="rt-chart-total-label" x="${cx}" y="${cy + fs * 0.62}" text-anchor="middle" dominant-baseline="middle">${esc(c.getAttribute('center-label') || 'Total')}</text>`;
		}
		return s;
	};
	const paint = (c) => {
		if (c.matches(':defined')) return;
		const d = parse(c.getAttribute('data'));
		const old = c.querySelector(':scope > .deck-chart-preview');
		if (!d || !d.series.length || !d.labels.length) { old?.remove(); return; }
		const W = c.clientWidth; if (!W) return;
		const type = c.getAttribute('type') || 'line', round = /^(donut|pie)$/.test(type);
		const H = +c.getAttribute('height') || (type === 'sparkline' ? 48 : round ? 220 : 240);
		const sig = `${W}|${H}|${type}|${c.getAttribute('data')}`;
		if (old && old.dataset.sig === sig) return;
		const body = round ? radial(c, d, W, H) : cartesian(c, d, W, H);
		const names = round ? d.labels : d.series.map((s) => s.name);
		const legend = flag(c, 'legend') ? `<div class="rt-chart-legend">${names.map((nm, i) => `<button type="button" class="rt-chart-legend-item" style="--_c:${round ? pal(i) : d.series[i].color}"><span class="rt-chart-swatch"></span><span>${esc(nm)}</span></button>`).join('')}</div>` : '';
		const box = old || document.createElement('div');
		box.className = 'deck-chart-preview'; box.inert = true; box.setAttribute('aria-hidden', 'true'); box.dataset.sig = sig;
		box.innerHTML = `<div class="rt-chart-plot" style="block-size:${H}px"><svg class="rt-chart-svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}">${body}</svg></div>${legend}`;
		if (!old) c.append(box);
		if (type !== 'sparkline') declutter(c);
	};
	const all = () => document.querySelectorAll('rt-chart:not(:defined)[data]').forEach(paint);
	const mo = new MutationObserver((muts) => {
		let added = false;
		for (const m of muts) {
			if (m.type === 'attributes') { if (m.target.localName === 'rt-chart') paint(m.target); }
			else if (!m.target.closest?.('rt-chart')) added = true;
		}
		if (added) all();
	});
	mo.observe(document.documentElement, { subtree: true, attributes: true, attributeFilter: ['data'], childList: true });
	all();
	/* the upgrade paints the same picture in the same task: drop the preview
	   before that task ends, so the next frame shows only the real chart */
	customElements.whenDefined('rt-chart').then(() => {
		mo.disconnect();
		document.querySelectorAll('rt-chart > .deck-chart-preview').forEach((p) => p.remove());
	});
}

/* x-axis labels: decluttered after every plot (declutter, above), in the same
   task as the plot: a MutationObserver runs before the frame is painted */
{
	const seen = new WeakSet();
	const mo = new MutationObserver((muts) => {
		/* every chart that re-plotted in this task, read together, then written */
		declutterAll([...new Set(muts.map(m => m.target.closest?.('rt-chart')).filter(Boolean))]);
	});
	const watch = () => document.querySelectorAll('rt-chart').forEach(c => {
		if (seen.has(c)) return; seen.add(c);
		mo.observe(c, { childList: true, subtree: true });
		if (c.querySelector('svg')) declutter(c);
	});
	watch();
	if (mainEl) new MutationObserver(watch).observe(mainEl, { childList: true, subtree: true });
}

/* how far the inspector's contents sit right of their rest (a pin aims at the rest) */
const inspShift = () => inspFlying ? IWo - Math.max(0, inspS.x[0]) : 0;
window.deck = { hurry, busy, idle, landed, reveal, flying: () => flying.size > 0, toggleSidebar, openPanel, switchPanel, pinInspector, toggleInspector, syncInspector, go, inspShift, lens: (instant) => { lens(instant); reribbon(instant); } };

