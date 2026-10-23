// chart.js — data-chart: an ApexCharts chart from a signal, in the theme.
//
//   <div data-chart="$c_status"></div>
//
// The expression returns a spec (usually a signal patched from the data
// plane); the plugin maps it to ApexCharts options, merges the Bento theme
// (tokens read from the live CSS custom properties, so light and dark both
// work and a theme toggle re-skins every chart), renders once, and updates
// in place whenever the signal changes. Cleanup destroys the chart.
// Put data-ignore-morph on the host: the page's hydration re-patch morphs
// #main, and without it the rendered SVG is stripped from under the chart
// (the plugin re-attaches and re-renders, but the flash is avoidable).
//
// Spec shape (everything optional except type + series):
//   type        'bar' | 'area' | 'line' | 'donut' | 'pie' | 'radialBar' | 'treemap' | 'heatmap'
//   series      Apex series: [{name, data:[…]}] for axis charts, [n, n] for radial ones
//   categories  x-axis labels for axis charts
//   labels      slice labels for radial charts
//   horizontal  bars run left→right
//   stacked     stack the series
//   colors      ['coral','mint','butter','violet','ink','fog'] or any CSS colour
//   highlight   index of the slot painted coral while the rest stay fog (single-series bars)
//   unit        suffix for values in tooltips and data labels ('€', 'px', '%')
//   height      px (default 240)
//   total       for donuts: the label under the centre figure ('contacts')
//   onBlack     true when the chart sits on a black card (inks flip to white)
//   apex        raw ApexCharts options merged last, for anything else
//
// OPT-IN. data-chart is not in the default plugin set: rt-chart (the UI
// library's own SVG chart) covers charts and costs nothing until a page uses
// it. plugins/index.js loads this file the first time an element with
// data-chart appears, and this file then loads ApexCharts 3.54.1 (MIT; 4+
// moved to a revenue-gated licence) once, from this workspace's own server:
// the binary serves the exact file (pinned by the Subresource Integrity
// below) at a hashed, immutable URL that the page's import map names
// "apexcharts" ({{.UI.ImportMap}}). A shell whose import map lacks that entry
// gets the same file at its unhashed path. No CDN is involved. A page that
// never uses data-chart downloads neither. A window.ApexCharts that already
// exists (say, a classic <script> of your own in the shell before
// plugins/index.js) is used as is.
import { attribute, effect } from "datastar";

const APEX = {
  specifier: "apexcharts",
  fallback: "/static/vendor/apexcharts/apexcharts.min.js",
  integrity: "sha384-KNaFJ+EK516RuHsoycvreec5pD7BkTKJEkjMrVSQWu9KGTl7En4dhIDv7t1DFJ+g",
};
// The import map's URL for ApexCharts; resolving an unmapped bare specifier
// throws, and then the unhashed path on the same server is used.
function apexURL() {
  try {
    return import.meta.resolve(APEX.specifier);
  } catch {
    return APEX.fallback;
  }
}
let apexPromise = null;
function loadApex() {
  if (typeof globalThis.ApexCharts === "function") return Promise.resolve(globalThis.ApexCharts);
  if (!apexPromise) {
    apexPromise = new Promise((resolve, reject) => {
      const s = document.createElement("script");
      s.src = apexURL();
      s.integrity = APEX.integrity;
      s.crossOrigin = "anonymous";
      s.onload = () => (typeof globalThis.ApexCharts === "function" ? resolve(globalThis.ApexCharts) : reject(new Error("ApexCharts did not define itself")));
      s.onerror = () => reject(new Error(`could not load ${s.src}`));
      document.head.append(s);
    }).catch((err) => {
      apexPromise = null;
      console.warn("[data-chart] ApexCharts unavailable:", err);
      throw err;
    });
  }
  return apexPromise;
}

const TOKEN = {
  coral: "--coral", mint: "--green", butter: "--yellow", violet: "--purple",
  ink: "--ink", fog: "--hover", card: "--card", well: "--card-inner",
};

function css(name, el = document.body) {
  return getComputedStyle(el).getPropertyValue(name).trim();
}
function fmt(unit) {
  return (v) => (v == null ? "" : unit === "%" ? `${v}%` : unit ? `${unit} ${Number(v).toLocaleString()}` : Number(v).toLocaleString());
}

function build(spec) {
  const type = spec.type || "bar";
  const radial = ["donut", "pie", "radialBar"].includes(type);
  // on a black card the inks flip: white text, muted white labels, the inner black as fog
  const onBlack = !!spec.onBlack;
  const ink = css(onBlack ? "--on-black" : "--ink"), ink3 = css(onBlack ? "--on-black-2" : "--ink-3"), line = css(onBlack ? "--on-black-line" : "--line");
  const cardBg = css(onBlack ? "--black" : "--card"), fogBg = css(onBlack ? "--black-inner" : "--hover");
  const colour = (c) => (c === "fog" ? fogBg : c === "card" ? cardBg : c === "ink" ? ink : TOKEN[c] ? css(TOKEN[c]) : c);
  const mono = css("--font-mono"), ui = css("--font-ui"), display = css("--font-hero");
  const unit = spec.unit || "";
  const f = fmt(unit);

  let colors;
  if (spec.colors) colors = spec.colors.map(colour);
  else if (radial) colors = ["coral", "mint", "butter", "violet"].map(colour);
  else colors = [colour("ink"), colour("coral"), colour("mint"), colour("butter"), colour("violet")];

  const opts = {
    chart: {
      type, height: spec.height || 240, fontFamily: ui, foreColor: ink3,
      toolbar: { show: false }, zoom: { enabled: false }, background: "transparent",
      animations: { enabled: !matchMedia("(prefers-reduced-motion: reduce)").matches, speed: 240, animateGradually: { enabled: false } },
      parentHeightOffset: 0, redrawOnParentResize: true,
    },
    colors,
    series: spec.series,
    ...(spec.labels ? { labels: spec.labels } : {}),
    stroke: radial ? { width: 3, colors: [cardBg] } : type === "area" || type === "line" ? { width: 2.5, curve: "smooth" } : { width: 0 },
    fill: type === "area" ? { type: "solid", opacity: 0.16 } : { opacity: 1 },
    dataLabels: { enabled: false },
    grid: {
      show: !radial, borderColor: line, strokeDashArray: 4,
      xaxis: { lines: { show: false } }, yaxis: { lines: { show: !spec.horizontal } },
      padding: { left: 4, right: 8, top: 0, bottom: 0 },
    },
    legend: {
      show: type === "donut" || type === "pie" || (spec.series?.length > 1), position: "bottom", horizontalAlign: "left",
      fontSize: "12px", fontWeight: 600, labels: { colors: ink }, markers: { size: 5, shape: "circle", strokeWidth: 0 }, itemMargin: { horizontal: 8, vertical: 4 },
    },
    tooltip: { theme: "dark", style: { fontSize: "12px", fontFamily: ui }, y: { formatter: f }, marker: { show: true } },
    states: { hover: { filter: { type: "none" } }, active: { filter: { type: "none" } } },
    plotOptions: {},
    xaxis: { ...(spec.categories ? { categories: spec.categories } : {}), axisBorder: { show: false }, axisTicks: { show: false },
      labels: { style: { fontSize: "11px", fontWeight: 500, colors: ink3 }, formatter: spec.horizontal ? f : undefined } },
    yaxis: { labels: { style: { fontSize: "11px", fontWeight: 500, colors: ink3 }, formatter: spec.horizontal ? undefined : f } },
  };

  if (type === "bar") {
    opts.plotOptions.bar = {
      horizontal: !!spec.horizontal, borderRadius: 8, borderRadiusApplication: "end",
      columnWidth: "56%", barHeight: "62%",
      distributed: spec.highlight != null && (spec.series?.length === 1),
    };
    opts.chart.stacked = !!spec.stacked;
    if (spec.highlight != null && spec.series?.length === 1) {
      const n = spec.series[0].data.length;
      opts.colors = Array.from({ length: n }, (_, i) => (i === spec.highlight ? colour("coral") : colour("fog")));
      opts.legend.show = false;
    }
    // value bubbles: the world's data labels, in the display face
    opts.dataLabels = {
      enabled: true, formatter: f, offsetY: spec.horizontal ? 0 : -18, offsetX: spec.horizontal ? 24 : 0,
      style: { fontFamily: display, fontSize: "15px", fontWeight: 800, colors: [ink] },
      background: { enabled: false }, dropShadow: { enabled: false },
      textAnchor: spec.horizontal ? "start" : "middle",
    };
    if (spec.stacked) opts.dataLabels.enabled = false;
    opts.plotOptions.bar.dataLabels = { position: "top" };
  }
  if (type === "donut" || type === "pie") {
    opts.plotOptions.pie = {
      expandOnClick: false,
      donut: {
        size: "72%",
        labels: {
          show: type === "donut",
          name: { show: true, fontSize: "12px", fontFamily: ui, fontWeight: 600, color: ink3, offsetY: 18 },
          value: { show: true, fontSize: "34px", fontFamily: display, fontWeight: 800, color: ink, offsetY: -12, formatter: f },
          total: { show: true, showAlways: true, label: spec.total || "total", fontSize: "12px", fontFamily: ui, fontWeight: 600, color: ink3, formatter: (w) => f(w.globals.seriesTotals.reduce((a, b) => a + b, 0)) },
        },
      },
    };
  }
  if (type === "radialBar") {
    opts.plotOptions.radialBar = {
      hollow: { size: "58%" }, track: { background: fogBg, strokeWidth: "100%", margin: 4 },
      dataLabels: {
        name: { show: true, fontSize: "12px", fontFamily: ui, fontWeight: 600, color: ink3, offsetY: 22 },
        value: { show: true, fontSize: "34px", fontFamily: display, fontWeight: 800, color: ink, offsetY: -12, formatter: (v) => `${Math.round(v)}%` },
      },
    };
    opts.stroke = { lineCap: "round" };
  }
  if (type === "treemap") {
    opts.plotOptions.treemap = { distributed: true, enableShades: false, borderRadius: 8 };
    opts.dataLabels = { enabled: true, style: { fontFamily: ui, fontSize: "12px", fontWeight: 600, colors: ["#0b0b0b"] } }; // marking fills read in carbon ink
    opts.stroke = { width: 3, colors: [cardBg] };
    opts.legend.show = false;
  }
  if (type === "heatmap") {
    opts.plotOptions.heatmap = { radius: 6, enableShades: true, shadeIntensity: 0.6, colorScale: { ranges: [] } };
    opts.stroke = { width: 3, colors: [cardBg] };
    opts.dataLabels = { enabled: false };
  }
  return deepMerge(opts, spec.apex || {});
}

function deepMerge(a, b) {
  for (const k of Object.keys(b)) {
    const v = b[k];
    if (v && typeof v === "object" && !Array.isArray(v) && a[k] && typeof a[k] === "object" && !Array.isArray(a[k])) deepMerge(a[k], v);
    else a[k] = v;
  }
  return a;
}

// every mounted chart re-skins when the theme flips (class toggle or OS)
const mounted = new Set();
function reskin() {
  for (const m of mounted) m.reskin();
}
new MutationObserver(reskin).observe(document.body, { attributes: true, attributeFilter: ["class"] });
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", reskin);

attribute({
  name: "chart",
  requirement: { key: "denied", value: "must" },
  returnsValue: true,
  apply({ el, rx, error }) {
    let chart = null;
    let gone = false;
    let last = null;
    const host = document.createElement("div");
    host.className = "chart-host";
    el.replaceChildren(host);
    const render = () => {
      let spec;
      try { spec = rx(); } catch (err) { console.warn("[data-chart] spec expression failed:", err); return; }
      if (!spec || typeof spec !== "object" || !spec.series) return;
      // signal objects have no prototype (Datastar builds them with Object.create(null));
      // ApexCharts calls hasOwnProperty on what it is given, so hand it plain JSON
      spec = JSON.parse(JSON.stringify(spec));
      last = spec;
      if (typeof globalThis.ApexCharts === "function") draw();
      else loadApex().then(() => { if (!gone) draw(); }, () => {});
    };
    // draw paints the latest spec; render (the effect) only reads it, so the
    // signal stays tracked while ApexCharts is still loading
    const draw = () => {
      if (!last) return;
      const opts = build(last);
      if (!chart) {
        chart = new globalThis.ApexCharts(host, opts);
        chart.render();
      } else {
        chart.updateOptions(opts, false, true);
      }
    };
    const entry = { reskin: () => { if (chart && last) chart.updateOptions(build(last), false, false); } };
    mounted.add(entry);
    const stop = effect(render);
    // a morph that removed the host (no data-ignore-morph) gets a fresh render
    const guard = new MutationObserver(() => {
      if (host.isConnected) return;
      if (chart) { chart.destroy(); chart = null; }
      el.replaceChildren(host);
      render();
    });
    guard.observe(el, { childList: true });
    return () => {
      gone = true;
      stop();
      guard.disconnect();
      mounted.delete(entry);
      if (chart) { chart.destroy(); chart = null; }
    };
  },
});
