// pill.js — the AI pill in the sheet's bar, and the shell's command centre.
// Ported from the critique-pill shell; here the pill lives inside the sheet
// (centred in its bar, measured against the bar, not the viewport), Ctrl/⌘ K
// grows its palette pane upward out of it, and an answer can be pinned: it
// flies into the inspector fused to the sheet's right edge.
//
// Levels: 0 rest (a line) · 1 open (prompt + tools) · 2 a tool's panel.
// Every movement is a spring on transform/opacity/filter, retargeted (never
// restarted) when interrupted, so velocity carries through. Width changes are
// FLIP: the layout changes at once, then each part springs from where it was
// seen to where it now belongs; the ground is two clipped halves that spring
// apart, so the round ends never stretch. The loop sleeps when nothing moves
// and in a hidden tab. Reduced motion: no travel, only short fades.
//
// The prompt is a <textarea> (#pill-input). At one line the pill is as it
// always was; when the text needs a second line it turns multiline (.is-multi):
// the text on top at the full width, the tools in a bar under it, the pill
// growing UPWARD (its bottom stays on the bar) to min(240px, 40vh), then the
// text scrolls. Enter sends, Shift+Enter is a new line, IME composition never
// sends. The ground's height is a spring like its width.
//
// Events in:  document 'pill:focus'  ·  window 'pill:prompt' {detail: text}
// API:        window.pill = { open(), close(), focus(), prompt(text), command(), rebuild(), level, agent }
//
// The agent slot: with #pill[data-agent="<capability id>"] the mode dot is a
// session chip painted from the signal $pillAgent, a prompt goes to that
// capability (POST /api/capabilities) instead of the demo reply, and the
// capability answers by Caller patches into #pill-console (Patch Region
// console). See "THE AGENT SLOT" below. Without data-agent the demo is as it was.

const $ = (s, r = document) => r.querySelector(s)
const $$ = (s, r = document) => [...r.querySelectorAll(s)]
const pill = $('#pill'), row = $('#pill-row'), rest = $('#pill-rest'), input = $('#pill-input')
const panel = $('#pill-panel'), bgEl = $('.pill-bg'), send = $('.pill-send'), att = $('#pill-att')
const layer = $('#layer'), body = document.body, bar = $('#region-status')
const cmdChip = $('[data-tool="command"]'), cmdList = $('#pill-cmd-list')
if (!pill) throw new Error('pill: #pill missing')

const reduced = matchMedia('(prefers-reduced-motion: reduce)')
/* "narrow" follows the BAR (the sheet), not the viewport: an open inspector
   or a wide sidebar narrows the sheet without touching the window */
const narrow = { matches: false }
const crowdEdge = () => { const r = pill.getBoundingClientRect(); return { cx: r.left + r.width / 2, left: r.left, right: r.right } }
const finePointer = matchMedia('(hover: hover) and (pointer: fine)')
const num = (name, d) => { const v = parseFloat(getComputedStyle(body).getPropertyValue(name)); return Number.isFinite(v) ? v : d }

/* ── springs ───────────────────────────────────────────────────────── */
const active = new Set(), dirty = new Set()
let raf = 0, last = 0
const K = () => num('--pill-k', 520), C = () => num('--pill-c', 38)
class Spring {
	constructor(x, owner, eps = 0.05) { this.x = x; this.t = x; this.v = 0; this.owner = owner; this.eps = eps; this.k = 0; this.c = 0 }
	to(t, { k, c, from, v } = {}) {
		if (from !== undefined) this.x = from
		if (v !== undefined) this.v = v
		this.t = t; this.k = k ?? K(); this.c = c ?? C()
		if (reduced.matches) { if (!this.fade) return this.jump(t); this.k = 1600; this.c = 80 }   // reduced: no travel, a short fade (about 120ms, critically damped)
		if (Math.abs(this.x - t) < this.eps && Math.abs(this.v) < this.eps) return this.jump(t)
		active.add(this); dirty.add(this.owner); wake(); return this
	}
	jump(x) { this.x = this.t = x; this.v = 0; active.delete(this); dirty.add(this.owner); wake(); return this }
	step(h) { const a = -this.k * (this.x - this.t) - this.c * this.v; this.v += a * h; this.x += this.v * h }
	get done() { return Math.abs(this.v) < this.eps * 4 && Math.abs(this.x - this.t) < this.eps }
}
// the clock starts on the first painted frame (the layout work between the
// trigger and that frame is setup, not motion), so nothing leaps on frame one
function wake() { if (!raf && !document.hidden) { last = -1; raf = requestAnimationFrame(frame) } }
function frame(now) {
	raf = 0
	const dt = last < 0 ? 1 / 60 : Math.min(0.034, Math.max(0.001, (now - last) / 1000)); last = now
	const n = Math.ceil(dt / 0.004), h = dt / n
	for (const s of [...active]) {
		for (let i = 0; i < n; i++) s.step(h)
		if (s.done) { s.x = s.t; s.v = 0; active.delete(s); s.owner.rested?.() }
		dirty.add(s.owner)
	}
	for (const o of dirty) o.render()
	dirty.clear()
	if (active.size) raf = requestAnimationFrame(frame)
}
document.addEventListener('visibilitychange', () => {
	document.documentElement.classList.toggle('pv-hidden', document.hidden)
	if (document.hidden) { cancelAnimationFrame(raf); raf = 0; for (const s of [...active]) s.jump(s.t) } else wake()
})

/* one element's transform/opacity/filter, each its own spring */
class Mover {
	constructor(el) {
		this.el = el
		this.x = new Spring(0, this); this.y = new Spring(0, this)
		this.s = new Spring(1, this, 0.001); this.o = new Spring(1, this, 0.002); this.o.fade = true; this.b = new Spring(0, this, 0.05)
	}
	render() {
		const st = this.el.style, x = this.x.x, y = this.y.x, s = this.s.x, o = this.o.x, b = this.b.x
		st.translate = Math.abs(x) > 0.01 || Math.abs(y) > 0.01 ? `${x.toFixed(2)}px ${y.toFixed(2)}px` : ''
		st.scale = Math.abs(s - 1) > 0.0005 ? s.toFixed(4) : ''
		st.opacity = Math.max(0, Math.min(1, o)).toFixed(3)   // always: some parts rest at opacity 0 in CSS
		st.filter = b > 0.1 && !reduced.matches ? `blur(${b.toFixed(2)}px)` : ''
		this.onRender?.()
	}
	rested() { if (![this.x, this.y, this.s, this.o, this.b].some(s => active.has(s))) this.onRest?.() }
}
const movers = new WeakMap()
const M = el => { let m = movers.get(el); if (!m) { m = new Mover(el); movers.set(el, m) } return m }

/* the ground: two halves at ±W/2 around the centre */
const halves = $$('.pill-half', bgEl)
const pillH = () => num('--pill-h', 40)
const ground = {
	w: null, g: null, sh: null, h: null,
	init() {
		this.w = new Spring(44, this, 0.1)
		this.sh = new Spring(0, this, 0.05)
		this.h = new Spring(pillH(), this, 0.1)   // the ground's height: grows upward with a multiline prompt
		this.g = M(bgEl); this.g.o.jump(0); this.g.s.jump(0.9)
		this.g.onRender = () => this.render()   // the clip follows the ground's scale too
	},
	render() {
		const w = Math.max(this.w.x, 0), sh = this.sh.x
				halves[0].style.translate = `${(sh - w / 2).toFixed(2)}px 0`
				halves[1].style.translate = `${(sh + w / 2).toFixed(2)}px 0`
				// the ground's height (the row's top clip follows it: pill.css)
				pill.style.setProperty('--pill-gh', Math.max(this.h.x, 0).toFixed(1) + 'px')
		// the row's clip follows the ground's centre, wherever the row itself sits
		pill.style.setProperty('--gdx', (sh - rowShift).toFixed(2) + 'px')
		// the row is clipped to this width (CSS), so nothing renders outside the ground
		const seen = w * (this.g ? this.g.s.x : 1)
		pill.style.setProperty('--gw', (level ? seen + 2 : seen).toFixed(1) + 'px')
		// the palette pane's sides ride the pill's live width: the list grows
		// up out of the pill's top edge as ONE object, never wider than the
		// field under it (its clip's side insets, read by the clip animations)
		if (panel.dataset.pane === 'command' && cmdW) {
			const d = (cmdW - seen) / 2
			panel.style.setProperty('--cmd-x', (d > 0.5 ? d : -48).toFixed(1) + 'px')
			if (cmdRise && d < 28) cmdRise()
		}
	},
}
ground.init()

/* ── FLIP over the row's visible parts ────────────────────────────── */
const parts = () => [...row.children].filter(el => el.getClientRects().length && !el.classList.contains('sr-only'))
function snapshot() { const m = new Map(); for (const el of parts()) m.set(el, el.getBoundingClientRect()); return m }
let entering = 0
function play(before, { fan = false, dir = 0 } = {}) {
	const now = parts(), cx = crowdEdge().cx
	const k = K(), c = C()
	const kf = k * 0.7, cf = 1.9 * Math.sqrt(kf)
	let i = 0
	for (const el of now) {
		// a part never moved before has no live value yet: it is not a re-entry
		const fresh = !movers.has(el)
		const m = M(el), r = el.getBoundingClientRect()
		const layoutLeft = r.left - m.x.x
		const prev = before && before.get(el)
				if (prev) {
					m.x.to(0, { from: prev.left - layoutLeft, k, c })
					// a part that changed lines (the multiline layout) also travels vertically
					const dy = prev.top - (r.top - m.y.x)
					if (Math.abs(dy) > 0.5 || active.has(m.y)) m.y.to(0, { from: dy, k, c })
		} else if (fan && !fresh && (m.o.x > 0.05 || active.has(m.x))) {
			// re-entry mid-close: continue from where the part is now, velocity and all
			m.x.to(0, { k: kf, c: cf }); m.s.to(1, { k: kf }); m.o.to(1, { k: 420, c: 40 }); m.b.to(0, { k: 420, c: 40 })
		} else if (fan) {
			// the unfold: every part springs out from the centre, nearer ones first,
			// after the ground has started to spread (it leads; the clip keeps parts inside it)
			const centre = layoutLeft + r.width / 2
			const d = Math.abs(centre - cx)
			const delay = reduced.matches ? 0 : Math.min(170, Math.max(40, d * 0.32))
			m.x.jump((cx - centre) * 0.55); m.s.jump(0.5); m.o.jump(0); m.b.jump(6)
			later(() => { m.x.to(0, { k: kf, c: cf }); m.s.to(1, { k: kf }); m.o.to(1, { k: 300, c: 30 }); m.b.to(0, { k: 300, c: 34 }) }, delay)
		} else if (dir && !reduced.matches) {
			// a face swap: the new face slides in along the same axis the old one left by
			m.x.jump(-dir * 24); m.s.jump(1); m.o.jump(0); m.b.jump(3)
			later(() => { m.x.to(0, { k: kf, c: cf }); m.o.to(1, { k: 520, c: 44 }); m.b.to(0, { k: 520, c: 44 }) }, 50)
		} else {
			// a part that just appeared blooms where it stands
			m.x.jump(0); m.s.jump(0.6); m.o.jump(0); m.b.jump(4)
			m.s.to(1); m.o.to(1, { k: 320, c: 32 }); m.b.to(0, { k: 320, c: 34 })
		}
		i++
	}
	if (before) for (const [el, r] of before) if (!now.includes(el)) ghost(el, r, dir)
	fitGround()
}
const timers = new Set()
function later(fn, ms) { if (!ms) return fn(); const t = setTimeout(() => { timers.delete(t); fn() }, ms); timers.add(t) }
function cancelLater() { for (const t of timers) clearTimeout(t); timers.clear() }

/* something leaving the layout fades where it was last seen */
function ghost(el, r, dir = 0) {
	if (!r || !r.width || level === 0) return
	const g = el.cloneNode(true)
	g.removeAttribute('id'); for (const n of g.querySelectorAll('[id]')) n.removeAttribute('id')
	g.setAttribute('aria-hidden', 'true'); g.inert = true
	const pr = pill.getBoundingClientRect()
	Object.assign(g.style, { position: 'absolute', left: (r.left - pr.left) + 'px', top: (r.top - pr.top) + 'px', width: r.width + 'px', height: r.height + 'px', margin: '0', pointerEvents: 'none', gridArea: 'auto', translate: '', scale: '', opacity: '', filter: '' })
	g.classList.add('pill-ghost')
	pill.append(g)
	const m = M(g)
	m.onRest = () => g.remove()
	if (dir) { m.o.to(0, { k: 700, c: 53 }); m.x.to(dir * 24, { k: 700, c: 53 }); m.b.to(3, { k: 700, c: 53 }) }
	else { m.o.to(0, { k: 600, c: 50 }); m.s.to(0.7, { k: 400, c: 36 }) }
}
// a ghost of a chip's label as it folds away: it rides inside the chip, so it
// travels with the chip's own spring instead of floating where it was
function ghostLabel(chip) {
		const l = [...chip.querySelectorAll('.pill-label')].find(n => n.getClientRects().length)
		if (!l || reduced.matches) return
	const r = l.getBoundingClientRect(), cr = chip.getBoundingClientRect()
	const g = l.cloneNode(true)
	g.classList.remove('pill-label'); g.classList.add('pill-label-ghost')
	g.setAttribute('aria-hidden', 'true')
	Object.assign(g.style, { left: (r.left - cr.left) + 'px', top: (r.top - cr.top) + 'px' })
	chip.append(g)
	const m = M(g); m.onRest = () => g.remove()
	m.o.to(0, { k: 900, c: 60 }); m.b.to(3, { k: 600, c: 50 }); m.x.to(-4, { k: 600, c: 50 })
}

function rowWidth() { const r = row.getBoundingClientRect(); return r.width }
/* a chip that blooms grows its OWN side of the pill: a left chip the left
   edge, a right chip the right edge. The row is shifted by half the growth at
   once (the FLIP in play() hides the jump), and the ground's centre follows
   on the same spring as its width, so the field and the other dots hold
   still under the pointer. The pill re-centres when nothing is bloomed. */
let rowShift = 0
function applyShift() {
	let d = 0
	if (bloomed && level && !narrow.matches && !pill.classList.contains('is-cmd') && bloomed.dataset.side) {
		const g = Math.max(0, bloomed.offsetWidth - 32)
		d = bloomed.dataset.side === 'l' ? -g / 2 : g / 2
	}
	rowShift = d
	row.style.translate = d ? `${d.toFixed(1)}px 0` : ''
}
/* the open pill must never sit on the status text: it steps back instead */
function crowd(w = level ? rowWidth() : 0) {
	const b = bar.getBoundingClientRect(), l = $('.bar-l', bar).scrollWidth, r = $('.bar-r', bar).scrollWidth
	body.classList.toggle('pill-crowds', !!level && (b.width - w) / 2 < Math.max(l, r) + 28)
}
function fitGround() {
	if (!level) { crowd(0); ground.sh.to(0); return ground.w.to(44, { k: K() * 1.2, c: 2 * Math.sqrt(K() * 1.2) }) }
	const w = rowWidth()
	crowd(w)
	ground.w.to(w)
	ground.sh.to(rowShift)
	// the canonical open width (nothing bloomed, no draft, no tags): reply bubbles share its edges
	if (!bloomed && send.hidden && att.hidden) body.style.setProperty('--pill-base-w', Math.round(w) + 'px')
}

/* ── levels ────────────────────────────────────────────────────────── */
let level = 0, quietRest = false
function setLevel(n) {
	level = n
	pill.dataset.level = String(n)
	body.dataset.pill = n ? 'open' : 'rest'
	rest.setAttribute('aria-expanded', String(n > 0))
		row.inert = n === 0
		rest.tabIndex = n === 0 ? 0 : -1
		if (window.pill) window.pill.level = n
		applyGrow()
	}
function open({ focus = false } = {}) {
	cancelClose()
	if (level === 0) {
		setLevel(1)
		cancelLater()
		const g = ground.g, kg = K() * 0.6, cg = 2 * Math.sqrt(kg)
		const midClose = g.o.x > 0.05
		// the unfold: the ground pops (scale, opacity) and then spreads sideways, critically
		// damped so it leads the parts without overshooting them. Mid-close it simply turns round.
		pill.style.setProperty('--near', '0')
		play(null, { fan: true })   // (its fitGround is retargeted just below with the unfold's own spring)
		ground.w.to(rowWidth(), midClose ? { k: kg, c: cg } : { from: 60, k: kg, c: cg })
		crowd()
		g.o.to(1, { k: 420, c: 40 }); g.s.to(1, midClose ? { k: K() * 0.9, c: C() * 0.9 } : { from: 0.8, k: K() * 0.9, c: C() * 0.9 })
	}
	if (focus) input.focus({ preventScroll: true })
}
function close({ refocus = false } = {}) {
	cancelClose()
	if (level === 0) return
	closePanel({ refocus: false })
	unbloomAll(false)
	if (pill.dataset.face !== 'main') setFace('main', false)
	const hadFocus = pill.contains(document.activeElement)
	setLevel(0)
	cancelLater()
	crowd(0)
	const cx = crowdEdge().cx
	// exit is faster than entry: parts fade in about 90 ms and barely drift, then the ground folds
	for (const el of parts()) {
		const m = M(el), r = el.getBoundingClientRect()
		const centre = r.left - m.x.x + r.width / 2
		m.x.to((cx - centre) * 0.15, { k: K() * 1.1, c: C() * 1.1 }); m.o.to(0, { k: 1800, c: 85 }); m.s.to(0.85, { k: 900, c: 60 })
	}
	const kc = K() * 1.1
	// reduced motion: the ground keeps its width and simply fades (no shrinking dot)
	if (!reduced.matches) later(() => ground.w.to(44, { k: kc, c: 2 * Math.sqrt(kc) }), 60)
	else ground.g.onRest = () => { if (level === 0) ground.w.jump(44) }
	ground.g.o.to(0, { k: 520, c: 46 }); ground.g.s.to(0.9, { k: 520, c: 46 })
	pill.toggleAttribute('data-draft', !!input.value.trim())
	// a pointer still resting on the pill must not reopen what was just closed: hold hover-open until it leaves
	hoverHold = true
	if (lastPt) onMove({ clientX: lastPt[0], clientY: lastPt[1] })
	// a programmatic return reads as rest, not as a selected control
	if (hadFocus && refocus) { quietRest = true; rest.dataset.quiet = ''; rest.focus({ preventScroll: true }); quietRest = false }
	else if (hadFocus) document.activeElement.blur()
}
let closeTimer = 0
function closeSoon(ms = 420) { cancelClose(); closeTimer = setTimeout(() => { if (canAutoClose()) close() }, ms) }
function cancelClose() { clearTimeout(closeTimer); closeTimer = 0 }
function canAutoClose() { return level > 0 && !pill.contains(document.activeElement) && !agent.holds(document.activeElement) && !panel.dataset.open && !pill.hasAttribute('data-voice') && !pointerInside }

/* ── blooming chips (hover / focus) ───────────────────────────────── */
let bloomed = null
function bloom(chip) {
	if (bloomed === chip || level === 0) return
	const before = snapshot()
	if (bloomed) { ghostLabel(bloomed); bloomed.removeAttribute('data-bloom') }
	bloomed = chip
	if (chip) chip.setAttribute('data-bloom', '')
	applyShift()
	play(before)
}
function unbloomAll(animate = true) {
	if (!bloomed) return
	const before = animate ? snapshot() : null
	if (animate) ghostLabel(bloomed)
	bloomed.removeAttribute('data-bloom'); bloomed = null
	applyShift()
	if (animate) play(before)
}
let unbloomTimer = 0, intentTimer = 0
// hover intent: a short dwell, then bloom whatever is under the pointer once it has
// stopped travelling, so a sweep never blooms the neighbour a growing chip pushed under it
function intent(ms = 60) {
	clearTimeout(intentTimer)
	intentTimer = setTimeout(() => {
		if (!lastPt || level === 0) return
		const chip = document.elementFromPoint(lastPt[0], lastPt[1])?.closest?.('.pill-chip')
		if (!chip || !row.contains(chip) || chip === bloomed) return
		if (Math.abs(M(chip).x.x) > 3) return intent(40)
		bloom(chip)
	}, reduced.matches ? 0 : ms)
}
row.addEventListener('pointerover', e => {
	if (e.pointerType !== 'mouse') return
	const chip = e.target.closest('.pill-chip')
	clearTimeout(unbloomTimer)
	if (chip) { lastPt = [e.clientX, e.clientY]; intent() }
	else if (bloomed && !bloomed.matches(':focus-visible')) unbloomTimer = setTimeout(() => { if (!bloomed?.matches(':focus-visible') && !panel.dataset.open) unbloomAll() }, 160)
})
row.addEventListener('pointermove', e => { if (e.pointerType === 'mouse' && e.target.closest('.pill-chip') && e.target.closest('.pill-chip') !== bloomed) intent() }, { passive: true })
row.addEventListener('pointerleave', e => {
	if (e.pointerType !== 'mouse') return
	clearTimeout(unbloomTimer); clearTimeout(intentTimer)
	unbloomTimer = setTimeout(() => { if (!panel.dataset.open && !(bloomed && bloomed.matches(':focus-visible'))) unbloomAll() }, 200)
})
row.addEventListener('focusin', e => {
	const chip = e.target.closest('.pill-chip')
	if (chip && chip.matches(':focus-visible')) bloom(chip)
	else if (!chip && !panel.dataset.open) unbloomAll()
		setRoving(e.target.closest('.pill-chip, #pill-input, .pill-send, .pill-tag button'))
})

/* ── the roving tab stop: one Tab stop, arrows inside ─────────────── */
const stops = () => [...row.querySelectorAll('.pill-chip, #pill-input, .pill-send, .pill-tag button')].filter(el => el.getClientRects().length && !el.closest('[hidden]'))
function setRoving(el) { if (!el) return; for (const s of stops()) s.tabIndex = s === el ? 0 : -1 }
function moveFocus(from, dir) {
	const list = stops(), i = list.indexOf(from)
	const next = list[(i + dir + list.length) % list.length]
	if (next) { setRoving(next); next.focus({ preventScroll: true }) }
}
row.addEventListener('keydown', e => {
	const t = e.target
	if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
		const dir = e.key === 'ArrowRight' ? 1 : -1
		if (t === input) {
			const atEdge = dir < 0 ? input.selectionStart === 0 && input.selectionEnd === 0 : input.selectionStart === input.value.length
			if (!atEdge) return
		}
		e.preventDefault(); moveFocus(t, dir)
	} else if (e.key === 'Home' && t !== input) { e.preventDefault(); const s = stops()[0]; setRoving(s); s.focus({ preventScroll: true }) }
	else if (e.key === 'End' && t !== input) { e.preventDefault(); const s = stops().at(-1); setRoving(s); s.focus({ preventScroll: true }) }
	else if ((e.key === 'ArrowUp') && t.matches('.pill-chip[aria-haspopup]')) { e.preventDefault(); openPanel(t, { focusFirst: true }) }
})

/* ── panels (level 2) ─────────────────────────────────────────────── */
const titles = { attach: 'Attach', mode: 'Agent mode', theme: 'Theme', command: 'Pages and actions' }
const pm = M(panel)
pm.o.jump(0); pm.s.jump(0.94)
pm.onRest = () => { if (!panel.dataset.open && pm.o.x < 0.01) { panel.removeAttribute('data-pane-shown'); panel.style.visibility = '' } }
let panelChip = null, cmdClip = null, cmdOpening = 0, cmdW = 0, cmdRise = null, cmdRiseT = 0
function panelX(chip) {
	if (narrow.matches || chip === cmdChip) return 0   // the palette grows straight up out of the pill
	const r = chip.getBoundingClientRect(), m = M(chip), e = crowdEdge()
	if (!r.width) return 0
	const centre = r.left - m.x.x + r.width / 2
	const w = panel.offsetWidth, half = w / 2
	const clamped = Math.max(e.left + 12 + half, Math.min(e.right - 12 - half, centre))   // inside the sheet
	return clamped - e.cx
}
function openPanel(chip, { focusFirst = false } = {}) {
	const tool = chip.dataset.tool
	const switching = !!panel.dataset.open
	if (switching && panel.dataset.pane === 'command' && tool !== 'command') leaveCommand()
	if (panelChip && panelChip !== chip) panelChip.setAttribute('aria-expanded', 'false')
	panelChip = chip
	chip.setAttribute('aria-expanded', 'true')
	if (bloomed !== chip && !narrow.matches && chip.getClientRects().length) bloom(chip)
	if (tool !== 'command' && cmdClip) { cmdClip.cancel(); cmdClip = null }
	panel.dataset.pane = tool
	panel.dataset.open = 'true'
	$('#pill-panel-title').textContent = titles[tool] || tool
	body.dataset.pillPanel = tool
	setLevel(2)
	if (tool === 'theme') markTheme()
	/* the palette's pane must never paint at full size before its rise starts
	   (one frame of an empty slab): it is shut from this frame, and the rise
	   below opens it from the pill's seam in the next */
	const cmdFresh = tool === 'command' && !(cmdClip && cmdClip.playState === 'running' && cmdClip._dir === -1)
	if (cmdFresh) panel.style.clipPath = reduced.matches ? '' : 'inset(100% 50% 0 50%)'
	if (cmdFresh && reduced.matches) panel.style.opacity = '0'
	requestAnimationFrame(() => {
		panel.style.clipPath = ''; panel.style.opacity = ''
		const x = panelX(chip)
		// spring out of the chip itself, even when the panel is clamped to the viewport edge
		const cr = chip.getBoundingClientRect(), chipCentre = cr.width ? cr.left - M(chip).x.x + cr.width / 2 : crowdEdge().cx
		const panelLeft = crowdEdge().cx + x - panel.offsetWidth / 2
		const ox = narrow.matches || chip === cmdChip ? panel.offsetWidth / 2 : Math.max(16, Math.min(panel.offsetWidth - 16, chipCentre - panelLeft))
		panel.style.transformOrigin = `${ox.toFixed(1)}px 100%`
		if (tool === 'command') {
			// the palette IS the pill: the pane sits on the pill's top edge at the
			// pill's own width and grows upward out of it (a clip rising from the
			// seam, critically damped, about 200 ms): one surface, never a card
			pm.x.jump(0); pm.y.jump(0); pm.s.jump(1); pm.o.jump(1); pm.b.jump(0)
			// re-opened while it was still sinking back: the same clip turns round
			if (cmdClip && cmdClip.playState === 'running' && cmdClip._dir === -1 && !reduced.matches) { cmdClip._dir = 1; cmdClip.reverse(); return }
			cmdClip?.cancel()
			cmdW = panel.offsetWidth; ground.render()
			// the rising edge keeps the pane's round corners all the way up, and
			// the rows come in on the first 60% of the rise, so a row the side
			// clip still crosses is never seen at full contrast
			if (!reduced.matches) {
				// the pane rises once the pill under it is nearly as wide as the
				// list (the side clips within 28px of it, or 200ms at most): rising
				// earlier showed only the list's empty middle, a blank slab (J6)
				const rise = () => {
					cmdRise = null; clearTimeout(cmdRiseT)
					if (panel.dataset.pane !== 'command' || !panel.dataset.open) { panel.style.clipPath = ''; return }
					panel.style.clipPath = ''
					cmdClip = panel.animate([{ clipPath: 'inset(calc(100% - 1px) var(--cmd-x) 0 var(--cmd-x) round 20px 20px 0 0)' }, { clipPath: 'inset(-48px var(--cmd-x) 0 var(--cmd-x) round 20px 20px 0 0)' }], { duration: 240, easing: 'cubic-bezier(.23, 1, .32, 1)' })
					// the rows are there from the first grown frame (never an empty pane)
					$('.pill-pane[data-pane="command"]', panel)?.animate([{ opacity: 0.6 }, { opacity: 1 }], { duration: 160, easing: 'cubic-bezier(.4, 0, .6, 1)' })
				}
				panel.style.clipPath = 'inset(100% 50% 0 50%)'
				cmdRise = rise; cmdRiseT = setTimeout(() => cmdRise?.(), 200)
				ground.render()
			}
			else cmdClip = panel.animate([{ opacity: 0 }, { opacity: 1 }], { duration: 120, easing: 'linear' })
		} else if (switching) { pm.x.to(x) } else {
			pm.x.jump(x); pm.y.jump(10); pm.s.jump(0.92); pm.o.jump(0); pm.b.jump(6)
			pm.y.to(0); pm.s.to(1, { k: K() * 0.9, c: C() * 0.85 }); pm.o.to(1, { k: 420, c: 38 }); pm.b.to(0, { k: 420, c: 40 })
		}
		if (focusFirst) panelItems()[0]?.focus({ preventScroll: true })
	})
}
function closePanel({ refocus = true } = {}) {
	if (!panel.dataset.open) return
	delete panel.dataset.open
	delete body.dataset.pillPanel
	const chip = panelChip
	panelChip = null
	chip?.setAttribute('aria-expanded', 'false')
	panel.style.visibility = 'visible'   // stay painted while it leaves; pm.onRest (or the clip) hides it
	if (panel.dataset.pane === 'command') {
		leaveCommand()
		// back down into the pill it came out of
		const hide = () => { if (panel.dataset.open) { cmdClip?.cancel(); cmdClip = null; return } pm.o.jump(0); panel.removeAttribute('data-pane-shown'); panel.style.visibility = ''; cmdClip?.cancel(); cmdClip = null }
		// closed while still growing: the same clip turns round from where it is
		if (cmdClip && cmdClip.playState === 'running' && cmdClip._dir !== -1 && !reduced.matches) { cmdClip._dir = -1; cmdClip.effect.updateTiming({ fill: 'backwards' }); cmdClip.reverse(); cmdClip.finished.then(hide, () => {}) }
		else {
		cmdClip?.cancel()
		// it sinks back into the pill and leaves nothing behind: the ring and
		// shadow fade with the clip, and the last pixel row closes too
		cmdClip = panel.animate(reduced.matches ? [{ opacity: 1 }, { opacity: 0 }] : [{ clipPath: 'inset(-48px var(--cmd-x) 0 var(--cmd-x) round 20px 20px 0 0)', opacity: 1 }, { opacity: 1, offset: 0.55 }, { clipPath: 'inset(100% var(--cmd-x) 0 var(--cmd-x) round 20px 20px 0 0)', opacity: 0 }],
			{ duration: reduced.matches ? 100 : 150, easing: 'cubic-bezier(.32, .72, 0, 1)', fill: 'forwards' })
		cmdClip._dir = -1
		cmdClip.finished.then(hide, () => {})
		}
	} else {
		pm.y.to(8, { k: 700, c: 54 }); pm.s.to(0.96, { k: 700, c: 54 }); pm.o.to(0, { k: 900, c: 60 }); pm.b.to(3, { k: 700, c: 54 })
	}
	if (level === 2) setLevel(1)
	if (refocus && chip === cmdChip) { setRoving(input); input.focus({ preventScroll: true }) }
	else if (refocus && chip) { chip.focus({ preventScroll: true }); setRoving(chip) }
}
const panelItems = () => panel.dataset.pane === 'command' ? [] : $$('.pill-pane', panel).filter(p => p.getClientRects().length).flatMap(p => $$('a[href], button', p)).filter(el => el.getClientRects().length)
panel.addEventListener('keydown', e => {
	if (e.key === 'ArrowDown' || e.key === 'ArrowUp' || e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
		const list = panelItems(), i = list.indexOf(document.activeElement)
		const dir = e.key === 'ArrowDown' || e.key === 'ArrowRight' ? 1 : -1
		e.preventDefault()
		list[(i + dir + list.length) % list.length]?.focus({ preventScroll: true })
	} else if (e.key === 'Tab') {
		e.preventDefault(); closePanel(); if (panelChip) moveFocus(panelChip, e.shiftKey ? -1 : 1)
	}
})

/* ── tools ─────────────────────────────────────────────────────────── */
row.addEventListener('click', e => {
	const chip = e.target.closest('.pill-chip')
	if (!chip) return
	const tool = chip.dataset.tool
	setRoving(chip)
	if (tool === 'command') { command(); return }
	if (tool === 'agent') { agent.chip(); return }
	if (tool === 'voice') { toggleVoice(); return }
	if (tool === 'tools') { setFace(pill.dataset.face === 'tools' ? 'main' : 'tools'); return }
	if (panelChip === chip) closePanel()
	else openPanel(chip, { focusFirst: e.detail === 0 })
})
function setFace(face, animate = true) {
	const before = animate ? snapshot() : null
	pill.dataset.face = face
	const t = row.querySelector('[data-tool="tools"]')
	t.setAttribute('aria-pressed', String(face === 'tools'))
		if (face === 'main') closePanel({ refocus: false })
		if (animate) play(before, { dir: face === 'tools' ? -1 : 1 })
		syncSend()   // the tools face has no field: never multiline there
	}
panel.addEventListener('mousedown', e => { if (e.target.closest('#pill-cmd-list')) e.preventDefault() })   // keep the caret in the filter
panel.addEventListener('click', e => {
	const b = e.target.closest('button, a, [role=option]'); if (!b) return
	if (b.dataset.setMode) setMode(b.dataset.setMode)
	else if (b.dataset.attach) addAttachment(b.dataset.attach)
	else if (b.dataset.themeSet) { const [axis, v] = b.dataset.themeSet.split(':'); window.shell?.setTheme(axis, v).then(markTheme) }
	else if (b.closest('[role=option]')) { e.preventDefault(); runOption(b.closest('[role=option]')) }
})

const MODES = { ask: ['Ask', 'Ask anything'], draft: ['Draft', 'Describe what to draft'], explain: ['Explain', 'What should I explain?'] }
function setMode(m, { quiet = false } = {}) {
	if (!MODES[m]) m = 'ask'
	pill.dataset.mode = m
	$('#pill-mode-label').textContent = MODES[m][0]
	row.querySelector('[data-tool="mode"]').setAttribute('aria-label', 'Agent mode: ' + MODES[m][0])
	if (!pill.hasAttribute('data-voice')) input.placeholder = (agent && agent.on && agent.placeholder()) || MODES[m][1]
	for (const b of $$('[data-set-mode]', panel)) b.setAttribute('aria-checked', String(b.dataset.setMode === m))
	try { localStorage.setItem('pill-mode', m) } catch {}
	if (!quiet) { closePanel(); }
}
try { setMode(localStorage.getItem('pill-mode') || 'ask', { quiet: true }) } catch { setMode('ask', { quiet: true }) }

// the radios are painted by the shell (index.html, markTheme): one painter for
// the settings sidebar and this pane, so the two never disagree
function markTheme() { window.shell?.markTheme?.() }
document.addEventListener('shell:theme', markTheme)

const attachments = []
function addAttachment(kind) {
	const label = kind === 'page' ? '/' + (window.shellNav?.current || 'page') : kind === 'selection' ? '#' + ($('#main > [id]')?.id || 'main') : 'notes.pdf'
	if (attachments.includes(label)) { closePanel(); input.focus({ preventScroll: true }); return }
	closePanel({ refocus: false })
	const before = snapshot()
	attachments.push(label)
	const tag = document.createElement('span')
	tag.className = 'pill-tag'
	tag.innerHTML = '<span></span><button type="button" tabindex="-1"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button>'
	tag.firstChild.textContent = label
	tag.querySelector('button').setAttribute('aria-label', 'Remove ' + label)
	tag.querySelector('button').addEventListener('click', () => {
		const b = snapshot(); attachments.splice(attachments.indexOf(label), 1); tag.remove()
		att.hidden = !attachments.length; play(b); input.focus({ preventScroll: true })
	})
	att.append(tag); att.hidden = false
	play(before)
	input.focus({ preventScroll: true })
}

let voiceTimer = 0
function toggleVoice() {
	const chip = row.querySelector('[data-tool="voice"]')
	const on = !pill.hasAttribute('data-voice')
	pill.toggleAttribute('data-voice', on)
	chip.setAttribute('aria-pressed', String(on))
	clearTimeout(voiceTimer)
	if (on) {
		if (narrow.matches && pill.dataset.face === 'tools') setFace('main')
		input.placeholder = 'Listening… (demo)'
		voiceTimer = setTimeout(() => { typeInto('Summarise what changed on this page'); toggleVoice() }, 2200)
	} else setMode(pill.dataset.mode, { quiet: true })
}
function typeInto(text) {
	let i = 0
	const step = () => { input.value = text.slice(0, ++i); syncSend(); if (i < text.length) setTimeout(step, reduced.matches ? 0 : 18) }
	step()
}

/* ── THE TEXT: one line, or the multiline layout ─────────────────────
   Whether the text needs a second line is read in the ONE-LINE layout
   (the class is lifted for one synchronous measurement), so the pill goes
   back to one line exactly when the text fits it again. In the multiline
   layout the textarea is sized to its text (up to the cap, then it
   scrolls) and the row's laid-out height is written to --pill-hh; the
   ground's height springs to it (ground.h). The pill grows upward: the row
   and the ground keep their bottom on the bar (pill.css), and the console,
   the replies and the tool panels above it step up by the growth. */
let multi = false
const multiOK = () => !pill.classList.contains('is-cmd') && !(narrow.matches && pill.dataset.face === 'tools')
function needsLines() {
	if (!multiOK() || !input.value) return false
	if (input.value.includes('\n')) return true
	const was = pill.classList.contains('is-multi'), h = input.style.height
	if (was) pill.classList.remove('is-multi')
	input.style.height = ''
	const over = input.scrollHeight > input.clientHeight + 2
	input.style.height = h
	if (was) pill.classList.add('is-multi')
	return over
}
let rowH = 0
function sizeText() {
	if (!multi) { input.style.height = ''; input.style.overflowY = ''; rowH = pillH(); return }
	// the cap: the whole pill at min(240px, 40vh) (about 10 lines of text)
	const bar = 46, cap = Math.max(44, Math.min(240, innerHeight * 0.4) - bar)
	input.style.height = 'auto'
	const sh = input.scrollHeight
	input.style.height = Math.min(sh, cap) + 'px'
	input.style.overflowY = sh > cap + 1 ? 'auto' : 'hidden'
	// typing at the end keeps the newest line in view
	if (sh > cap + 1 && input.selectionEnd === input.value.length) input.scrollTop = sh
	rowH = Math.max(pillH(), row.offsetHeight)
}
/* the growth reaches the ground (a spring) and what stands above the pill */
let grown = -1
function applyGrow() {
	const h = level && multi ? rowH : pillH()
	pill.style.setProperty('--pill-hh', (multi ? rowH : pillH()) + 'px')
	ground.h.to(h, { k: K(), c: 2 * Math.sqrt(K()) })
	const g = Math.max(0, h - pillH())
	if (g !== grown) {
		grown = g
		for (const el of [layer, $('#pill-console')]) el?.style.setProperty('--pill-grow', g + 'px')
	}
}
function setMulti(m) {
	multi = m
	pill.classList.toggle('is-multi', m)
	sizeText()
	applyGrow()
}
/* the send button exists only while there is something to send */
function syncSend() {
	const has = !!input.value.trim() && !pill.classList.contains('is-cmd')
	const m = needsLines()
	if (send.hidden === !has && m === multi) { if (multi) { sizeText(); applyGrow() } return }
	const before = snapshot()
	send.hidden = !has
	setMulti(m)
	play(before)
}
input.addEventListener('input', () => { if (pill.classList.contains('is-cmd')) filterCommand(); else { syncSend(); agent.typed() } })

/* ── the prompt and the demo reply (no backend) ───────────────────── */
/* Enter sends (Shift+Enter is a new line; an IME composition never sends).
   The palette's and the picker's own Enter run first and claim the key */
input.addEventListener('keydown', e => {
	if (e.key !== 'Enter' || e.defaultPrevented || e.isComposing || e.keyCode === 229) return
	if (e.shiftKey && !pill.classList.contains('is-cmd')) return
	e.preventDefault()
	submitPrompt(e.isTrusted)
})
row.addEventListener('submit', e => { e.preventDefault(); submitPrompt(e.isTrusted) })
function submitPrompt(trusted) {
	const q = input.value.trim()
	if (agent.on) { agent.submit(q, trusted); return }
	if (!q) return
	answer(q, attachments.slice())
	input.value = ''
	syncSend()
		if (attachments.length) { const b = snapshot(); attachments.length = 0; att.replaceChildren(); att.hidden = true; play(b) }
	}
let bubbleN = 0
function words(text) { return text.split(/(\s+)/).map((w, i) => { if (/^\s+$/.test(w)) return document.createTextNode(w); const s = document.createElement('span'); s.className = 'pill-w'; s.style.setProperty('--i', String(i >> 1)); s.textContent = w; return s }) }
function reply(q, files) {
	const pages = window.shellNav?.model?.().flatMap(g => g.pages) || []
	const lower = q.toLowerCase()
	const hit = pages.find(p => lower.includes(p.slug) || lower.includes(p.title.toLowerCase()))
	const mode = pill.dataset.mode
	const ctx = files.length ? ' With ' + files.join(', ') + ' attached.' : ''
	if (/\b(dark|night)\b/.test(lower)) { window.shell?.setTheme('mode', 'dark'); return { text: 'Done: the workspace is in dark mode now. Say “light” to switch back.' + ctx } }
	if (/\b(light|day)\b/.test(lower)) { window.shell?.setTheme('mode', 'light'); return { text: 'Done: back to light mode.' + ctx } }
	if (hit) return { text: (mode === 'explain' ? 'The page ' : 'You will find that on ') + hit.title + ' (/' + hit.slug + '). It is in the sidebar, or open it here:' + ctx, link: hit }
	if (mode === 'draft') return { text: 'Here is a first draft, kept local: a short note titled “' + q.slice(0, 40) + '” with three bullet points, ready to be patched into #aside by a real agent.' + ctx }
	if (mode === 'explain') return { text: 'This page is one Stage: a quiet title, the canvas, and this pill. Agents patch Regions into it; the notes card and these bubbles appear only when something arrives.' + ctx }
	return { text: 'This is a local demo reply: no backend is wired yet. A real agent would answer by patching #layer or #aside, and the pill would stay exactly where it is.' + ctx }
}
function answer(q, files) {
	for (const old of $$('.pill-bubble', layer).slice(0, -1)) dismiss(old)
	const id = 'pill-bubble-' + (++bubbleN)
	const b = document.createElement('article')
	b.className = 'pill-bubble'; b.id = id
	b.setAttribute('aria-label', 'Assistant reply')
	const m = pill.dataset.mode
	b.innerHTML = '<header><i class="pill-dot" aria-hidden="true"></i><span></span><button type="button" class="pv-x pill-pin" aria-label="Pin the answer to the inspector" disabled><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 17v5"/><path d="M9 10.8a2 2 0 0 1-1.1 1.8l-1.8.9A2 2 0 0 0 5 15.2V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.8a2 2 0 0 0-1.1-1.8l-1.8-.9A2 2 0 0 1 15 10.8V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/></svg></button><button type="button" class="pv-x" aria-label="Dismiss the reply"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button></header><p class="pill-q"></p><p class="pill-a"><span class="pill-think" aria-label="Thinking"><i></i><i></i><i></i></span></p>'
	b.dataset.mode = m
	b.querySelector('.pill-dot').dataset.mode = m
	b.querySelector('header span').textContent = MODES[m][0] + ' · demo reply'
	b.querySelector('.pill-q').textContent = '“' + q + '”'
	b.querySelector('.pv-x:not(.pill-pin)').addEventListener('click', () => dismiss(b))
	b.querySelector('.pill-pin').addEventListener('click', () => pinBubble(b))
	layer.append(b)
	setTimeout(() => {
		if (!b.isConnected) return
		const r = reply(q, files), a = b.querySelector('.pill-a')
		a.replaceChildren(...words(r.text))
		if (r.link) { a.append(' '); const l = document.createElement('a'); l.href = '/' + r.link.slug; l.textContent = 'Open ' + r.link.title; a.append(l) }
		b.querySelector('.pill-pin').disabled = false
		b.dataset.answer = r.text.replace(/, or open it here:/, '.')
	}, reduced.matches ? 150 : 750)
}
function dismiss(b) {
	if (!b || b.hasAttribute('data-leaving')) return
	b.setAttribute('data-leaving', '')
	setTimeout(() => b.remove(), reduced.matches ? 0 : 170)
}

/* ── pointer: proximity, hover-open, leave-close, tap ─────────────── */
let pointerInside = false, nearTick = 0, lastPt = null
function within(x, y, r, pad) { return x >= r.left - pad && x <= r.right + pad && y >= r.top - pad && y <= r.bottom + pad }
function onMove(e) {
	if (e.pointerType && e.pointerType !== 'mouse') return
	lastPt = [e.clientX, e.clientY]
	if (nearTick) return
	nearTick = requestAnimationFrame(() => {
		nearTick = 0
		const [x, y] = lastPt
		if (level === 0) {
			const r = rest.getBoundingClientRect()
			if (hoverHold && !within(x, y, zone.getBoundingClientRect(), 0)) hoverHold = false
			const dx = Math.max(0, Math.abs(x - (r.left + r.width / 2)) - 40), dy = Math.max(0, Math.abs(y - (r.top + r.height / 2)) - 6)
			const near = Math.max(0, 1 - Math.hypot(dx / 1.6, dy) / 260)
			pill.style.setProperty('--near', (near * near).toFixed(3))
		} else {
			const inside = within(x, y, row.getBoundingClientRect(), 28) || (panel.dataset.open && within(x, y, panel.getBoundingClientRect(), 20)) || agent.covers(x, y)
			if (inside !== pointerInside) { pointerInside = inside; if (inside) cancelClose(); else closeSoon() }
		}
	})
}
document.addEventListener('pointermove', onMove, { passive: true })
document.documentElement.addEventListener('pointerleave', () => { pill.style.setProperty('--near', '0'); if (level && pointerInside) { pointerInside = false; closeSoon() } })
const zone = $('.pill-zone')
let hoverTimer = 0, hoverHold = false
function hoverOpen(e) {
	if (e.pointerType !== 'mouse' || level || hoverHold) return
	clearTimeout(hoverTimer)
	// the intent dwell guards a pointer passing by; one that comes back while
	// the pill is still folding away has already decided: turn it round now
	const returning = ground.g.o.x > 0.05
	hoverTimer = setTimeout(() => { if (level === 0) { pointerInside = true; open() } }, returning ? 0 : 70)
}
zone.addEventListener('pointerenter', hoverOpen)
rest.addEventListener('pointerenter', hoverOpen)
zone.addEventListener('pointerleave', () => { clearTimeout(hoverTimer); hoverHold = false })
rest.addEventListener('pointerleave', () => clearTimeout(hoverTimer))
// tap (touch, pen) or click on the line or its zone: open
zone.addEventListener('click', () => open({ focus: finePointer.matches }))
rest.addEventListener('click', () => open({ focus: true }))
rest.addEventListener('blur', () => rest.removeAttribute('data-quiet'))
rest.addEventListener('focus', () => { if (!quietRest && rest.matches(':focus-visible')) open({ focus: true }) })
// a press anywhere outside the pill closes it
document.addEventListener('pointerdown', e => {
		if (!level || pill.contains(e.target) || e.target.closest?.('#layer, .pin-flyer, #pill-console, #agents-panel [data-pill-call]')) return
	pointerInside = false
	close()
}, true)
pill.addEventListener('focusout', () => setTimeout(() => {
	if (level && !pill.contains(document.activeElement) && !agent.holds(document.activeElement) && !pointerInside) close()
}, 0))

/* ── keys: "/" opens, Esc closes one level ────────────────────────── */
function editable(t) { return t && (t.isContentEditable || /^(input|textarea|select)$/i.test(t.tagName)) }
document.addEventListener('keydown', e => {
	if (e.key === '/' && !e.metaKey && !e.ctrlKey && !e.altKey && !editable(e.target) && !document.querySelector('dialog[open]')) {
		e.preventDefault(); open({ focus: true }); return
	}
	if (e.key === 'Escape' && level) e.stopPropagation()
	if (e.key !== 'Escape' || document.querySelector('dialog:modal')) return
	if (level === 0 && e.defaultPrevented) return
	if (level !== 2 && agent.collapse()) { e.preventDefault(); return }
	if (level === 2) { e.preventDefault(); closePanel() }
	else if (level === 1) {
		e.preventDefault()
		if (pill.hasAttribute('data-voice')) toggleVoice()
		else if (narrow.matches && pill.dataset.face === 'tools') { setFace('main'); input.focus({ preventScroll: true }) }
		else if (bloomed && document.activeElement !== input && pill.contains(document.activeElement)) { setRoving(input); input.focus({ preventScroll: true }) ; unbloomAll() }
		else close({ refocus: true })
	} else {
		const last = $$('.pill-bubble:not([data-leaving])', layer).at(-1)
		if (last) { e.preventDefault(); dismiss(last) }
	}
}, true)
document.addEventListener('pill:focus', () => open({ focus: true }))
addEventListener('pill:prompt', e => { open({ focus: true }); input.value = String(e.detail || ''); syncSend(); input.setSelectionRange(input.value.length, input.value.length) })
addEventListener('resize', () => { if (level) { fitGround(); if (panelChip) pm.x.to(panelX(panelChip)) } })
/* the bar decides "narrow" (and whether the open pill crowds the status) */
function measureBar() {
	const w = bar.getBoundingClientRect().width
	const n = w < 560
	body.classList.toggle('sheet-narrow', w < 720)
	if (n !== narrow.matches) {
		narrow.matches = n
		pill.toggleAttribute('data-narrow', n)
		if (pill.dataset.face !== 'main') setFace('main', false)
	}
		syncSend()   // a new width can add or remove a line
		if (level) { fitGround(); if (panelChip) pm.x.to(panelX(panelChip)) }
		crowd()
	}
	new ResizeObserver(() => measureBar()).observe(bar)

/* ── THE PALETTE: Ctrl/⌘ K grows it upward out of the pill; the prompt is
   its filter. Pages come from shellNav.model() (grouped like the tree), the
   actions run through window.shell. "Ask …" is always the last option, so a
   query that finds nothing still goes somewhere. ─────────────────────── */
let cmdFromRest = false, cmdDraft = '', cmdActive = 0, cmdItems = new Map()
const SVG = {
	page: '<path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/>',
	act: '<path d="m9 18 6-6-6-6"/>',
	theme: '<circle cx="12" cy="12" r="9"/><path d="M12 3a9 9 0 0 1 0 18z" fill="currentColor"/>',
	ask: '<path d="M12 3v3M12 18v3M3 12h3M18 12h3M5.6 5.6l2.1 2.1M16.3 16.3l2.1 2.1M5.6 18.4l2.1-2.1M16.3 7.7l2.1-2.1"/>',
}
function goPage(slug) {
	const a = document.querySelector(`#page-list a[href="/${CSS.escape(slug)}"]`)
	if (a && window.deck?.go) window.deck.go(a); else location.href = '/' + slug
}
/* the icon a page wears in the tree (its own, or its group's) */
function treeIcon(slug) {
	const a = document.querySelector(`#page-list a[href="/${CSS.escape(slug)}"]`)
	const svg = a?.querySelector(':scope > svg') || a?.closest('details')?.querySelector(':scope > summary > svg')
	return svg ? svg.outerHTML : null
}
function commandGroups() {
	const nav = window.shellNav, sh = window.shell || {}
	const mod = sh.mac ? '⌘' : 'Ctrl '
	const groups = (nav?.model?.() || []).map(g => ({ label: g.label, items: g.pages.map(p => ({
		id: 'page:' + p.slug, label: g.landing === p && g.pages.length > 1 ? g.label : p.title, hint: '/' + p.slug, kw: p.slug + ' ' + g.label + ' ' + p.title,
		icon: 'page', svg: treeIcon(p.slug), current: p.slug === nav.current, run: () => goPage(p.slug) })) }))
	const theme = (axis, v) => () => sh.setTheme?.(axis, v, { palette: true })
	/* the theme rows show which one is in force (J1: the palette's rows agree with <html>) */
	const H = document.documentElement, curP = H.getAttribute('data-theme') || '', curM = H.getAttribute('data-mode') || 'system'
	const on = (axis, v) => axis === 'mode' ? v === curM : v === curP
	const panel = (name, label, kw, n) => ({ id: 'act:panel:' + name, label: 'Sidebar: ' + label, kw: kw + ' sidebar ribbon panel', kbd: 'Alt ' + n, icon: 'act', run: () => sh.openPanel?.(name, { focus: true }) })
	groups.push({ label: 'Sidebars', items: [
		panel('pages', 'Pages', 'tree navigation', 1), panel('inbox', 'Inbox', 'agents alerts notifications', 2),
				panel('data', 'Data', 'bases records tables', 3), panel('settings', 'Settings', 'appearance account theme', 4),
				sh.hasAgent ? panel('agents', 'Agents', 'herdr sessions claude codex crew terminals', 5) : null,
			].filter(Boolean) })
	groups.push({ label: 'Actions', items: [
		{ id: 'act:sidebar', label: 'Toggle sidebar', kw: 'collapse expand fold rail navigation', kbd: mod + 'B', run: () => sh.toggleSidebar?.() },
		{ id: 'act:inspector', label: 'Toggle inspector', kw: 'regions signals detail aside pins pinned', kbd: mod + '.', run: () => sh.toggleInspector?.() },
		{ id: 'act:light', label: 'Theme: Light', kw: 'mode day bright', icon: 'theme', on: on('mode', 'light'), run: theme('mode', 'light') },
		{ id: 'act:dark', label: 'Theme: Dark', kw: 'mode night', icon: 'theme', on: on('mode', 'dark'), run: theme('mode', 'dark') },
		{ id: 'act:system', label: 'Theme: System', kw: 'mode os auto', icon: 'theme', on: on('mode', 'system'), run: theme('mode', 'system') },
		{ id: 'act:bento', swatch: 'bento', label: 'Personality: Bento', kw: 'theme default neutral', hint: 'the default · neutral', icon: 'theme', on: on('personality', ''), run: theme('personality', '') },
		{ id: 'act:harbor', swatch: 'harbor', label: 'Personality: Harbor', kw: 'theme cool technical', hint: 'cool and technical', icon: 'theme', on: on('personality', 'harbor'), run: theme('personality', 'harbor') },
		{ id: 'act:folio', swatch: 'folio', label: 'Personality: Folio', kw: 'theme editorial serif paper ink reading', hint: 'editorial · paper and ink', icon: 'theme', on: on('personality', 'folio'), run: theme('personality', 'folio') },
		{ id: 'act:copy', label: 'Copy page URL', kw: 'link share clipboard', run: () => sh.copyURL?.() },
		sh.superuser ? { id: 'act:admin', label: 'Admin console', kw: 'settings administrator', run: () => { location.href = '/admin/overview' } } : null,
		sh.authed ? { id: 'act:signout', label: 'Sign out', kw: 'logout account', run: () => sh.signOut?.() }
			: { id: 'act:signin', label: 'Sign in', kw: 'login account', run: () => { location.href = '/auth/sign-in?return_to=/' + (nav?.current || '') } },
	].filter(Boolean).map(a => ({ icon: 'act', ...a })) })
	return groups
}
function option(it) {
	const o = document.createElement('div')
	o.className = 'pill-opt'; o.setAttribute('role', 'option'); o.id = 'pill-cmd-' + it.id.replace(/[^a-z0-9-]/gi, '-')
	o.dataset.id = it.id; if (it.ask) o.dataset.ask = ''
	o.setAttribute('aria-selected', 'false')
	if (it.current) o.setAttribute('aria-current', 'page')
	if (it.on) o.setAttribute('aria-current', 'true')
	o.innerHTML = (it.swatch ? '<i class="pill-swatch" data-p="' + it.swatch + '" aria-hidden="true"></i>' : it.svg || '<svg viewBox="0 0 24 24" aria-hidden="true">' + SVG[it.icon] + '</svg>') + '<span><b></b></span>'
	o.querySelector('b').textContent = it.label
	if (it.hint || it.kbd) { const h = document.createElement(it.kbd ? 'kbd' : 'small'); h.className = 'pill-slug'; h.textContent = it.kbd || it.hint; o.querySelector('span').append(h) }
	cmdItems.set(it.id, it)
	return o
}
function filterCommand() {
	const q = input.value.trim().toLowerCase(), words = q.split(/\s+/).filter(Boolean)
	cmdItems = new Map()
	const out = []
	for (const g of commandGroups()) {
		const hits = g.items.filter(it => { const hay = (it.label + ' ' + (it.kw || '') + ' ' + (it.hint || '')).toLowerCase(); return words.every(w => hay.includes(w)) })
		if (q) hits.sort((a, b) => (b.label.toLowerCase().startsWith(q) ? 1 : 0) - (a.label.toLowerCase().startsWith(q) ? 1 : 0))
		if (!hits.length) continue
		const grp = document.createElement('div')
		grp.setAttribute('role', 'group'); grp.setAttribute('aria-label', g.label)
		const h = document.createElement('div'); h.className = 'pill-group'; h.setAttribute('aria-hidden', 'true'); h.textContent = g.label
		grp.append(h, ...hits.map(option))
		out.push(grp)
	}
	if (q) {
		const grp = document.createElement('div')
		grp.setAttribute('role', 'group'); grp.setAttribute('aria-label', 'Assistant')
		grp.append(option({ id: 'ask', ask: true, icon: 'ask', label: 'Ask “' + input.value.trim() + '”', hint: (MODES[pill.dataset.mode] || MODES.ask)[0], run: null }))
		out.push(grp)
	}
	cmdList.replaceChildren(...out)
	const opts = $$('[role=option]', cmdList)
	setActive(q ? 0 : Math.max(0, opts.findIndex(o => !o.hasAttribute('aria-current'))))
}
function setActive(i, { scroll = true } = {}) {
	const opts = $$('[role=option]', cmdList)
	if (!opts.length) { input.removeAttribute('aria-activedescendant'); return }
	cmdActive = (i + opts.length) % opts.length
	opts.forEach((o, j) => o.setAttribute('aria-selected', String(j === cmdActive)))
	input.setAttribute('aria-activedescendant', opts[cmdActive].id)
	if (scroll) opts[cmdActive].scrollIntoView({ block: 'nearest' })
}
/* ONE highlight: the pointer moves the selection itself (no separate hover
   wash), so the keyboard and the mouse can never light two rows at once */
cmdList.addEventListener('pointermove', e => {
	const o = e.target.closest?.('[role=option]')
	if (!o) return
	const i = $$('[role=option]', cmdList).indexOf(o)
	if (i >= 0 && i !== cmdActive) setActive(i, { scroll: false })
})
function runOption(o) {
	const it = o && cmdItems.get(o.dataset.id)
	if (!it) return
	if (it.ask) {
		const q = input.value.trim()
		cmdDraft = ''
		closePanel({ refocus: true })
		input.value = q; syncSend()
		row.requestSubmit()
		return
	}
	cmdDraft = input.value = ''
	closePanel({ refocus: false })
	close()
	it.run()
}
input.addEventListener('keydown', e => {
	if (!pill.classList.contains('is-cmd') && agent.key(e)) return
	if (!pill.classList.contains('is-cmd')) return
	if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); e.stopPropagation(); setActive(cmdActive + (e.key === 'ArrowDown' ? 1 : -1)) }
	else if (e.key === 'Enter') { e.preventDefault(); e.stopPropagation(); runOption($$('[role=option]', cmdList)[cmdActive]) }
})
function enterCommand() {
	// the pill turns into the palette's field: its tools step aside (FLIP) and
	// it widens to the palette's width, so the pane above it is the same object
	const before = level && !cmdOpening ? snapshot() : null
	if (bloomed) { bloomed.removeAttribute('data-bloom'); bloomed = null }
	cmdDraft = input.value
	input.value = ''
	pill.classList.add('is-cmd')
	applyShift()
	if (before) play(before); else fitGround()
	input.placeholder = 'Search pages and actions…'
	input.setAttribute('aria-controls', 'pill-cmd-list')
	input.setAttribute('aria-autocomplete', 'list')
	syncSend()
	filterCommand()
}
function leaveCommand() {
	if (!pill.classList.contains('is-cmd')) return
	const before = level ? snapshot() : null
	pill.classList.remove('is-cmd')
	if (before) requestAnimationFrame(() => { if (level) play(before) })
	input.removeAttribute('aria-activedescendant'); input.removeAttribute('aria-controls'); input.removeAttribute('aria-autocomplete')
	input.value = cmdDraft; cmdDraft = ''
	setMode(pill.dataset.mode, { quiet: true })
	syncSend()
}
/* Ctrl/⌘ K (or the Search chip, or the workspace menu): toggles the palette */
function command(want) {
	const on = pill.classList.contains('is-cmd') && panel.dataset.open
	if (want === undefined) want = !on
	if (!want) { if (on) { closePanel({ refocus: !cmdFromRest }); if (cmdFromRest) close() } return }
	if (on) { input.focus({ preventScroll: true }); return }
	cmdFromRest = level === 0
	if (level === 0) {
		open(); cmdOpening = 1; enterCommand(); cmdOpening = 0; ground.w.to(rowWidth(), { k: K() * 0.6, c: 2 * Math.sqrt(K() * 0.6) })
		/* J6: from rest the palette's ground is opaque from its first frame and
		   its field is already there: the ground only WIDENS (the row's clip
		   uncovers the field as it goes), so no frame shows an empty or
		   translucent box. The Esc key sits at the far end, where the widening
		   edge would cut it: it arrives once the ground has passed it */
		if (!reduced.matches) {
			cancelLater()
			ground.g.o.jump(1); ground.g.s.jump(1)
			for (const el of parts()) {
				const m = M(el); m.x.jump(0); m.s.jump(1); m.b.jump(0)
				if (el.classList.contains('pill-esc')) { m.o.jump(0); later(() => m.o.to(1, { k: 520, c: 46 }), 170) } else m.o.jump(1)
			}
			ground.render()
		}
	}
	else enterCommand()
	openPanel(cmdChip)
	setRoving(input); input.focus({ preventScroll: true })
}

/* ── PINS: an answer, pinned, flies from its bubble into an #aside card in
   the inspector fused to the sheet's right edge. Agent notes (the #aside
   Patch Region) and pinned answers are one list; the selected card sits on
   a tab of sheet with mirrored flares that glides between cards on the tab's
   spring. Pins are article[data-pin] children of #aside, stored only in this
   browser (localStorage), never sent; a stream re-send that replaces #aside
   gets them back at once (the observer below). ─────────────────────────── */
const PIN_KEY = 'shell-pins', PIN_MAX = 8
const aside = $('#aside'), pinTab = $('.pin-tab'), insp = $('#region-detail'), inspScroll = $('.detail-inner', insp)
let pins = (() => { try { return JSON.parse(localStorage.getItem(PIN_KEY) || '[]') || [] } catch { return [] } })()
let selected = null   // the selected card (an element of #aside)
const savePins = () => { try { localStorage.setItem(PIN_KEY, JSON.stringify(pins.slice(0, PIN_MAX))) } catch {} }
/* response / damping → k, c (the deck's parameterisation, so both files speak one language) */
const springOf = (response, damping) => ({ k: (2 * Math.PI / response) ** 2, c: 4 * Math.PI * damping / response })
const TAB = springOf(0.30, 0.69), FLY_X = springOf(0.40, 0.95), FLY_Y = springOf(0.46, 0.9), FLY_S = springOf(0.42, 1)
const cards = () => [...aside.children].filter(c => !c.hasAttribute('data-deck-ghost') && !c.hasAttribute('data-deck-leaving'))
function pinEl(p) {
	const el = document.createElement('article')
	el.className = 'pin'; el.dataset.pin = p.id
	el.setAttribute('data-no-ghost', '')
	el.innerHTML = '<b></b><p></p><button type="button" class="pin-x"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M18 6 6 18M6 6l12 12"/></svg></button>'
	el.querySelector('b').textContent = p.q
	el.querySelector('p').textContent = p.a
	el.setAttribute('aria-label', 'Pinned answer: ' + p.q)
	el.querySelector('.pin-x').setAttribute('aria-label', 'Unpin “' + p.q + '”')
	return el
}
let rendering = false
function renderPins() {
	rendering = true
	const sel = selected?.dataset.pin
	for (const el of aside.querySelectorAll(':scope > [data-pin]')) el.remove()
	aside.prepend(...pins.map(pinEl))
	if (sel) selected = aside.querySelector(`:scope > [data-pin="${sel}"]`)
	rendering = false
	body.classList.toggle('has-pins', pins.length > 0)
	markCards()
}
/* every card is a stop; exactly one is current (the first, until one is chosen) */
function markCards() {
	const list = cards()
	if (!list.includes(selected)) selected = list[0] || null
	for (const c of list) { c.tabIndex = 0; c.setAttribute('aria-current', String(c === selected)) }
}
const tabOwner = { render() { placePinTab() } }
tabOwner.y = new Spring(0, tabOwner, 0.05)
let tabShown = false
function pinTarget() {
	const c = selected
	if (!c || !c.isConnected || !c.getClientRects().length) return null
	return { y: aside.offsetTop + c.offsetTop, h: c.offsetHeight }
}
function placePinTab() {
	const y = tabOwner.y.x
	pinTab.style.translate = `0 ${y.toFixed(2)}px`
	const ir = insp.getBoundingClientRect(), sr = inspScroll.getBoundingClientRect()
	const iy = sr.top - ir.top + y - inspScroll.scrollTop, vy = ir.top + iy, h = parseFloat(body.style.getPropertyValue('--pin-h')) || 0
	insp.style.setProperty('--pin-iy', iy.toFixed(2) + 'px')
	body.style.setProperty('--pin-vy', vy.toFixed(2) + 'px')
	const seen = vy + h > sr.top + 4 && vy < sr.bottom - 4
	body.classList.toggle('pin-tab-on', tabShown && seen)
	body.classList.toggle('pin-flare-off', vy - 12 < ir.top + 14 || vy + h + 12 > ir.bottom - 14 || vy < sr.top || vy + h > sr.bottom)
}
function selectCard(el, instant = false) {
	selected = el
	markCards()
	retabPins(instant)
}
let landing = false   // a pin is in flight: its tab appears only on contact
function retabPins(instant = false) {
	const open = body.classList.contains('insp') || body.classList.contains('insp-hold')
	const t = open && !landing ? pinTarget() : null
	if (!t) { tabShown = false; body.classList.remove('pin-tab-on'); return }
	body.style.setProperty('--pin-h', t.h + 'px')
	if (instant || !tabShown || !body.classList.contains('live')) tabOwner.y.jump(t.y)
	else tabOwner.y.to(t.y, TAB)
	tabShown = true
	placePinTab()
}
aside.addEventListener('click', e => {
	const c = e.target.closest('#aside > *'); if (!c || c.hasAttribute('data-deck-ghost')) return
	if (e.target.closest('.pin-x')) return unpin(c.dataset.pin)
	if (e.target.closest('a, button, input, select, textarea')) return
	selectCard(c)
})
aside.addEventListener('keydown', e => {
	const c = e.target; if (c.parentElement !== aside) return
	if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); selectCard(c) }
	else if ((e.key === 'Delete' || e.key === 'Backspace') && c.dataset.pin) { e.preventDefault(); const n = c.nextElementSibling || c.previousElementSibling; unpin(c.dataset.pin); n?.focus() }
	else if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); (e.key === 'ArrowDown' ? c.nextElementSibling : c.previousElementSibling)?.focus() }
})
function unpin(id) {
	const el = aside.querySelector(`:scope > [data-pin="${id}"]`)
	const commit = () => {
		pins = pins.filter(p => p.id !== id); savePins()
		if (selected === el) selected = null
		renderPins(); retabPins()
		if (!pins.length) window.deck?.syncInspector?.(false, 'unpin')
	}
	if (!el || reduced.matches) return commit()
	/* the last card leaves WITH its piece of sheet: the tab and its flares fade
	   on the card's own clock, so the closing column never shows an empty card */
	const last = ![...aside.children].some(c => c !== el && !c.hasAttribute('data-deck-ghost'))
	if (last || selected === el) { tabShown = false; body.classList.remove('pin-tab-on') }
	/* the last card never leaves an empty column behind: the column starts
	   closing on this frame (the card no longer counts as content) and the
	   card fades on the first stretch of that same closing, riding the edge */
	if (last) { el.setAttribute('data-deck-leaving', ''); window.deck?.syncInspector?.(false, 'unpin') }
	el.style.transition = 'none'
	el.animate([{ opacity: 1 }, { opacity: 0 }], { duration: last ? 120 : 140, easing: 'cubic-bezier(.32, .72, 0, 1)', fill: 'forwards' }).finished.then(commit, commit)
}
/* the flight: the bubble lifts off on the same frame the curtain starts. Its
   x and y ride separate springs (y a touch slower, so the path arcs), and
   its BOX morphs to the card's slot: width and height (never a scale, so no
   glyph is ever drawn smaller), the corners from the bubble's to the tab's,
   the ground from the pill's to the sheet's. The bubble's words leave in the
   first fifth of the flight; what lands is the slot itself, which becomes the
   card's tab in the same frame, and only then do the card's words appear:
   one layer of text at a time, never two, never a ghost. */
function pinBubble(b) {
	const a = b.dataset.answer
	if (!a || b.hasAttribute('data-leaving')) return
	const q = (b.querySelector('.pill-q')?.textContent || '').replace(/^“|”$/g, '')
	const p = { id: 'pin-' + Date.now().toString(36), q, a, mode: b.dataset.mode || 'ask' }
	pins.unshift(p); pins = pins.slice(0, PIN_MAX); savePins()
	const src = b.getBoundingClientRect()
	landing = !reduced.matches && body.classList.contains('live')
	renderPins()
	const el = aside.querySelector(`:scope > [data-pin="${p.id}"]`)
	selected = el; markCards()
	el.style.transition = 'none'; el.style.opacity = '0'
	window.deck?.syncInspector?.()        // the curtain starts now; the inspector's layout is final already
	// (the card's tab is placed on the next frame by deck:inspector: reading it
	// here would force one more full layout before the flight's first frame)
	if (!body.classList.contains('live')) retabPins(true)
	const fl = b.cloneNode(true)
	fl.removeAttribute('id'); for (const n of fl.querySelectorAll('[id]')) n.removeAttribute('id')
	fl.className = 'pill-bubble pin-flyer'; fl.setAttribute('aria-hidden', 'true'); fl.inert = true
	/* the words keep the bubble's measure while the box changes round them */
	const words = document.createElement('div'); words.className = 'pin-flyer-words'
	words.append(...fl.childNodes); fl.append(words)
	words.style.inlineSize = Math.max(0, src.width - 28) + 'px'
	Object.assign(fl.style, { left: src.left + 'px', top: src.top + 'px', width: src.width + 'px', height: src.height + 'px' })
	/* the card's own words ride in the flyer too, set as the card sets them,
	   pinned to the corner where the card's box lands: as the bubble's words
	   leave, the card's arrive, so the flyer always carries the answer and
	   what lands is the finished card */
	const cw = document.createElement('div'); cw.className = 'pin-flyer-card'
	cw.innerHTML = '<b></b><p></p>'
	cw.querySelector('b').textContent = p.q
	cw.querySelector('p').textContent = p.a
	cw.style.opacity = '0'
	fl.append(cw)
	document.body.append(fl)
	b.setAttribute('data-no-ghost', ''); b.remove()
	/* (K2) the flyer rests on the card's place until the page's content (the
	   inspector's column with it) has faded back in beneath it, then it hands
	   over to the identical card: the card arrives with the content */
	const land = () => window.deck?.idle ? window.deck.idle().then(handOver) : handOver()
	const handOver = () => {
		landing = false
		/* the slot becomes the tab in this frame: no fade on the tab or flares */
		const quiet = [pinTab, ...document.querySelectorAll('.pin-flare')]
		for (const n of quiet) n.style.transition = 'none'
		retabPins(true)
		const carried = +cw.style.opacity > 0.98
		const done = () => { el.style.transition = ''; requestAnimationFrame(() => requestAnimationFrame(() => { for (const n of quiet) n.style.transition = '' })) }
		if (!carried) {
			fl.remove()
			el.style.opacity = ''
			el.animate([{ opacity: 0 }, { opacity: 1 }], { duration: reduced.matches ? 120 : 160, easing: 'cubic-bezier(.23, 1, .32, 1)' }).finished.then(done, done)
			return
		}
		/* (J3/K2) the card is shown at once, UNDER the flyer, at full opacity
		   (its region's 160ms-delayed condense transition stays off: that was
		   the empty slot), then the flyer (the same words, on the same
		   ground) fades off it over the content's own 140ms. Its box is put on
		   the card's laid-out box first, read now that the content has landed,
		   so the two coincide and nothing shifts as it fades */
		el.style.opacity = ''
		const r = el.getBoundingClientRect(), f0 = fl.getBoundingClientRect()
		if (Math.abs(r.left - f0.left) > 0.5 || Math.abs(r.top - f0.top) > 0.5 || Math.abs(r.width - f0.width) > 0.5) {
			const [tx, ty] = (fl.style.translate || '0px 0px').split(' ').map(v => parseFloat(v) || 0)
			fl.style.translate = `${(tx + r.left - f0.left).toFixed(2)}px ${(ty + r.top - f0.top).toFixed(2)}px`
			fl.style.width = r.width + 'px'; fl.style.height = r.height + 'px'; cw.style.width = r.width + 'px'
		}
		fl.animate([{ opacity: 1 }, { opacity: 0 }], { duration: reduced.matches ? 100 : 140, easing: 'cubic-bezier(.23, 1, .32, 1)', fill: 'forwards' })
			.finished.then(() => { fl.remove(); done() }, () => { fl.remove(); done() })
	}
	if (reduced.matches || !body.classList.contains('live')) {
		/* nothing travels: the bubble leaves, THEN the card appears (no overlap) */
		fl.animate([{ opacity: 1 }, { opacity: 0 }], { duration: 90, easing: 'linear', fill: 'forwards' }).finished.then(land, land)
		close()
		return
	}
	/* read once: the card's RESTING box (less how far the sliding inspector
	   still sits from its rest): the flyer aims at where the card will be */
	const r0 = el.getBoundingClientRect(), dst = { left: r0.left - (window.deck?.inspShift?.() || 0), top: r0.top, width: r0.width, height: r0.height }
	cw.style.width = r0.width + 'px'
	const clamp = v => Math.max(0, Math.min(1, v))
	const tabR = parseFloat(getComputedStyle(body).getPropertyValue('--tab-r')) || 9
	const f = { render() {
		const tx = this.x.x, ty = this.y.x, ts = this.s.x
		fl.style.translate = `${((dst.left - src.left) * tx).toFixed(2)}px ${((dst.top - src.top) * ty).toFixed(2)}px`
		fl.style.width = (src.width + (dst.width - src.width) * ts).toFixed(2) + 'px'
		fl.style.height = (src.height + (dst.height - src.height) * ts).toFixed(2) + 'px'
		const rl = 18 * (1 - clamp(ts)), rr = 18 + (tabR - 18) * clamp(ts)
		fl.style.borderRadius = `${rl.toFixed(1)}px ${rr.toFixed(1)}px ${rr.toFixed(1)}px ${rl.toFixed(1)}px`
		fl.style.background = `color-mix(in oklab, var(--sheet) ${(clamp(ts) * 100).toFixed(1)}%, var(--pill-ground))`
		/* the bubble's words leave over the first 30% of the morph, the card's
		   arrive over 22-55%: the box always carries the answer */
		const m = Math.max(tx, ty, ts)
		words.style.opacity = (1 - clamp(m / 0.3)).toFixed(3)
		cw.style.opacity = clamp((m - 0.22) / 0.33).toFixed(3)
		/* it rides ABOVE the cards on its way (an elevation that settles as it
		   lands), so crossing them never reads as a collision */
		const lift = 1 - clamp((ts - 0.7) / 0.3)
		fl.style.boxShadow = `0 ${(14 * lift).toFixed(1)}px ${(32 * lift).toFixed(1)}px -10px color-mix(in srgb, var(--elevation-color, #000) ${(34 * lift).toFixed(0)}%, transparent), 0 0 0 1px color-mix(in srgb, var(--rt-border) ${(100 * lift).toFixed(0)}%, transparent)`
	}, rested() { if (![this.x, this.y, this.s].some(s => active.has(s))) land() } }
	f.x = new Spring(0, f, 0.002); f.y = new Spring(0, f, 0.002); f.s = new Spring(0, f, 0.002)
	f.x.to(1, FLY_X); f.y.to(1, FLY_Y); f.s.to(1, FLY_S)
	close()
}
renderPins()
/* a re-send of #aside (a reconnect, or a producer's inner replace) drops the
   local pins: put them straight back; any change re-marks the cards */
new MutationObserver(() => {
	if (rendering) return
	const have = aside.querySelectorAll(':scope > [data-pin]').length
	if (have !== pins.length) renderPins(); else markCards()
	requestAnimationFrame(() => retabPins(true))
}).observe(aside, { childList: true })
addEventListener('deck:inspector', () => requestAnimationFrame(() => retabPins(true)))
inspScroll.addEventListener('scroll', () => placePinTab(), { passive: true })
addEventListener('resize', () => retabPins(true))
requestAnimationFrame(() => retabPins(true))
document.fonts?.ready.then(() => retabPins(true))

/* ── THE AGENT SLOT: #pill[data-agent] names a capability (the herdr console
   Plugin). The mode dot gives way to a session chip painted from the signal
   $pillAgent ({pane, name, kind, status, crew, folder}, or null); the chip or
   an "@" at the start of the prompt asks for the session picker ("open"); a
   prompt with a session goes out as "send" {pane, text}. The capability
   answers by Caller patches into #pill-console (Patch Region console): this
   file only posts actions. Its markup's [data-pill-call] controls post their
   JSON, a [data-pill-form] posts {action: <form name>, ...named inputs}. A
   failed call always leaves a visible error line in the console. Esc folds
   the console; while a session is chosen and the page is visible, "follow"
   renews the Plugin's Caller Ticket every 4 minutes. ─────────────────── */
var agent = (() => {
	const cap = pill.dataset.agent, version = pill.dataset.agentVersion || '1.0.0'
	const con = $('#pill-console'), chipEl = $('.pill-agent', row)
	const none = { on: false, chip() {}, typed() {}, submit() {}, key: () => false, covers: () => false, holds: () => false, collapse: () => false, placeholder: () => '', api: null }
	if (!cap || !con || !chipEl) { chipEl?.setAttribute('hidden', ''); return none }
	chipEl.hidden = false
	const FOLLOW_MS = 4 * 60 * 1000, PANE_KEY = 'pill-agent-pane'
	const ICON = {
		claude: '<path d="M12 3.2v4.2M12 16.6v4.2M3.2 12h4.2M16.6 12h4.2M5.8 5.8l3 3M15.2 15.2l3 3M18.2 5.8l-3 3M8.8 15.2l-3 3"/>',
		codex: '<path d="M12 2.8 20 7.4v9.2L12 21.2 4 16.6V7.4z"/><path d="m8.5 10 2.5 2-2.5 2M12.5 14.5h3"/>',
		pi: '<path d="M4.5 7.5h15M9 7.5v11M15.5 7.5v8.5c0 1.6.8 2.5 2.4 2.5"/>',
		gen: '<rect x="3" y="4.5" width="18" height="15" rx="3"/><path d="m7.5 10 2.5 2-2.5 2M12.5 14.5h4"/>',
	}
	const STATUS = new Set(['idle', 'working', 'blocked', 'done'])
	let current = null, painted = '', picking = false, lastCall = 0, inflight = 0
	const store = (k, v) => { try { v == null ? sessionStorage.removeItem(k) : sessionStorage.setItem(k, v) } catch {} }
	const stored = k => { try { return sessionStorage.getItem(k) } catch { return null } }
	const hadPane = stored(PANE_KEY)
	const expanded = () => con.children.length > 0 && !con.hasAttribute('data-collapsed')
	const placeholder = () => current ? 'Message ' + current.name + '…' : 'Type @ to pick a session'

	/* the chip: mark, name, status dot (herdr's colours), crew mark */
	function paint(a) {
		const key = JSON.stringify(a)
		if (key === painted) return
		painted = key
		const before = level ? snapshot() : null
		const prev = current
		current = a
		const kind = a ? (ICON[a.kind] ? a.kind : 'gen') : 'gen'
		const ico = $('.pill-agent-ico', chipEl)
		ico.dataset.kind = a ? a.kind || 'other' : 'other'
		ico.innerHTML = '<svg viewBox="0 0 24 24" aria-hidden="true">' + ICON[kind] + '</svg>'
		$('.pill-agent-name', chipEl).textContent = a ? a.name : ''
		$('.pill-agent-dot', chipEl).dataset.status = a ? a.status : 'unknown'
		chipEl.toggleAttribute('data-has', !!a)
		chipEl.toggleAttribute('data-crew', !!a?.crew)
		chipEl.setAttribute('aria-label', a ? 'Session ' + a.name + (a.crew ? ' (crew)' : '') + ', ' + a.status + '. Choose another session' : 'Choose a session')
		if (!pill.hasAttribute('data-voice') && !pill.classList.contains('is-cmd')) input.placeholder = placeholder()
		if (a) store(PANE_KEY, a.pane); else if (prev) store(PANE_KEY, null)
		if (before) play(before)
	}
	/* $pillAgent, read without creating it: the root's `in` and the object's
	   `in` track additions and deletions (a patch to null deletes the signal) */
	function readAgent(root) {
		if (!('pillAgent' in root)) return null
		const v = root.pillAgent
		if (!v || typeof v !== 'object' || !('pane' in v) || !v.pane) return null
		const f = k => (k in v ? v[k] : undefined)
		const status = String(f('status') || 'unknown')
		return { pane: String(v.pane), name: String(f('name') || v.pane), kind: String(f('kind') || 'other'), status: STATUS.has(status) ? status : 'unknown', crew: !!f('crew'), folder: String(f('folder') || '') }
	}
	paint(null)
	import('datastar').then(({ effect, root }) => effect(() => paint(readAgent(root))))
		.catch(err => showError('The session chip cannot read its signal: ' + (err?.message || err)))

	/* the console: open while it has content and is not folded; the pill
	   takes its width; #layer's notices step up over it */
	function sync() {
		const open = expanded()
		body.classList.toggle('hc-console-open', open)
		chipEl.setAttribute('aria-expanded', String(open && !!con.querySelector('.hc-picker')))
		con.toggleAttribute('data-narrow', pill.hasAttribute('data-narrow'))
		if (pill.classList.contains('is-agent-wide') !== open) {
			const before = level ? snapshot() : null
			pill.classList.toggle('is-agent-wide', open)
			if (before) play(before); else if (level) fitGround()
		}
		body.style.setProperty('--hc-console-h', (open ? con.offsetHeight : 0) + 'px')
	}
	function expand() { if (con.hasAttribute('data-collapsed')) { con.removeAttribute('data-collapsed'); sync() } }
	function collapse() {
		if (!expanded()) return false
		const had = con.contains(document.activeElement)
		con.setAttribute('data-collapsed', '')
		if (picking) { picking = false; if (input.value.startsWith('@')) { input.value = ''; syncSend() } }
		query = ''
		sync()
		// focus in the folded console would be lost: it goes back to the prompt
		if (had && level) input.focus({ preventScroll: true })
		return true
	}
	new ResizeObserver(() => sync()).observe(con)
	new MutationObserver(() => sync()).observe(pill, { attributes: true, attributeFilter: ['data-narrow'] })
	/* a thread stays on its newest line while you have not scrolled up */
	const stick = new WeakMap()
	con.addEventListener('scroll', e => { const t = e.target; if (t.classList?.contains('hc-thread')) stick.set(t, t.scrollHeight - t.scrollTop - t.clientHeight < 40) }, { capture: true, passive: true })
	new MutationObserver(() => {
		for (const t of con.querySelectorAll('.hc-thread')) if (stick.get(t) !== false) t.scrollTop = t.scrollHeight
		if (con.querySelector('.hc-picker')) {
			filter()
			if (wantFind) { wantFind = false; finder()?.focus({ preventScroll: true }) }
		}
		sync()
	}).observe(con, { childList: true, subtree: true, characterData: true })

	/* a call: POST /api/capabilities {id, version, input}, same origin. A
	   failure is shown in the console, never swallowed */
		async function call(inp, quiet = false, background = false, sink = null) {
		// a background call (a follow the page makes on its own) leaves a folded console folded
		if (!background) expand()
		inflight++; con.setAttribute('aria-busy', 'true'); chipEl.setAttribute('data-busy', '')
		try {
			const r = await fetch('/api/capabilities', { method: 'POST', credentials: 'include', headers: { 'Content-Type': 'application/json', Accept: 'application/json' }, body: JSON.stringify({ id: cap, version, input: inp }) })
			let out = null
			try { out = await r.json() } catch {}
			if (!r.ok || !out || out.ok === false) throw new Error(faultText(r, out))
			lastCall = Date.now()
			clearError()
			return { ok: true, result: out.result }
		} catch (err) {
						if (!quiet) (sink || showError)(err?.message || String(err), inp)
						return { ok: false }
		} finally {
			if (!--inflight) { con.removeAttribute('aria-busy'); chipEl.removeAttribute('data-busy') }
		}
	}
	function faultText(r, out) {
		const f = out?.fault || {}
		if (r.status === 401 || f.reason === 'sign_in_required' || /authenticated .*session required/i.test(f.message || '')) return 'Sign in to this Workspace to talk to your sessions.'
		const msg = f.message || (r.ok ? 'the answer was not understood' : 'HTTP ' + r.status)
		if (f.code === 'not_found' && /not registered|no capability|unknown capability/i.test(f.message || '')) return 'The console Plugin is not installed in this Workspace: ' + cap + ' ' + version + ' is not registered [' + f.code + '].'
		return 'The console (' + cap + ') could not answer: ' + msg + (f.code ? ' [' + f.code + ']' : '')
	}
	function clearError() { for (const e of con.querySelectorAll('.hc-error[data-pill-local]')) e.remove() }
	function showError(text, inp) {
		clearError()
		const e = document.createElement('div')
		e.className = 'hc-error'; e.dataset.pillLocal = ''; e.setAttribute('role', 'alert')
		e.innerHTML = '<span></span><button type="button" data-pill-local-dismiss aria-label="Dismiss the error">Dismiss</button>'
		e.firstChild.textContent = text + (inp?.action ? ' (action: ' + inp.action + ')' : '')
		con.append(e)
		con.removeAttribute('data-collapsed')
		sync()
	}
	/* the named inputs of a [data-pill-form] (radios and checkboxes only when checked) */
	function fields(el) {
		const out = {}
		for (const f of el.querySelectorAll('[name]')) {
			if (f.disabled || !('value' in f)) continue
			if ((f.type === 'radio' || f.type === 'checkbox') && !f.checked) continue
			const v = f.type === 'checkbox' && f.value === 'on' ? true : f.value
			if (f.name in out) out[f.name] = [].concat(out[f.name], v); else out[f.name] = v
		}
		return out
	}
		function post(el, trusted, { background = false, sink = null } = {}) {
			let inp
			try { inp = JSON.parse(el.dataset.pillCall) } catch { inp = null }
			const fail = sink || showError
			if (!inp || typeof inp !== 'object' || !inp.action) { fail('This control carries no valid call (data-pill-call must be JSON with an action).'); return }
			// a confirmed send or key press goes only on a person's own click or Enter
			if (inp.confirm === true && !trusted) { fail('A confirmation needs your own click or Enter.', inp); return }
			if (inp.action === 'select' || inp.action === 'new') { picking = false; if (input.value.startsWith('@')) { input.value = ''; syncSend() } }
			return call(inp, false, background, sink).then(r => { if (r.ok && inp.action === 'select' && level) input.focus({ preventScroll: true }); return r })
		}
	con.addEventListener('click', e => {
		if (e.target.closest('[data-pill-local-dismiss]')) { clearError(); sync(); return }
		const c = e.target.closest('[data-pill-call]')
		if (c && con.contains(c)) { e.preventDefault(); post(c, e.isTrusted); return }
		// a submit button of a [data-pill-form] that is not a <form>
		const sb = e.target.closest('button[type="submit"], button:not([type])')
		const holder = sb?.closest('[data-pill-form]')
		if (holder && holder.tagName !== 'FORM') { e.preventDefault(); call({ ...fields(holder), action: holder.dataset.pillForm }) }
	})
	con.addEventListener('submit', e => {
		const f = e.target.closest('[data-pill-form]')
		if (!f) return
		e.preventDefault()
		call({ ...fields(f), action: f.dataset.pillForm })
	})
	/* ↑ ↓ between the picker's rows when one has focus */
	con.addEventListener('keydown', e => {
		if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
		const list = rows(true), i = list.indexOf(document.activeElement)
		if (i < 0) return
		e.preventDefault()
		list[(i + (e.key === 'ArrowDown' ? 1 : -1) + list.length) % list.length]?.focus({ preventScroll: true })
	})

		/* THE ONE FILTER, shared by the picker (its search row and "@text") and
		   the Agents sidebar (its Find field): a row matches by its data-search
		   (name and folder, written by the Plugin) or else its text; a list with
		   rows but no match says so in a local line of its own class */
		function matchRows(list, q) {
			let shown = 0
			for (const r of list) {
				r.hidden = !!q && !(r.dataset.search || r.textContent).toLowerCase().includes(q)
				if (!r.hidden) shown++
			}
			return shown
		}
		function noMatch(list, cls, none, q) {
			let note = list?.querySelector(`.${cls}[data-pill-local]`)
			if (list && none) {
				if (!note) { note = document.createElement('p'); note.className = cls; note.dataset.pillLocal = ''; list.append(note) }
				const t = 'No session matches “' + q + '”.'
				if (note.textContent !== t) note.textContent = t
			} else note?.remove()
		}
		/* "@name" in the prompt filters the picker; ↑ ↓ Enter choose */
	/* One filter for the picker: the search row at its top ("Find a session
	   or folder") and "@text" in the prompt write the same query, and each
	   shows it. Rows match by name or folder (their data-search). */
	const rows = (withNew = false) => [...con.querySelectorAll(withNew ? '.hc-row, .hc-new' : '.hc-row')].filter(r => !r.hidden && r.getClientRects().length)
	const finder = () => con.querySelector('.hc-picker .hc-find')
	let active = 0, query = '', wantFind = false
		function filter() {
			const q = query
			const find = finder()
			if (find && find.value.trim().toLowerCase() !== q && document.activeElement !== find) find.value = q
			const shown = matchRows(con.querySelectorAll('.hc-row'), q)
		for (const g of con.querySelectorAll('.hc-group')) {
			let n = g.nextElementSibling, any = false
			while (n && !n.classList.contains('hc-group')) { if (n.classList.contains('hc-row') ? !n.hidden : n.querySelector?.('.hc-row:not([hidden])')) { any = true; break } n = n.nextElementSibling }
			g.hidden = !any
		}
				// nothing matches: say so in the list (a server "none running" line stays as it is)
				noMatch(con.querySelector('.hc-picker .hc-list'), 'hc-empty', !!q && !shown && !!con.querySelector('.hc-row'), q)
		if (q || picking || document.activeElement === find) mark(0)
		else for (const r of con.querySelectorAll('[data-hc-active]')) r.removeAttribute('data-hc-active')
	}
	// the search row: typing filters (and an "@" prompt follows it); ↑ ↓ Enter choose
	con.addEventListener('input', e => {
		if (!e.target.classList?.contains('hc-find')) return
		query = e.target.value.trim().toLowerCase()
		if (picking && input.value.startsWith('@')) { input.value = '@' + e.target.value; syncSend() }
		filter()
	})
	con.addEventListener('keydown', e => {
		if (!e.target.classList?.contains('hc-find')) return
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); e.stopPropagation(); mark(active + (e.key === 'ArrowDown' ? 1 : -1)) }
		else if (e.key === 'Enter') { e.preventDefault(); const r = rows()[active] || rows()[0]; if (r) post(r, e.isTrusted) }
	}, true)
	function mark(i) {
		const list = rows()
		for (const r of con.querySelectorAll('[data-hc-active]')) r.removeAttribute('data-hc-active')
		if (!list.length) return
		active = (i + list.length) % list.length
		list[active].setAttribute('data-hc-active', '')
		list[active].scrollIntoView({ block: 'nearest' })
	}
	function typed() {
		const at = input.value.startsWith('@')
		if (at) query = input.value.slice(1).trim().toLowerCase()
		if (at && !picking) { picking = true; call({ action: 'open' }) }
		else if (!at && picking) { picking = false; query = ''; filter() }
		else if (at) filter()
	}
	function key(e) {
		if (!picking || !expanded()) return false
		if (e.key === 'ArrowDown' || e.key === 'ArrowUp') { e.preventDefault(); mark(active + (e.key === 'ArrowDown' ? 1 : -1)); return true }
		if (e.key === 'Enter') { e.preventDefault(); const r = rows()[active] || rows()[0]; if (r) post(r, e.isTrusted); return true }
		return false
	}
	function submit(q, trusted) {
		if (q.startsWith('@')) { const r = rows()[active] || rows()[0]; if (r) post(r, trusted); else call({ action: 'open' }); return }
		if (!q) {
			// a second Enter on an empty prompt delivers what the red confirm bar holds
			const c = [...con.querySelectorAll('.hc-confirm [data-pill-call]')].find(b => { try { return JSON.parse(b.dataset.pillCall).confirm === true } catch { return false } })
			if (c) post(c, trusted)
			return
		}
		if (!current) { picking = false; call({ action: 'open' }); showHint(); return }
		input.value = ''; syncSend()
		call({ action: 'send', pane: current.pane, text: q }).then(r => { if (!r.ok && !input.value) { input.value = q; syncSend() } })
	}
	function showHint() { input.placeholder = 'Pick a session first: your message is kept'; }
	function chip() {
		if (expanded() && con.querySelector('.hc-picker')) { collapse(); return }
		// a fresh picker: no filter yet; on a desktop the search row takes focus
		if (!picking) query = ''
		wantFind = !pill.hasAttribute('data-narrow')
		call({ action: 'open' })
	}

	/* ── THE AGENTS SIDEBAR (#panel-agents, Alt+5). Its body is the personal
	   Patch Region agents (#agents-panel), filled by the same capability:
	   "agents" asks for it when the panel opens (and on every follow while it
	   stays open, which renews the Caller Ticket its live pushes need). This
	   file only: posts its [data-pill-call] controls (a row "select" attaches
	   the session and opens the pill on its thread; "＋ New" opens the
	   new-session form in the pill), filters its rows with the Find field
	   (the picker's filter), keeps the groups a reader folded folded across
	   re-renders, keeps focus on the same control when a re-render replaces
	   it, counts the rows into the head, and paints the ribbon's badge from
	   $herdrBadge ("blocked" | "done" | null). ─────────────────────────── */
	const agents = (() => {
		const ap = $('#agents-panel'), sec = $('#panel-agents'), btn = $('.ribbon-btn[data-panel="agents"]')
		const sub = $('#panel-agents-sub'), wait = $('#agents-wait'), err = $('#agents-error')
		const sbEl = $('#shell-sidebar'), navEl = $('#region-nav')
		const off = { open: () => false, load: () => {}, since: () => 0 }
		if (!ap || !sec) return off

		/* the badge: red while a session is blocked, blue while one is done */
		function badge(v) {
			if (!btn) return
			const s = v === 'blocked' || v === 'done' ? v : ''
			if ((btn.dataset.badge || '') === s) return
			if (s) btn.dataset.badge = s; else delete btn.dataset.badge
			const i = btn.querySelector('.ribbon-badge'); if (i) i.hidden = !s
			btn.setAttribute('aria-label', 'Agents' + (s === 'blocked' ? ', a session is blocked' : s === 'done' ? ', a session is done' : ''))
		}
		import('datastar').then(({ effect, root }) => effect(() => badge('herdrBadge' in root ? root.herdrBadge : null))).catch(() => {})

		/* open = the active panel, on a sidebar that is showing */
		const shown = () => sec.hasAttribute('data-active') && (typeof sbEl?.expanded === 'boolean' ? sbEl.expanded : sbEl?.getAttribute('data-state') !== 'collapsed')
		function showErr(text) { err.querySelector('span').textContent = text; err.hidden = false; tidy() }
		let loadedAt = 0
		const load = () => { loadedAt = Date.now(); return call({ action: 'agents' }, false, true, showErr).then(r => { if (r.ok) { err.hidden = true; tidy() } return r }) }
		let was = false, t = 0
		function check() {
			clearTimeout(t)
			t = setTimeout(() => { const o = shown(); if (o && !was) load(); was = o }, 30)
		}
		new MutationObserver(check).observe(sec, { attributes: true, attributeFilter: ['data-active'] })
		if (sbEl) { new MutationObserver(check).observe(sbEl, { attributes: true, attributeFilter: ['data-state', 'data-layout'] }); sbEl.addEventListener('rt-sidebar-change', check) }
		if (navEl) new MutationObserver(check).observe(navEl, { attributes: true, attributeFilter: ['data-panel'] })
		setTimeout(check, 300)   // remembered open across a page change
		err.addEventListener('click', e => { if (e.target.closest('[data-agents-retry]')) { err.hidden = true; tidy(); load() } })

		/* the head's count, the waiting line */
		function tidy() {
			const n = ap.querySelectorAll('.ha-row').length, has = ap.children.length > 0
			const t = has ? n + (n === 1 ? ' session' : ' sessions') : ''
			if (sub && sub.textContent !== t) sub.textContent = t
			if (wait) wait.hidden = has || !err.hidden
		}

		/* the Find field: the shared filter; a folded group stays folded */
		let q = '', raw = ''
		const fold = new Map()   // group key → the reader's own choice (true = folded); else the Plugin's default
		// the Plugin's stable id (data-ws, the herdr workspace); the header's
		// title alone only for markup without one
		const keyOf = g => g.dataset.ws || g.dataset.group || (g.querySelector('.ha-group-h > span:not(.ha-count)')?.textContent || '').trim().toLowerCase()
		const finder = () => ap.querySelector('[data-ha-find]')
		function refilter() {
			const find = finder()
			if (find && document.activeElement !== find && find.value !== raw) find.value = raw
			const all = ap.querySelectorAll('.ha-row')
			const shown = matchRows(all, q)
			for (const g of ap.querySelectorAll('.ha-group')) {
				const any = !!g.querySelector('.ha-row:not([hidden])')
				g.hidden = !!q && !any
				const k = keyOf(g), mine = fold.get(k)
				if (g.tagName === 'DETAILS') {
					// the reader just clicked its header: the toggle is theirs
					if (g._reader) { delete g._reader; continue }
					// a filter opens the groups with a match; cleared, each group goes
					// back to the reader's choice, or else to how the Plugin sent it
					if (q && !('_def' in g)) g._def = g.open
					const want = q ? (any || g.open) : mine !== undefined ? !mine : '_def' in g ? g._def : null
					if (!q) delete g._def
					if (want !== null && g.open !== want) g.open = want
				} else {
					const folded = !q && (mine ?? g.hasAttribute('data-collapsed'))
					g.toggleAttribute('data-collapsed', folded)
					g.querySelector('button.ha-group-h')?.setAttribute('aria-expanded', String(!folded))
				}
			}
			// a close button beside its row (not inside it) follows the row
			for (const x of ap.querySelectorAll('.ha-row + .ha-close')) x.hidden = x.previousElementSibling.hidden
			noMatch(ap.querySelector('.ha-panel') || ap, 'ha-empty', !!q && !shown && all.length > 0, q)
			tidy()
		}
		ap.addEventListener('input', e => {
			if (!e.target.matches?.('[data-ha-find]')) return
			raw = e.target.value; q = raw.trim().toLowerCase()
			refilter()
		})
		// a group the reader folds (details, or a button header) stays folded.
		// A details group: the click on its summary is the reader's (it runs
		// before the toggle); every other change of open (a morph, our own
		// refilter) is not, so the toggle event is no evidence of intent
		ap.addEventListener('click', e => {
			const h = e.target.closest('summary.ha-group-h'), g = h?.parentElement
			if (!g?.classList.contains('ha-group') || !ap.contains(g)) return
			g._reader = true
			if (!q) fold.set(keyOf(g), g.open)   // open now = folded after this click
		}, true)

		/* focus survives a re-render: the same control (its call, or the Find
		   field) takes it back, caret and all */
		let focusKey = null, caret = null
		const keyOfEl = el => el?.matches?.('[data-ha-find]') ? 'find' : el?.dataset?.pillCall || null
		ap.addEventListener('focusin', e => { focusKey = keyOfEl(e.target) })
		// (a control a re-render removes may report a focusout as it goes: decide after the re-render)
		ap.addEventListener('focusout', e => { const el = e.target; setTimeout(() => { if (el.isConnected && !ap.contains(document.activeElement)) focusKey = null }, 0) })
		document.addEventListener('selectionchange', () => { const f = finder(); if (f && document.activeElement === f) caret = [f.selectionStart, f.selectionEnd] })
		function refocus() {
			if (!focusKey || (document.activeElement && document.activeElement !== document.body && ap.contains(document.activeElement))) return
			if (document.activeElement && document.activeElement !== document.body) { focusKey = null; return }
			const el = focusKey === 'find' ? finder() : [...ap.querySelectorAll('[data-pill-call]')].find(n => n.dataset.pillCall === focusKey)
			if (!el) return
			el.focus({ preventScroll: true })
			if (focusKey === 'find' && caret) try { el.setSelectionRange(caret[0], caret[1]) } catch {}
		}
		// a live patch of #ha-list is a Datastar morph: it may only change
		// attributes (a group's open back on, a filtered row's hidden off),
		// so those are watched too; the records our own writes leave are
		// dropped, or the observer would wake itself
		const mo = new MutationObserver(() => { refilter(); refocus(); mo.takeRecords() })
		mo.observe(ap, { childList: true, subtree: true, attributes: true, attributeFilter: ['open', 'hidden'] })

		/* clicks: the rows, "＋ New", a close ✕, a button group header */
		ap.addEventListener('click', e => {
			const h = e.target.closest('button.ha-group-h')
			if (h && ap.contains(h) && !h.matches('[data-pill-call]')) {
				const g = h.closest('.ha-group'); if (!g) return
				fold.set(keyOf(g), !g.hasAttribute('data-collapsed'))
				refilter(); return
			}
			const c = e.target.closest('[data-pill-call]')
			if (!c || !ap.contains(c)) return
			e.preventDefault()
			let inp = null; try { inp = JSON.parse(c.dataset.pillCall) } catch {}
			if (inp?.action === 'select' || inp?.action === 'new') {
				// to the pill: on a phone the drawer gives way first
				if (sbEl?.dataset.layout === 'mobile' && sbEl.expanded) sbEl.close?.()
				open({ focus: true })
				post(c, e.isTrusted)
			} else post(c, e.isTrusted, { background: true, sink: showErr })?.then(r => r?.ok && !err.hidden && (err.hidden = true, tidy()))
		})
		/* keys: ↓ from Find to the first row, ↑ from the first row back to
		   Find (deck.js moves between the rows), Enter in Find attaches the
		   first match, Esc in Find clears it */
		ap.addEventListener('keydown', e => {
			const f = finder()
			const rowsNow = () => [...ap.querySelectorAll('.ha-row')].filter(r => !r.hidden && r.checkVisibility())
			if (e.target === f) {
				if (e.key === 'ArrowDown') { const r = rowsNow()[0]; if (r) { e.preventDefault(); e.stopPropagation(); r.focus() } }
				else if (e.key === 'Enter' && !e.isComposing) { e.preventDefault(); const r = rowsNow()[0]; if (r) r.click() }
				else if (e.key === 'Escape' && f.value) { e.preventDefault(); e.stopPropagation(); f.value = ''; raw = q = ''; refilter() }
				return
			}
			// a row the Plugin draws as div[role=button] (its ✕ is nested inside, and a
			// button may not hold a button): Enter and Space attach it, as a button would
			if ((e.key === 'Enter' || e.key === ' ') && !e.isComposing && !e.repeat && e.target.matches?.('.ha-row:not(button)')) { e.preventDefault(); e.target.click(); return }
			if (e.key === 'ArrowUp' && f && e.target === rowsNow()[0]) { e.preventDefault(); e.stopPropagation(); f.focus() }
		})
		refilter()
		return { open: shown, load, since: () => Date.now() - loadedAt }
	})()

	/* "follow": every 4 minutes while a session is chosen and the page is
	   visible (each call carries a fresh Caller Ticket, so any call counts) */
	const follow = () => {
		if (document.hidden) return
		// the Agents sidebar, while it stays open: the Plugin keeps it live
		// only within one ticket's life of the last "agents" call, so that
		// call is renewed on its own clock, however busy the pill is
		if (agents.open() && agents.since() >= FOLLOW_MS) agents.load()
		if (Date.now() - lastCall < FOLLOW_MS) return
		if (current) call({ action: 'follow', pane: current.pane }, false, true)
	}
	setInterval(follow, 30 * 1000)
	/* a hidden tab's page Stream is closed, so it may have missed the
	   console's patches: shown again, "resync" has the Plugin render the
	   thread whole (after a moment, once the Stream is open again) */
	let wasHidden = document.hidden
	document.addEventListener('visibilitychange', () => {
		if (document.hidden) { wasHidden = true; return }
		// the open Agents sidebar may have missed its pushes too: render it again
		if (wasHidden && agents.open()) setTimeout(() => { if (!document.hidden && agents.open()) agents.load() }, 500)
		if (!wasHidden || !current) { follow(); return }
		wasHidden = false
		setTimeout(() => { if (current && !document.hidden) call({ action: 'follow', pane: current.pane, resync: true }, true, true) }, 500)
	})
	// a tab that had a session picks it up again after a page change or reload
	// it names the pane, so a Plugin that restarted (and lost its state) adopts it again
	// ("fresh": this page's console is empty, so the Plugin renders its next view whole)
	// (a Plugin older than "fresh" refuses the field: then the plain follow)
	if (hadPane) setTimeout(() => { if (!current) call({ action: 'follow', pane: hadPane, fresh: true }, true).then(r => { if (!r.ok && !current) call({ action: 'follow', pane: hadPane }) }) }, 400)

		return {
			on: true, chip, typed, submit, key, collapse, placeholder,
			covers: (x, y) => expanded() && within(x, y, con.getBoundingClientRect(), 12),
			// a press on an Agents sidebar control is the pill's own business (it opens it)
			holds: el => !!el && (con.contains(el) || !!el.closest?.('#agents-panel [data-pill-call]')),
		// no raw call here: page script reaches the console only through its controls
		api: { open: () => call({ action: 'open' }), state: () => current && { ...current }, collapse, expand },
	}
})()

window.pill = { open, close, focus: () => open({ focus: true }), prompt: t => dispatchEvent(new CustomEvent('pill:prompt', { detail: t })), command, rebuild: () => { if (pill.classList.contains('is-cmd')) filterCommand() }, pins: () => pins.slice(), level: 0, agent: agent.api }

