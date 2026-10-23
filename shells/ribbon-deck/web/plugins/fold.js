// fold.js — fold records into chart specs, inside Datastar expressions.
//
// The publisher patches the raw records of a base as one page signal
// ($rows); filters are ordinary signals bound to inputs; every chart's
// data-chart expression folds the filtered rows into a spec, so a change
// to a filter or to the records re-renders the chart with no round trip.
//
//   data-computed:rows_f="fold.where($rows, {status: $f_status, owner: $f_owner, min: {mrr: $f_min}, q: $f_q})"
//   data-chart="fold.bar($rows_f, {by: 'company', sum: 'mrr', top: $f_top, horizontal: true, highlight: 0})"
//
// Loaded as a classic script in the head, BEFORE Datastar, so window.fold
// exists when the first expression runs; importable as a module too.

// (an IIFE: as a classic script, top-level `function top` would collide with window.top)
(() => {
const num = (v) => (typeof v === "number" ? v : Number(v) || 0);
const norm = (s) => String(s ?? "").toLowerCase();

// where(rows, {field: value | [values] | 'all', min: {field: n}, max: {field: n}, q: 'text'})
function where(rows, f = {}) {
  const list = Array.isArray(rows) ? rows : [];
  return list.filter((r) => {
    for (const [k, v] of Object.entries(f)) {
      if (k === "min") { for (const [fk, n] of Object.entries(v || {})) if (num(r[fk]) < num(n)) return false; continue; }
      if (k === "max") { for (const [fk, n] of Object.entries(v || {})) if (num(r[fk]) > num(n)) return false; continue; }
      if (k === "q") { const q = norm(v).trim(); if (q && !Object.values(r).some((x) => norm(x).includes(q))) return false; continue; }
      if (v == null || v === "all" || v === "") continue;
      if (Array.isArray(v)) { if (v.length && !v.includes(r[k])) return false; continue; }
      if (r[k] !== v) return false;
    }
    return true;
  });
}

function sum(rows, key) { return (rows || []).reduce((a, r) => a + num(r[key]), 0); }
function count(rows) { return (rows || []).length; }
function values(rows, key) { return [...new Set((rows || []).map((r) => r[key]).filter((v) => v != null && v !== ""))].sort(); }

// group(rows, by, measure) → [[key, value], …] sorted by value desc; measure = {sum: field} | {count: true}
function group(rows, by, m = { count: true }) {
  const acc = new Map();
  for (const r of rows || []) {
    const k = r[by] ?? "—";
    acc.set(k, (acc.get(k) || 0) + (m.sum ? num(r[m.sum]) : 1));
  }
  return [...acc.entries()].sort((a, b) => b[1] - a[1]);
}

const measure = (o) => (o.sum ? { sum: o.sum } : { count: true });
const name = (o) => o.name || (o.sum ? o.sum.toUpperCase() : "count");

// bar: one series, one slot per group value
function bar(rows, o) {
  let g = group(rows, o.by, measure(o));
  if (o.order) g = o.order.map((k) => [k, (g.find(([x]) => x === k) || [k, 0])[1]]);
  if (o.top) g = g.slice(0, num(o.top));
  const { by, sum: s, count: c, top, order, name: n, ...rest } = o;
  return { type: "bar", series: [{ name: name(o), data: g.map(([, v]) => v) }], categories: g.map(([k]) => String(k)), ...rest };
}

// stacked: one series per `stack` value, one column per `by` value
function stacked(rows, o) {
  const cols = o.order || values(rows, o.by);
  const stacks = o.stacks || values(rows, o.stack);
  const series = stacks.map((s) => ({
    name: String(s),
    data: cols.map((c) => (o.sum ? sum(where(rows, { [o.by]: c, [o.stack]: s }), o.sum) : count(where(rows, { [o.by]: c, [o.stack]: s })))),
  }));
  const { by, stack, stacks: st, order, sum: su, count: co, ...rest } = o;
  return { type: "bar", stacked: true, series, categories: cols.map(String), ...rest };
}

// donut: slices per group value (order pins the slice order so colours stay put)
function donut(rows, o) {
  let g = group(rows, o.by, measure(o));
  if (o.order) g = o.order.map((k) => [k, (g.find(([x]) => x === k) || [k, 0])[1]]);
  const { by, sum: s, count: c, order, ...rest } = o;
  return { type: "donut", labels: g.map(([k]) => String(k)), series: g.map(([, v]) => v), ...rest };
}

// treemap: one tile per group value, zero-weight values dropped
function treemap(rows, o) {
  const g = group(rows, o.by, measure(o)).filter(([, v]) => v > 0);
  const { by, sum: s, count: c, ...rest } = o;
  return { type: "treemap", series: [{ data: g.map(([k, v]) => ({ x: String(k), y: v })) }], ...rest };
}

// share: the percentage of rows where `field` > 0 (or matches `is`), as a radial
function share(rows, o) {
  const n = count(rows);
  const hit = (rows || []).filter((r) => (o.is !== undefined ? r[o.field] === o.is : num(r[o.field]) > 0)).length;
  const { field, is, label, ...rest } = o;
  return { type: "radialBar", labels: [label || field], series: [n ? Math.round((hit / n) * 100) : 0], ...rest };
}

// top: the leading group value and its measure → ['Ferrous Labs', 1250]
function top(rows, by, sumKey) {
  const g = group(rows, by, sumKey ? { sum: sumKey } : { count: true });
  return g[0] || ["—", 0];
}

globalThis.fold = { where, sum, count, values, group, bar, stacked, donut, treemap, share, top };
})();
