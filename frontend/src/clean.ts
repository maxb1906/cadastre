// Cleanup: the "To clean" panel (rules grouped by what Cadastre may do,
// checkboxes, manual selection, fixed footer), the mandatory simulation
// (dry-run with live checks), explicit confirmation, progress, result.
// Every safeguard is enforced in Go; this file only presents it.
import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { t } from './i18n';
import { bytes, count, h } from './format';
import { state, toast, errorMsg, renderPanel, navigate, refreshMain, contextMenu } from './main';

interface Manual {
  path: string;
  alloc: number;
  dir: boolean;
}

const sel = {
  rules: new Set<string>(),
  entries: new Map<number, Manual>(),
  mode: 'recycle',
};

// Rules whose card is unfolded, kept across re-renders.
const open = new Set<string>();

type Group = 'safe' | 'care' | 'official' | 'info';

/** groupOf sorts a finding by what Cadastre may do with it. */
function groupOf(f: any): Group {
  if (f.deletable) return f.risk === 'green' ? 'safe' : 'care';
  if (f.clean === 'official') return 'official';
  return 'info';
}

const groupIcon: Record<Group | 'manual', string> = { safe: '🟢', care: '🟠', official: '⚙', info: 'ℹ', manual: '✋' };

export function initClean() {
  // Drop rules that no longer have findings (after a cleanup or a rescan).
  for (const id of [...sel.rules]) if (!state.findings.some((f: any) => f.id === id)) sel.rules.delete(id);
  updateCount();
}

export function isSelected(id: number) {
  return sel.entries.has(id);
}

export async function addToSelection(id?: number, rule?: string) {
  if (rule) sel.rules.add(rule);
  if (id !== undefined) {
    try {
      const it: any = await App.GetItem(id);
      sel.entries.set(id, { path: it.name, alloc: it.alloc, dir: it.dir });
    } catch (e) {
      toast(errorMsg(e), 'error');
      return;
    }
  }
  updateCount();
  toast(t('clean.in_selection'), 'ok');
  if (state.tab === 'clean') renderPanel();
}

/** cleanReclaimable checks exactly the rules behind the "Reclaimable" card
 * and opens the simulation of those rules. */
export async function cleanReclaimable() {
  const ids = state.findings.filter((f: any) => f.reclaim > 0).map((f: any) => f.id as string);
  if (!ids.length) {
    toast(t('clean.nothing_reclaimable'));
    return;
  }
  sel.rules = new Set(ids);
  state.tab = 'clean';
  document.querySelectorAll('.right .tab').forEach((x) => x.classList.toggle('active', (x as HTMLElement).dataset.tab === 'clean'));
  updateCount();
  renderPanel();
  await simulate(ids, []);
}

function updateCount() {
  const el = document.getElementById('sel-count');
  if (el) {
    const n = sel.rules.size + sel.entries.size;
    el.textContent = n ? String(n) : '';
  }
}

function riskDot(risk: string) {
  return h('span', { class: 'risk ' + risk, title: t('risk.' + risk) });
}

function selectedFindings() {
  return state.findings.filter((f: any) => sel.rules.has(f.id));
}

// ---- Panel ----------------------------------------------------------------------

export function cleanPanel(panel: HTMLElement, foot: HTMLElement) {
  const groups: Record<Group, any[]> = { safe: [], care: [], official: [], info: [] };
  for (const f of state.findings) groups[groupOf(f)].push(f);
  if (!state.findings.length && !sel.entries.size) {
    panel.append(h('p', { class: 'muted' }, t('clean.empty')));
    return;
  }
  for (const g of ['safe', 'care', 'official', 'info'] as Group[]) {
    if (groups[g].length) panel.append(groupBlock(g, groups[g]));
  }
  if (sel.entries.size) panel.append(manualBlock());
  footer(foot);
}

function groupBlock(g: Group, findings: any[]): HTMLElement {
  const selectable = g !== 'info';
  const total = findings.reduce((a, f) => a + f.alloc, 0);
  const head = h('div', { class: 'g-head' });
  if (selectable) {
    const all = h('input', { type: 'checkbox', title: t('clean.check_all') }) as HTMLInputElement;
    const n = findings.filter((f) => sel.rules.has(f.id)).length;
    all.checked = n === findings.length;
    all.indeterminate = n > 0 && n < findings.length;
    all.onchange = () => {
      for (const f of findings) {
        if (all.checked) sel.rules.add(f.id);
        else sel.rules.delete(f.id);
      }
      updateCount();
      renderPanel();
    };
    head.append(all);
  } else {
    head.append(h('span', { class: 'g-pad' }));
  }
  head.append(
    h('span', { class: 'g-ico' }, groupIcon[g]),
    h('div', { class: 'g-title' }, h('b', {}, t('clean.group.' + g)), h('div', { class: 'muted small' }, t('clean.group.' + g + '_desc'))),
    h('span', { class: 'f-size' }, bytes(total)));
  const box = h('div', { class: 'group g-' + g }, head);
  for (const f of findings) box.append(ruleCard(f, selectable, g === 'official' || g === 'info'));
  return box;
}

function ruleCard(f: any, selectable: boolean, showRisk: boolean): HTMLElement {
  const card = h('div', { class: 'finding' + (open.has(f.id) ? ' open' : '') + (sel.rules.has(f.id) ? ' checked' : '') });
  const head = h('div', { class: 'f-head' });
  if (selectable) {
    const cb = h('input', { type: 'checkbox' }) as HTMLInputElement;
    cb.checked = sel.rules.has(f.id);
    cb.onclick = (e) => e.stopPropagation();
    cb.onchange = () => {
      if (cb.checked) sel.rules.add(f.id);
      else sel.rules.delete(f.id);
      updateCount();
      renderPanel();
    };
    head.append(cb);
  } else {
    head.append(h('span', { class: 'g-pad' }));
  }
  const tags = h('div', { class: 'f-tags' }, h('span', { class: 'muted small' }, count(f.files) + ' ' + t('ui.files').toLowerCase()));
  const tag = (txt: string, cls = '') => tags.append(h('span', { class: 'chip ' + cls }, txt));
  if (f.official && selectable) tag('⚙ ' + t('clean.tag.official', f.official));
  // The official action deletes its items itself, whatever the mode.
  if (f.permanent && !f.official) tag(t('clean.tag.permanent'), 'warn');
  if (f.system && !state.info.admin) tag('🛡 ' + t('clean.tag.admin'), 'warn');
  if (f.closeFirst?.length) tag(t('clean.tag.close', f.closeFirst.join(', ')));
  // Part of what was found that a cleanup would really free (the rest is
  // protected, a hard link, a placeholder… or needs administrator rights).
  if (f.reclaimable && f.reclaim > 0 && f.reclaim < f.alloc) tag(t('clean.tag.reclaim', bytes(f.reclaim)), 'ok');
  // In the safe and care groups the group already tells the risk.
  head.append(
    showRisk ? riskDot(f.risk) : '',
    h('div', { class: 'f-title' }, h('b', {}, f.title), tags),
    h('div', { class: 'f-size' }, bytes(f.alloc)),
    h('span', { class: 'chev' }, '›'));

  const body = h('div', { class: 'f-body' }, h('div', { class: 'advice' }, f.advice));
  if (f.groups?.length) {
    body.append(h('div', { class: 'groups' }, ...f.groups.map((g: any) => h('div', { class: 'small' }, (g.ghost ? '⚠ ' : '') + g.label + ' — ' + bytes(g.alloc)))));
  }
  const items = h('div', { class: 'f-items' });
  body.append(items);
  const loadItems = async () => {
    if (items.childElementCount) return;
    const rows: any[] = (await App.GetFindingItems(f.id, 0, 300)) ?? [];
    for (const it of rows) {
      const r = h('div', { class: 'f-item', title: it.name }, h('span', { class: 'path' }, it.name), h('span', { class: 'num' }, bytes(it.alloc)));
      r.onclick = async () => navigate(it.dir ? it.id : await App.Locate(it.id), it.id);
      r.oncontextmenu = (e) => contextMenu(e, it.id, it.dir);
      items.append(r);
    }
    if (f.files > rows.length && rows.length >= 300) items.append(h('div', { class: 'muted small' }, t('ui.more_items', f.files - rows.length)));
  };
  head.onclick = () => {
    card.classList.toggle('open');
    if (card.classList.contains('open')) {
      open.add(f.id);
      loadItems();
    } else open.delete(f.id);
  };
  if (open.has(f.id)) loadItems();
  card.append(head, body);
  return card;
}

function manualBlock(): HTMLElement {
  const total = [...sel.entries.values()].reduce((a, m) => a + m.alloc, 0);
  const box = h('div', { class: 'group g-manual' },
    h('div', { class: 'g-head' },
      h('span', { class: 'g-pad' }),
      h('span', { class: 'g-ico' }, groupIcon.manual),
      h('div', { class: 'g-title' }, h('b', {}, t('clean.group.manual')), h('div', { class: 'muted small' }, t('clean.group.manual_desc'))),
      h('span', { class: 'f-size' }, bytes(total))));
  for (const [id, m] of sel.entries) {
    const rm = h('button', { class: 'btn tiny ghost', title: t('clean.remove') }, '✕');
    rm.onclick = () => {
      sel.entries.delete(id);
      updateCount();
      renderPanel();
    };
    const label = h('span', { class: 'grow path', title: m.path }, (m.dir ? '📁 ' : '📄 ') + m.path);
    label.onclick = async () => navigate(m.dir ? id : await App.Locate(id), id);
    box.append(h('div', { class: 'sel-row' }, h('span', { class: 'risk orange', title: t('risk.orange') }), label, h('span', { class: 'num' }, bytes(m.alloc)), rm));
  }
  return box;
}

function footer(foot: HTMLElement) {
  const fs = selectedFindings();
  const n = fs.length + sel.entries.size;
  // An estimate: a manual item may sit inside a selected rule.
  const est = fs.reduce((a, f) => a + (f.reclaimable ? f.reclaim : f.alloc), 0) + [...sel.entries.values()].reduce((a, m) => a + m.alloc, 0);
  const seg = h('div', { class: 'seg' });
  for (const [value, label, hint] of [['recycle', t('clean.recycle_short'), t('clean.recycle_desc')], ['permanent', t('clean.permanent_short'), t('clean.permanent_desc')]]) {
    const b = h('button', { class: 'seg-btn' + (sel.mode === value ? ' active' : ''), title: hint }, label);
    b.onclick = () => {
      sel.mode = value;
      renderPanel();
    };
    seg.append(b);
  }
  const sim = h('button', { class: 'btn primary' }, '🧹 ' + t('clean.simulate')) as HTMLButtonElement;
  sim.disabled = n === 0;
  sim.onclick = () => simulate([...sel.rules], [...sel.entries.keys()]);
  const notes = h('div', { class: 'foot-notes' });
  if (sel.mode === 'recycle') {
    const perm = fs.filter((f) => f.permanent && !f.official);
    if (perm.length) notes.append(h('div', { class: 'small warn' }, '⚠ ' + t('clean.foot.perm_warn', perm.map((f) => f.title).join(', '))));
  }
  if (fs.some((f) => f.official)) notes.append(h('div', { class: 'small muted' }, '⚙ ' + t('clean.foot.official_note')));
  if (fs.some((f) => f.risk === 'orange') || sel.entries.size) notes.append(h('div', { class: 'small muted' }, '🟠 ' + t('clean.foot.orange_note')));
  foot.append(
    h('div', { class: 'foot-sum' },
      n ? h('span', {}, h('b', {}, t('clean.foot.summary', n)), h('span', { class: 'muted' }, ' · ≈ ' + bytes(est))) : h('span', { class: 'muted' }, t('clean.foot.none')),
      h('span', { class: 'spacer' }), seg),
    notes,
    sim);
}

// ---- Simulation --------------------------------------------------------------------

async function simulate(rules: string[], entries: number[], mode = sel.mode) {
  // Rules with an official action go through it (the documented way, e.g.
  // stopping wuauserv around the Windows Update cache).
  const official = state.findings.some((f: any) => rules.includes(f.id) && f.official);
  let plan: any;
  try {
    plan = await App.PlanClean({ rules, entries, mode, official } as any);
  } catch (e) {
    toast(errorMsg(e), 'error');
    return;
  }
  showPlan(plan, () => {
    sel.mode = 'permanent';
    renderPanel();
    simulate(rules, entries, 'permanent');
  });
}

// showPlan is the confirmation screen: every concrete action (files deleted
// by Cadastre, official actions with their commands), what is left out and
// why, then one button that really deletes. Behind it, the plan has just
// been checked item by item on the disk (the mandatory dry-run).
function showPlan(plan: any, resimPermanent: () => void) {
  const overlay = h('div', { class: 'overlay' });
  const modal = h('div', { class: 'modal confirm' });
  overlay.append(modal);
  const close = () => overlay.remove();
  const actions: any[] = plan.actions ?? [];
  const items: any[] = plan.items ?? [];

  const orange = h('input', { type: 'checkbox' }) as HTMLInputElement;
  const restore = h('input', { type: 'checkbox' }) as HTMLInputElement;
  restore.checked = true;
  const run = h('button', { class: 'btn danger' }) as HTMLButtonElement;
  const total = h('b', { class: 'big' });
  const partial = h('div', { class: 'small warn' });
  const list = h('div', { class: 'actions' });

  // An orange action only runs with the explicit confirmation.
  const active = (a: any) => a.risk !== 'orange' || orange.checked;
  const render = () => {
    list.replaceChildren();
    for (const a of actions) list.append(actionRow(a, items, active(a)));
    const skipped = (plan.skipped ?? []) as any[];
    if (skipped.length) {
      const n = skipped.reduce((x, g) => x + g.count, 0);
      const al = skipped.reduce((x, g) => x + g.alloc, 0);
      const d = h('details', { class: 'action skipped' },
        h('summary', {}, h('span', { class: 'a-ico' }, '⏭'), h('span', { class: 'a-title' }, h('b', {}, t('clean.skipped_count', count(n))), h('div', { class: 'muted small' }, t('clean.skipped_sub'))), h('span', { class: 'f-size muted' }, bytes(al))));
      for (const g of skipped) {
        d.append(h('div', { class: 'small skip-reason' }, h('b', {}, `${g.label} — ${count(g.count)} (${bytes(g.alloc)})`)));
        for (const ex of g.examples ?? []) d.append(h('div', { class: 'path small muted' }, ex));
      }
      list.append(d);
    }
    const bytesNow = actions.filter(active).reduce((x, a) => x + a.bytes, 0);
    const doable = actions.filter(active).length;
    total.textContent = bytes(bytesNow);
    const all = actions.reduce((x, a) => x + a.bytes, 0);
    partial.textContent = bytesNow < all ? t('clean.to_free_partial', bytes(all)) : '';
    run.textContent = '🧹 ' + (doable ? t('clean.run_now', bytes(bytesNow)) : t('clean.run_nothing'));
    run.disabled = !doable;
  };
  orange.onchange = render;

  const script = h('button', { class: 'btn' }, '📜 ' + t('clean.script'));
  script.onclick = async () => {
    try {
      const p = await App.ExportScript(plan.id);
      if (p) toast(t('ui.saved', p), 'ok');
    } catch (e) {
      toast(errorMsg(e), 'error');
    }
  };
  const cancel = h('button', { class: 'btn ghost' }, t('ui.cancel'));
  cancel.onclick = close;
  run.onclick = async () => {
    try {
      await App.ExecuteClean(plan.id, orange.checked, restore.checked);
      close();
      showRunning();
    } catch (e) {
      toast(errorMsg(e), 'error');
    }
  };
  // Items left out only because the recycle bin makes no sense there.
  const need = (plan.skipped ?? []).find((x: any) => x.reason === 'need_permanent');
  let permNote: HTMLElement | string = '';
  if (need && plan.mode === 'recycle') {
    const b = h('button', { class: 'btn tiny' }, t('clean.resim_permanent'));
    b.onclick = () => {
      close();
      resimPermanent();
    };
    permNote = h('div', { class: 'note' }, t('clean.need_permanent_note', count(need.count), bytes(need.alloc)), ' ', b);
  }
  const hasOrange = actions.some((a) => a.risk === 'orange');
  const hasRestore = actions.some((a) => a.restorePoint);

  modal.append(
    h('h2', {}, t('clean.confirm_title')),
    h('div', { class: 'confirm-head' },
      h('div', {}, h('div', { class: 'muted small' }, t('clean.to_free')), total, partial),
      h('div', { class: 'small' }, h('div', {}, t('clean.mode_is', t('clean.mode.' + plan.mode))), h('div', { class: 'muted' }, '✔ ' + t('clean.sim_checked')))),
    plan.fromSnapshot ? h('div', { class: 'note' }, t('clean.snapshot_note')) : '',
    permNote,
    actions.length ? '' : h('div', { class: 'note' }, t('clean.nothing_doable')),
    h('div', { class: 'scroll' }, list),
    h('div', { class: 'foot' },
      hasOrange ? h('label', { class: 'opt warnbox' }, orange, ' ' + t('clean.confirm_orange')) : '',
      hasRestore ? h('label', { class: 'opt' }, restore, ' ' + t('clean.restore_point')) : '',
      h('span', { class: 'spacer' }), script, cancel, run));
  render();
  document.body.append(overlay);
}

function actionRow(a: any, items: any[], active: boolean): HTMLElement {
  const official = a.kind === 'official';
  let what: string;
  if (official) what = '⚙ ' + t('clean.action.official', a.official) + (a.files ? ' · ' + t('clean.action.files', count(a.files)) : '');
  else what = t('clean.action.delete', count(a.files));
  const head = h('summary', {},
    h('span', { class: 'risk ' + a.risk, title: t('risk.' + a.risk) }),
    h('span', { class: 'a-title' }, h('b', {}, a.title), h('div', { class: 'small' }, what),
      active ? '' : h('div', { class: 'small warn' }, t('clean.action.needs_orange'))),
    h('span', { class: 'f-size' }, bytes(a.bytes)));
  const d = h('details', { class: 'action' + (active ? '' : ' off') }, head);
  if (official) {
    d.append(h('div', { class: 'small muted' }, t('clean.commands')));
    for (const c of a.commands ?? []) d.append(h('div', { class: 'path cmd' }, '> ' + c));
    if (a.targets?.length) {
      d.append(h('div', { class: 'small muted' }, t('clean.action.targets')));
      for (const p of a.targets.slice(0, 50)) d.append(h('div', { class: 'path small' }, p));
    }
  } else {
    const mine = items.filter((it) => (it.rule ?? '') === a.rule);
    const tbl = h('table', { class: 'tbl' });
    for (const it of mine.slice(0, 500)) {
      tbl.append(h('tr', {}, h('td', { class: 'path' }, it.path + (it.reparse ? '  ↪' : '')), h('td', { class: 'num' }, bytes(it.alloc))));
    }
    if (a.files > mine.length || mine.length > 500) tbl.append(h('tr', {}, h('td', { colspan: '2', class: 'muted' }, t('clean.more', a.files - Math.min(mine.length, 500)))));
    d.append(tbl);
  }
  return d;
}

let runOverlay: HTMLElement | null = null;

function showRunning() {
  const stop = h('button', { class: 'btn' }, t('ui.cancel'));
  stop.onclick = () => App.CancelClean();
  runOverlay = h('div', { class: 'overlay' },
    h('div', { class: 'progress-box' },
      h('div', { class: 'spinner' }),
      h('h2', {}, t('clean.running')),
      h('div', { class: 'meter big' }, h('span', { id: 'cbar', style: 'width:0%' })),
      h('div', { id: 'ctext', class: 'muted path' }, ''),
      stop));
  document.body.append(runOverlay);
}

EventsOn('clean:progress', (p: any) => {
  const bar = document.getElementById('cbar');
  const txt = document.getElementById('ctext');
  if (bar) bar.style.width = (100 * p.done) / Math.max(1, p.total) + '%';
  if (txt) txt.textContent = `${count(p.done)} / ${count(p.total)} · ${bytes(p.freed)} — ${p.path}`;
});

EventsOn('clean:error', (msg: string) => {
  runOverlay?.remove();
  toast(t('ui.error') + ' : ' + msg, 'error');
});

EventsOn('clean:done', async (r: any) => {
  runOverlay?.remove();
  sel.rules.clear();
  sel.entries.clear();
  const journal = h('button', { class: 'btn' }, '📒 ' + r.journal);
  journal.onclick = () => App.OpenJournalFolder();
  const ok = h('button', { class: 'btn primary' }, t('clean.close'));
  const overlay = h('div', { class: 'overlay' });
  const tbl = h('table', { class: 'tbl' });
  for (const e of (r.entries ?? []).slice(0, 500)) {
    tbl.append(h('tr', {}, h('td', { class: 'small muted' }, e.time), h('td', {}, e.action), h('td', { class: 'path' }, e.path), h('td', { class: 'num' }, e.size ? bytes(e.size) : ''), h('td', { class: 'small' }, e.result)));
  }
  overlay.append(h('div', { class: 'modal' },
    h('h2', {}, t('clean.done_title')),
    h('p', {}, h('b', {}, t('clean.result', r.done, r.failed, r.skipped, bytes(r.freed)))),
    r.freeBefore ? h('p', { class: 'muted' }, t('clean.freed_real', bytes(r.freeBefore), bytes(r.freeAfter))) : '',
    h('div', { class: 'scroll' }, tbl),
    h('div', { class: 'foot' }, journal, h('span', { class: 'spacer' }), ok)));
  ok.onclick = () => overlay.remove();
  document.body.append(overlay);
  // Refresh the whole view from the updated index.
  await refreshMain();
  updateCount();
});
