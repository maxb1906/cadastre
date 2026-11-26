import './style.css';
import './shared/treemap.js';
import type { TmNode, Treemap } from './shared/treemap';
import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { t, setCatalog, lang } from './i18n';
import { bytes, count, pct, date, esc, h } from './format';
import { VList } from './vlist';
import { initClean, cleanPanel, addToSelection, isSelected, cleanReclaimable } from './clean';

// ---- State -------------------------------------------------------------------

interface Item {
  id: number;
  name: string;
  dir: boolean;
  size: number;
  alloc: number;
  mtime: number;
  files?: number;
  ext?: string;
  flags?: string[];
  grouped?: number;
}

export const state = {
  info: null as any,
  summary: null as any,
  dir: 0,
  sort: 'alloc',
  tab: 'clean',
  selected: null as number | null,
  findings: [] as any[],
  dirAlloc: 0,
};

const app = document.getElementById('app')!;
let tm: Treemap | null = null;
let list: VList<Item> | null = null;

// ---- Utilities -----------------------------------------------------------------

export function toast(msg: string, kind = 'info') {
  const el = h('div', { class: 'toast ' + kind }, msg);
  document.body.append(el);
  setTimeout(() => el.classList.add('gone'), 3500);
  setTimeout(() => el.remove(), 4000);
}

export function errorMsg(e: unknown): string {
  return typeof e === 'string' ? e : (e as Error)?.message ?? String(e);
}

function flagLabel(f: string) {
  return t('ui.flag.' + f);
}

function displayName(it: Item) {
  if (it.grouped) return t('ui.folded', count(it.grouped));
  if (it.name === '<orphans>') return t('ui.orphans');
  return it.name;
}

function famColor(it: Item) {
  const T = window.CadastreTreemap;
  const fam = T.familyOf({ i: it.id, n: it.name, s: it.alloc, d: it.dir, e: it.ext, o: it.grouped } as TmNode);
  const c = T.colors[fam];
  return `rgb(${c.join(',')})`;
}

// ---- Context menu ----------------------------------------------------------------

let menuEl: HTMLElement | null = null;
document.addEventListener('click', () => menuEl?.remove());
document.addEventListener('keydown', (e) => {
  if (e.key === 'Escape') menuEl?.remove();
  if (e.key === 'Backspace' && state.summary && !(e.target instanceof HTMLInputElement)) goUp();
});

export function contextMenu(e: MouseEvent, id: number, isDir: boolean) {
  e.preventDefault();
  menuEl?.remove();
  const m = h('div', { class: 'menu' });
  const add = (label: string, fn: () => void, disabled = false) => {
    const b = h('button', disabled ? { disabled: '' } : {}, label);
    b.onclick = (ev) => {
      ev.stopPropagation();
      menuEl?.remove();
      fn();
    };
    m.append(b);
  };
  if (isDir) add(t('ui.goto'), () => navigate(id));
  else add(t('ui.goto'), async () => navigate(await App.Locate(id), id));
  add(t('ui.reveal'), () => App.Reveal(id));
  add(t('ui.copy_path'), async () => {
    await navigator.clipboard.writeText(await App.PathOf(id));
    toast(t('ui.copied'));
  });
  add(isSelected(id) ? '✓ ' + t('clean.in_selection') : t('clean.add_selection'), () => addToSelection(id), isSelected(id));
  m.style.left = Math.min(e.clientX, innerWidth - 240) + 'px';
  m.style.top = Math.min(e.clientY, innerHeight - 160) + 'px';
  document.body.append(m);
  menuEl = m;
}

// ---- Welcome screen ----------------------------------------------------------------

async function welcome() {
  state.summary = null;
  tm?.destroy();
  tm = null;
  const vols: any[] = (await App.ListVolumes()) ?? [];
  const admin = state.info.admin;
  const engineSel = h('select', { id: 'engine' },
    h('option', { value: 'auto' }, t('ui.engine.auto')),
    h('option', { value: 'mft' }, t('ui.engine.mft')),
    h('option', { value: 'walk' }, t('ui.engine.walk')));
  const minSize = h('select', { id: 'minsize' },
    h('option', { value: '0' }, '—'), h('option', { value: '4k' }, '4 K'), h('option', { value: '64k' }, '64 K'), h('option', { value: '1M' }, '1 M'));
  const grid = h('div', { class: 'vols' });
  for (const v of vols) {
    const used = v.total - v.free;
    const card = h('button', { class: 'vol' },
      h('div', { class: 'vol-head' }, h('b', {}, v.root), h('span', { class: 'muted' }, ' ' + (v.label || '')), h('span', { class: 'tag' }, v.fs || '?')),
      h('div', { class: 'meter' }, h('span', { style: `width:${v.total ? (100 * used) / v.total : 0}%` })),
      h('div', { class: 'muted small' }, t('ui.free_of', bytes(v.free), bytes(v.total))),
      h('div', { class: 'small ' + (v.canMft ? 'ok' : 'muted') }, v.canMft ? 'MFT ⚡' : t('ui.engine.walk')));
    card.onclick = () => startScan(v.root);
    grid.append(card);
  }
  const folder = h('button', { class: 'btn' }, '📁 ' + t('ui.scan_folder'));
  folder.onclick = async () => {
    const p = await App.PickFolder();
    if (p) startScan(p);
  };
  const snap = h('button', { class: 'btn' }, '📦 ' + t('ui.open_snapshot'));
  snap.onclick = () => App.OpenSnapshot('').catch((e) => toast(errorMsg(e), 'error'));
  app.replaceChildren(
    h('div', { class: 'welcome' },
      h('div', { class: 'brand-big' }, h('span', { class: 'logo' }, '▦'), ' Cadastre', h('span', { class: 'muted small' }, '  ' + state.info.version)),
      h('h1', {}, t('ui.welcome')),
      h('p', { class: 'muted' }, t('ui.welcome_sub')),
      h('div', { class: 'banner ' + (admin ? 'ok' : 'warn') }, admin ? '🛡 ' + t('ui.admin_yes') : '⚠ ' + t('ui.admin_no')),
      grid,
      h('div', { class: 'row-actions' }, folder, snap),
      h('div', { class: 'opts' }, h('label', {}, t('ui.engine') + ' ', engineSel), h('label', {}, t('ui.minsize') + ' ', minSize), langButton()),
      h('p', { class: 'muted small' }, t('ui.drop_hint'))));
}

function langButton() {
  const b = h('button', { class: 'btn ghost' }, '🌐 ' + t('ui.lang'));
  b.onclick = async () => {
    const next = lang === 'fr' ? 'en' : 'fr';
    setCatalog(next, await App.SetLang(next));
    if (state.summary) await showMain(state.dir);
    else await welcome();
  };
  return b;
}

// ---- Scan ------------------------------------------------------------------------

let lastTarget = '';
let overlay: HTMLElement | null = null;

async function startScan(target: string) {
  lastTarget = target;
  const engine = (document.getElementById('engine') as HTMLSelectElement | null)?.value ?? 'auto';
  const minSize = (document.getElementById('minsize') as HTMLSelectElement | null)?.value ?? '0';
  showProgress(target);
  try {
    await App.StartScan(target, engine, minSize);
  } catch (e) {
    hideProgress();
    toast(errorMsg(e), 'error');
  }
}

function showProgress(target: string) {
  hideProgress();
  const cancel = h('button', { class: 'btn' }, t('ui.cancel'));
  cancel.onclick = () => App.CancelScan();
  overlay = h('div', { class: 'overlay' },
    h('div', { class: 'progress-box' },
      h('div', { class: 'spinner' }),
      h('h2', {}, t('ui.scanning')),
      h('div', { class: 'muted path' }, target),
      h('div', { class: 'meter big' }, h('span', { id: 'pbar', style: 'width:0%' })),
      h('div', { id: 'ptext', class: 'muted' }, ''),
      cancel));
  document.body.append(overlay);
}

function hideProgress() {
  overlay?.remove();
  overlay = null;
}

EventsOn('scan:progress', (p: any) => {
  const bar = document.getElementById('pbar');
  const txt = document.getElementById('ptext');
  if (!bar || !txt) return;
  if (p.phase === 'analyze') {
    bar.style.width = '100%';
    txt.textContent = t('ui.progress.analyze');
  } else if (p.total) {
    const v = Math.floor((100 * p.done) / p.total);
    bar.style.width = v + '%';
    txt.textContent = t('ui.progress.mft', String(v));
  } else {
    bar.classList.add('indeterminate');
    txt.textContent = t('ui.progress.walk', count(p.done));
  }
});
EventsOn('scan:done', async () => {
  hideProgress();
  state.selected = null;
  await showMain(-1);
});
EventsOn('scan:error', (msg: string) => {
  hideProgress();
  toast(t('ui.error') + ' : ' + msg, 'error');
});
EventsOn('scan:canceled', () => {
  hideProgress();
  toast(t('ui.canceled'));
});

// ---- Main screen -------------------------------------------------------------------

async function showMain(dir: number) {
  const s = await App.GetSummary();
  state.summary = s;
  state.findings = (await App.GetFindings()) ?? [];
  if (dir < 0) dir = s.rootId;
  tm?.destroy();
  tm = null;

  const exportMenu = h('div', { class: 'dropdown' });
  const exportBtn = h('button', { class: 'btn' }, '⇩ ' + t('ui.export') + ' ▾');
  const exportList = h('div', { class: 'dropdown-list' });
  for (const k of ['html', 'csv', 'json', 'md']) {
    const b = h('button', {}, t('ui.export.' + k));
    b.onclick = async () => {
      exportList.classList.remove('open');
      try {
        const p = await App.Export(k);
        if (p) toast(t('ui.saved', p), 'ok');
      } catch (e) {
        toast(errorMsg(e), 'error');
      }
    };
    exportList.append(b);
  }
  exportBtn.onclick = (e) => {
    e.stopPropagation();
    exportList.classList.toggle('open');
  };
  document.addEventListener('click', () => exportList.classList.remove('open'));
  exportMenu.append(exportBtn, exportList);

  const newBtn = h('button', { class: 'btn ghost' }, '⌂ ' + t('ui.new'));
  newBtn.onclick = () => welcome();
  const rescan = h('button', { class: 'btn ghost' }, '⟳ ' + t('ui.rescan'));
  rescan.onclick = () => startScan(lastTarget || s.root);
  const save = h('button', { class: 'btn ghost' }, '📦 ' + t('ui.save'));
  save.onclick = async () => {
    try {
      const p = await App.SaveSnapshot();
      if (p) toast(t('ui.saved', p), 'ok');
    } catch (e) {
      toast(errorMsg(e), 'error');
    }
  };

  const badges = h('span', { class: 'badges' },
    h('span', { class: 'tag' }, t('engine.' + s.engine)),
    s.fs ? h('span', { class: 'tag' }, s.fs) : '',
    s.fromSnapshot ? h('span', { class: 'tag warn' }, t('ui.snapshot_badge')) : '',
    h('span', { class: 'tag' }, (lang === 'fr' ? s.duration.toFixed(1).replace('.', ',') : s.duration.toFixed(1)) + ' s'));

  const tabs = h('div', { class: 'tabs' });
  if (!['clean', 'space', 'stats'].includes(state.tab)) state.tab = 'clean';
  for (const k of ['clean', 'space', 'stats']) {
    const b = h('button', { class: 'tab' + (state.tab === k ? ' active' : ''), 'data-tab': k }, t('ui.tab.' + k));
    if (k === 'clean') b.append(h('span', { class: 'count', id: 'sel-count', title: t('clean.selection') }, ''));
    b.onclick = () => {
      state.tab = k;
      tabs.querySelectorAll('.tab').forEach((x) => x.classList.toggle('active', x === b));
      renderPanel();
    };
    tabs.append(b);
  }

  app.replaceChildren(
    h('div', { class: 'main' },
      h('header', { class: 'toolbar' },
        h('span', { class: 'brand' }, h('span', { class: 'logo' }, '▦'), ' Cadastre'),
        h('span', { class: 'root' }, s.root, s.label ? h('span', { class: 'muted' }, ' « ' + s.label + ' »') : ''),
        badges,
        h('span', { class: 'spacer' }),
        newBtn, rescan, save, exportMenu, langButton()),
      h('div', { class: 'stats' },
        stat(t('rep.card.files'), count(s.totals.Files), t('rep.card.dirs', count(s.totals.Dirs))),
        stat(t('ui.alloc'), bytes(s.totals.Alloc), bytes(s.totals.Size) + ' ' + t('ui.size').toLowerCase()),
        s.recon && s.recon.used ? stat(t('rep.card.used'), bytes(s.recon.used), t('rep.card.of', bytes(s.recon.total), pct(s.recon.used, s.recon.total))) : '',
        reclaimCard(s),
        s.recon && s.recon.missing > 0 ? stat(t('rep.card.missing'), bytes(s.recon.missing), t('rep.card.missing_sub')) : '',
        s.warnings?.length ? h('div', { class: 'warnings' }, ...s.warnings.map((w: string) => h('div', { class: 'warn-line' }, '⚠ ' + w))) : ''),
      h('div', { class: 'body' },
        h('section', { class: 'left' },
          h('div', { class: 'crumbs', id: 'crumbs' }),
          h('div', { class: 'list-head' },
            sortHead('name', t('ui.name'), 'name'),
            sortHead('alloc', t('ui.alloc'), 'num'),
            sortHead('share', t('ui.share'), 'share'),
            sortHead('files', t('ui.files'), 'num'),
            sortHead('mtime', t('ui.modified'), 'num')),
          h('div', { class: 'list', id: 'list' })),
        h('section', { class: 'center' }, h('div', { id: 'treemap' })),
        h('section', { class: 'right' }, tabs, h('div', { class: 'panel', id: 'panel' }), h('div', { class: 'panel-foot', id: 'panel-foot' }))),
      h('footer', { class: 'status' },
        h('span', {}, t('ui.mem', count(s.entries), String(s.memoryMb))),
        h('span', { class: 'muted' }, s.admin ? t('rep.admin') : t('rep.noadmin')))));

  list = new VList<Item>(document.getElementById('list')!);
  tm = new window.CadastreTreemap.Treemap(document.getElementById('treemap')!, {
    format: bytes,
    dark: !matchMedia('(prefers-color-scheme: light)').matches,
    othersLabel: (n) => t('tm.others', n),
    onClick: (node, _e, path) => {
      if (node.o) return;
      if (node.d && node.c && node.c.length) navigate(node.i);
      else if (node.d) navigate(node.i);
      else {
        const parent = path[path.length - 1];
        state.selected = node.i;
        if (parent && parent.i !== state.dir) navigate(parent.i, node.i);
        else tm?.select(node.i);
      }
    },
    onContext: (node, e) => {
      if (node && !node.o) contextMenu(e, node.i, !!node.d);
      else goUp();
    },
  });
  initClean();
  await navigate(dir, state.selected ?? undefined);
  renderPanel();
}

/** Re-renders the main screen from the current (possibly cleaned) index. */
export async function refreshMain() {
  await showMain(state.dir);
}

/** The "Reclaimable" card is a button: it checks exactly the rules behind
 * its figure and opens their simulation. */
function reclaimCard(s: any): HTMLElement {
  const sub = t('rep.card.ghosts_sub', state.findings.length);
  if (!s.reclaimable) return stat(t('rep.card.ghosts'), bytes(0), sub, 'accent');
  const b = h('button', { class: 'stat accent action', title: t('clean.reclaim_hint') },
    h('div', { class: 'k' }, t('rep.card.ghosts')),
    h('div', { class: 'v' }, bytes(s.reclaimable)),
    h('div', { class: 's' }, sub),
    s.reclaimPermanentOnly ? h('div', { class: 's' }, t('clean.card_perm_only', bytes(s.reclaimPermanentOnly))) : '',
    h('div', { class: 's cta' }, '🧹 ' + t('clean.reclaim_cta') + ' →'));
  b.onclick = () => cleanReclaimable();
  return b;
}

function stat(k: string, v: string, sub: string, cls = '') {
  return h('div', { class: 'stat ' + cls }, h('div', { class: 'k' }, k), h('div', { class: 'v' }, v), h('div', { class: 's' }, sub));
}

function sortHead(key: string, label: string, cls: string) {
  const el = h('div', { class: 'col ' + cls + (state.sort === key ? ' sorted' : '') }, label);
  if (key !== 'share') {
    el.onclick = () => {
      state.sort = key;
      document.querySelectorAll('.list-head .col').forEach((c) => c.classList.remove('sorted'));
      el.classList.add('sorted');
      navigate(state.dir);
    };
  }
  return el;
}

async function goUp() {
  const s = state.summary;
  if (!s || state.dir === s.rootId) return;
  const p = await App.Locate(state.dir);
  navigate(p, state.dir);
}

export async function navigate(dir: number, select?: number) {
  const first = await App.GetChildren(dir, state.sort, 0, 400);
  state.dir = first.dir.id;
  state.dirAlloc = first.dir.alloc;
  if (select !== undefined) state.selected = select;
  // Breadcrumbs.
  const crumbs = document.getElementById('crumbs')!;
  crumbs.replaceChildren();
  const up = h('button', { class: 'btn tiny ghost', title: t('ui.back') }, '↑');
  up.onclick = goUp;
  crumbs.append(up);
  first.path.forEach((p: Item, k: number) => {
    const a = h('a', {}, p.name === '<orphans>' ? t('ui.orphans') : p.name);
    a.onclick = () => navigate(p.id);
    crumbs.append(a);
    if (k < first.path.length - 1) crumbs.append(h('span', { class: 'sep' }, '›'));
  });
  crumbs.append(h('span', { class: 'muted small' }, '  ' + bytes(first.dir.alloc)));
  // List.
  list!.setSource({
    total: first.total || 1,
    fetch: async (offset, limit) => (await App.GetChildren(dir, state.sort, offset, limit)).items as Item[],
    render: (it, _i) => renderRow(it),
  }, first.total ? (first.items as Item[]) : ([{ id: -1, name: t('ui.empty'), dir: false, size: 0, alloc: 0, mtime: 0 }] as Item[]));
  // Treemap.
  const node = (await App.GetTreemap(dir, 4000)) as TmNode;
  tm?.setData(node);
  tm?.select(state.selected);
}

function renderRow(it: Item): HTMLElement {
  if (it.id === -1) return h('div', { class: 'row muted' }, h('div', { class: 'col name' }, it.name));
  const share = state.dirAlloc ? (100 * it.alloc) / state.dirAlloc : 0;
  const name = h('div', { class: 'col name' },
    h('span', { class: 'dot', style: `background:${famColor(it)}` }),
    h('span', { class: 'ico' }, it.grouped ? '⋯' : it.dir ? '📁' : '📄'),
    h('span', { class: 'label' }, displayName(it)));
  for (const f of it.flags ?? []) {
    if (['hidden', 'system'].includes(f)) continue;
    name.append(h('span', { class: 'flag flag-' + f }, flagLabel(f)));
  }
  const row = h('div', { class: 'row' + (it.id === state.selected && !it.grouped ? ' selected' : '') + (isSelected(it.id) ? ' marked' : '') },
    name,
    h('div', { class: 'col num' }, bytes(it.alloc)),
    h('div', { class: 'col share' }, h('div', { class: 'sharebar' }, h('span', { style: `width:${Math.min(100, share)}%` })), h('span', { class: 'small muted' }, share >= 0.1 ? share.toFixed(1) : '')),
    h('div', { class: 'col num muted' }, it.dir ? count(it.files ?? 0) : ''),
    h('div', { class: 'col num muted' }, date(it.mtime)));
  if (it.grouped) return row;
  row.onclick = () => {
    state.selected = it.id;
    tm?.select(it.id);
    list?.refresh();
  };
  row.ondblclick = () => {
    if (it.dir) navigate(it.id);
  };
  row.oncontextmenu = (e) => contextMenu(e, it.id, it.dir);
  row.title = it.name;
  return row;
}

// ---- Right panels -------------------------------------------------------------------

export function renderPanel() {
  const panel = document.getElementById('panel');
  const foot = document.getElementById('panel-foot');
  if (!panel || !foot) return;
  const top = state.tab === lastTab ? panel.scrollTop : 0;
  lastTab = state.tab;
  panel.replaceChildren();
  foot.replaceChildren();
  switch (state.tab) {
    case 'clean':
      cleanPanel(panel, foot);
      break;
    case 'space':
      spacePanel(panel);
      break;
    case 'stats':
      statsPanel(panel);
      break;
  }
  // Re-rendering after a checkbox must not jump back to the top.
  panel.scrollTop = top;
  foot.hidden = !foot.childElementCount;
}

let lastTab = '';

function spacePanel(panel: HTMLElement) {
  const rc = state.summary.recon;
  if (!rc || !rc.used) {
    panel.append(h('p', { class: 'muted' }, t('ui.no_recon')));
    return;
  }
  const files = rc.components.find((c: any) => c.key === 'files');
  panel.append(h('p', {}, t('recon.summary', bytes(rc.used), bytes(rc.total), bytes(files?.bytes ?? 0))));
  if (rc.missing > 0) panel.append(h('p', { class: 'accent' }, t('rep.recon.missing', bytes(rc.missing))));
  const bar = h('div', { class: 'stack' });
  const table = h('table', { class: 'tbl' });
  for (const c of rc.components) {
    if (!c.bytes) continue;
    bar.append(h('span', { class: 'c-' + c.key, style: `width:${(100 * c.bytes) / rc.used}%`, title: t('recon.' + c.key) + ' : ' + bytes(c.bytes) }));
    const tr = h('tr', {},
      h('td', {}, h('span', { class: 'sq c-' + c.key }), t('recon.' + c.key), c.source === 'wmi' ? h('span', { class: 'tag' }, 'WMI') : ''),
      h('td', { class: 'num' }, bytes(c.bytes)),
      h('td', { class: 'num muted' }, pct(c.bytes, rc.used)));
    table.append(tr);
    if (c.details?.length) {
      table.append(h('tr', { class: 'detail' }, h('td', { colspan: '3' }, c.details.slice(0, 8).map((d: any) => d.name + ' ' + bytes(d.bytes)).join(' · '))));
    }
  }
  panel.append(bar, h('div', { class: 'muted small' }, t('ui.recon_hint')), table);
  if (rc.vss?.known) panel.append(h('p', { class: 'small muted' }, t('rep.vss', bytes(rc.vss.used), bytes(rc.vss.allocated), bytes(rc.vss.max), rc.vss.copies)));
  for (const n of rc.notes ?? []) panel.append(h('div', { class: 'note' }, t(n)));
}

function statsPanel(panel: HTMLElement) {
  const section = (title: string, fill: (box: HTMLElement) => Promise<void>) => {
    const box = h('div', {});
    const d = h('details', { class: 'stat-sec', open: '' }, h('summary', {}, title), box);
    panel.append(d);
    fill(box).catch((e) => toast(errorMsg(e), 'error'));
  };
  section(t('ui.stats.top'), topFiles);
  section(t('ui.stats.exts'), extensions);
}

async function topFiles(box: HTMLElement) {
  const files: Item[] = (await App.GetTopFiles(50)) ?? [];
  for (const f of files) {
    const r = h('div', { class: 'f-item', title: f.name },
      h('span', { class: 'dot', style: `background:${famColor(f)}` }),
      h('span', { class: 'path' }, f.name),
      h('span', { class: 'num' }, bytes(f.alloc)));
    r.onclick = async () => navigate(await App.Locate(f.id), f.id);
    r.oncontextmenu = (e) => contextMenu(e, f.id, false);
    box.append(r);
  }
}

async function extensions(box: HTMLElement) {
  const exts: any[] = (await App.GetExtensions(50)) ?? [];
  const total = state.summary.totals.Alloc;
  const table = h('table', { class: 'tbl' }, h('tr', {}, h('th', {}, t('col.ext')), h('th', { class: 'num' }, t('ui.alloc')), h('th', { class: 'num' }, t('ui.files')), h('th', { class: 'num' }, '%')));
  for (const e of exts) {
    const fam = window.CadastreTreemap.familyOf({ i: 0, n: '', s: 0, e: e.Ext } as TmNode);
    table.append(h('tr', {},
      h('td', {}, h('span', { class: 'sq', style: `background:rgb(${window.CadastreTreemap.colors[fam].join(',')})` }), e.Ext ? '.' + e.Ext : t('ext.none')),
      h('td', { class: 'num' }, bytes(e.Alloc)),
      h('td', { class: 'num muted' }, count(e.Files)),
      h('td', { class: 'num muted' }, pct(e.Alloc, total))));
  }
  box.append(table);
}

// ---- Boot -----------------------------------------------------------------------

(async () => {
  state.info = await App.GetInfo();
  setCatalog(state.info.lang, state.info.catalog);
  await welcome();
})();

export { esc };
