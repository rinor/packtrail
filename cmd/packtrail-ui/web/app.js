"use strict";

const $ = (sel) => document.querySelector(sel);
const state = { selected: null, flowCache: {} };

async function getJSON(url) {
  const r = await fetch(url);
  if (!r.ok) throw new Error(`${url}: ${r.status}`);
  return r.json();
}

// ---- execution list ---------------------------------------------------------

async function loadFlows() {
  const flows = (await getJSON("/api/flows")) || [];
  const sel = $("#flow-filter");
  for (const f of flows) {
    const opt = document.createElement("option");
    opt.value = opt.textContent = f;
    sel.appendChild(opt);
  }
}

async function refreshList() {
  const flow = $("#flow-filter").value;
  const status = $("#status-filter").value;
  let url = "/api/executions";
  if (status) url += "?status=" + encodeURIComponent(status);
  else if (flow) url += "?flow=" + encodeURIComponent(flow);
  let execs = (await getJSON(url)) || [];
  if (flow && status) execs = execs.filter((e) => e.flow === flow);
  execs.sort((a, b) => new Date(b.updated_at) - new Date(a.updated_at));

  const list = $("#exec-list");
  list.innerHTML = "";
  for (const e of execs) {
    const li = document.createElement("li");
    if (e.id === state.selected) li.classList.add("active");
    li.innerHTML = `<div class="row1"><span class="flow">${esc(e.flow)}</span>
      <span class="badge ${e.status}">${e.status}</span></div>
      <div class="id">${esc(e.id)}</div>`;
    li.onclick = () => selectExec(e.id);
    list.appendChild(li);
  }
}

// ---- execution detail -------------------------------------------------------

async function selectExec(id) {
  state.selected = id;
  refreshList();
  await renderDetail(id);
}

async function renderDetail(id) {
  let ex;
  try {
    ex = await getJSON("/api/executions/" + encodeURIComponent(id));
  } catch {
    $("#detail").innerHTML = `<p class="empty">Execution not found.</p>`;
    return;
  }
  const d = $("#detail");
  d.innerHTML = `
    <h2>${esc(ex.flow)} <span class="badge ${ex.status}">${ex.status}</span></h2>
    <div class="meta">${esc(ex.id)} · node: ${esc(ex.current_node || "—")} · attempt ${ex.attempt || 0}
      · updated ${new Date(ex.updated_at).toLocaleString()}</div>
    ${ex.error ? `<div class="err">⚠ ${esc(ex.error)}</div>` : ""}
    <section><h3>flow</h3><div id="graph-wrap"></div></section>
    <section><h3>payload</h3><pre>${esc(pretty(ex.payload))}</pre></section>
    ${ex.branches ? `<section><h3>branches</h3><pre>${esc(pretty(ex.branches))}</pre></section>` : ""}
    ${ex.signals ? `<section><h3>signals</h3><pre>${esc(pretty(ex.signals))}</pre></section>` : ""}
  `;
  const g = await loadFlow(ex.flow);
  if (g) $("#graph-wrap").appendChild(renderGraph(g, ex));
}

async function loadFlow(name) {
  if (state.flowCache[name]) return state.flowCache[name];
  try {
    const g = await getJSON("/api/flows/" + encodeURIComponent(name));
    state.flowCache[name] = g;
    return g;
  } catch {
    return null;
  }
}

// ---- flow graph (layered SVG) ----------------------------------------------

// derivedEdges expands routing implied by node type (choice rules, fanout
// branches, signal on_timeout) on top of explicit edges.
function derivedEdges(g) {
  const edges = (g.edges || []).map((e) => [e.from, e.to]);
  for (const n of g.nodes) {
    if (n.type === "choice") for (const r of n.rules || []) edges.push([n.id, r.to]);
    if (n.type === "fanout") for (const b of n.branches || []) edges.push([n.id, b]);
    if (n.type === "signal" && n.on_timeout) edges.push([n.id, n.on_timeout]);
  }
  return edges;
}

function layout(g) {
  const edges = derivedEdges(g);
  const indeg = {}, children = {};
  for (const n of g.nodes) { indeg[n.id] = 0; children[n.id] = []; }
  for (const [from, to] of edges) {
    if (!(to in indeg)) continue;
    indeg[to]++; if (children[from]) children[from].push(to);
  }
  // BFS depth from roots; cap iterations to tolerate cycles.
  const depth = {};
  let frontier = g.nodes.filter((n) => indeg[n.id] === 0).map((n) => n.id);
  if (frontier.length === 0 && g.nodes.length) frontier = [g.nodes[0].id];
  frontier.forEach((id) => (depth[id] = 0));
  let guard = 0;
  while (frontier.length && guard++ < 1000) {
    const next = [];
    for (const id of frontier)
      for (const c of children[id] || [])
        if (depth[c] === undefined || depth[c] < depth[id] + 1) { depth[c] = depth[id] + 1; next.push(c); }
    frontier = next;
  }
  g.nodes.forEach((n) => { if (depth[n.id] === undefined) depth[n.id] = 0; });

  const byDepth = {};
  for (const n of g.nodes) (byDepth[depth[n.id]] ||= []).push(n.id);
  const pos = {};
  const COLW = 200, ROWH = 80, NW = 150, NH = 46;
  for (const d of Object.keys(byDepth)) {
    byDepth[d].forEach((id, i) => { pos[id] = { x: 30 + d * COLW, y: 24 + i * ROWH }; });
  }
  const maxRows = Math.max(...Object.values(byDepth).map((a) => a.length), 1);
  const maxDepth = Math.max(...Object.keys(byDepth).map(Number), 0);
  return { edges, pos, NW, NH, COLW, width: 60 + (maxDepth + 1) * COLW, height: 48 + maxRows * ROWH };
}

function renderGraph(g, ex) {
  const L = layout(g);
  const svg = svgEl("svg", { class: "graph", viewBox: `0 0 ${L.width} ${L.height}`, height: Math.min(L.height, 520) });
  svg.appendChild(arrowDefs());

  for (const [from, to] of L.edges) {
    const a = L.pos[from], b = L.pos[to];
    if (!a || !b) continue;
    const x1 = a.x + L.NW, y1 = a.y + L.NH / 2, x2 = b.x, y2 = b.y + L.NH / 2;
    const mx = (x1 + x2) / 2;
    svg.appendChild(svgEl("path", {
      class: "gedge", "marker-end": "url(#arrow)",
      d: `M ${x1} ${y1} C ${mx} ${y1}, ${mx} ${y2}, ${x2} ${y2}`,
    }));
  }

  for (const n of g.nodes) {
    const p = L.pos[n.id];
    const current = n.id === ex.current_node;
    const cls = "gnode" + (current ? " current " + ex.status : "");
    const grp = svgEl("g", { class: cls, transform: `translate(${p.x},${p.y})` });
    grp.appendChild(svgEl("rect", { width: L.NW, height: L.NH, rx: 8 }));
    grp.appendChild(text(10, 19, n.id, "nid"));
    grp.appendChild(text(10, 35, nodeLabel(n), "ntype"));
    svg.appendChild(grp);
  }
  return svg;
}

function nodeLabel(n) {
  if (n.type === "task") return "task" + (n.target ? " → " + n.target : "");
  if (n.type === "fanin") return "fanin (" + (n.join_policy || "all") + ")";
  if (n.type === "signal") return "signal: " + (n.signal_name || "");
  return n.type;
}

// ---- live updates -----------------------------------------------------------

function connectEvents() {
  const es = new EventSource("/api/events");
  es.onopen = () => $("#conn").classList.add("live");
  es.onerror = () => $("#conn").classList.remove("live");
  let pending = false;
  es.onmessage = (m) => {
    let ev; try { ev = JSON.parse(m.data); } catch { return; }
    if (!pending) { pending = true; setTimeout(() => { pending = false; refreshList(); }, 300); }
    if (ev.exec_id === state.selected) renderDetail(state.selected);
  };
}

// ---- helpers ----------------------------------------------------------------

function svgEl(tag, attrs) {
  const el = document.createElementNS("http://www.w3.org/2000/svg", tag);
  for (const k in attrs) el.setAttribute(k, attrs[k]);
  return el;
}
function text(x, y, s, cls) {
  const t = svgEl("text", { x, y, class: cls });
  t.textContent = s.length > 20 ? s.slice(0, 19) + "…" : s;
  return t;
}
function arrowDefs() {
  const defs = svgEl("defs", {});
  const m = svgEl("marker", { id: "arrow", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 6, markerHeight: 6, orient: "auto-start-reverse" });
  const p = svgEl("path", { d: "M 0 0 L 10 5 L 0 10 z", fill: "#2a2f3a" });
  m.appendChild(p); defs.appendChild(m); return defs;
}
function pretty(v) { try { return JSON.stringify(v, null, 2); } catch { return String(v); } }
function esc(s) { return String(s ?? "").replace(/[&<>]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;" }[c])); }

// ---- boot -------------------------------------------------------------------

$("#flow-filter").onchange = refreshList;
$("#status-filter").onchange = refreshList;
loadFlows().then(refreshList).then(connectEvents);
