// Multi-disk cleanup: the volumes checked on the welcome screen are scanned
// one after the other, then each shows its rules with a suggested selection
// (green rules whose space is really reclaimable). The cleanup goes through
// the same confirmation screen and safeguards as a single volume.
import * as App from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { t } from './i18n';
import { bytes, count, h } from './format';
import { toast, errorMsg, goHome } from './main';
import { groupOf, groupIcon, showConfirm, cleanHooks, sizeCell, type Group } from './clean';

const ms = {
  disks: [] as any[],
  rules: new Map<string, Set<string>>(), // root → checked rules
  mode: 'recycle',
};

let overlay: HTMLElement | null = null;

/** startMulti scans the given volumes, then opens the multi-disk screen. */
export async function startMulti(roots: string[], engine: string) {
  overlay?.remove();
  const cancel = h('button', { class: 'btn' }, t('ui.cancel'));
  cancel.onclick = () => App.CancelScan();
  overlay = h('div', { class: 'overlay' },
    h('div', { class: 'progress-box' },
      h('div', { class: 'spinner' }),
      h('h2', {}, t('multi.scanning')),
      h('div', { id: 'mdisk', class: 'muted path' }, roots.join('  ')),
      h('div', { class: 'meter big' }, h('span', { id: 'mbar', style: 'width:0%' })),
      h('div', { id: 'mtext', class: 'muted' }, ''),
      cancel));
  document.body.append(overlay);
  try {
    await App.StartMulti(roots, engine);
  } catch (e) {
    overlay?.remove();
    toast(errorMsg(e), 'error');
  }
}

EventsOn('multi:progress', (m: any) => {
  const bar = document.getElementById('mbar');
  const txt = document.getElementById('mtext');
  const disk = document.getElementById('mdisk');
  if (!bar || !txt || !disk) return;
  const p = m.progress ?? {};
  disk.textContent = t('multi.disk_n', m.index + 1, m.count, m.root);
  // Overall progress: finished volumes plus the share of the current one.
  const part = p.phase === 'analyze' ? 1 : p.total ? p.done / p.total : 0;
  bar.style.width = (100 * (m.index + part)) / m.count + '%';
  txt.textContent = p.phase === 'analyze' ? t('ui.progress.analyze') : p.total ? t('ui.progress.mft', String(Math.floor(100 * part))) : p.done ? t('ui.progress.walk', count(p.done)) : '';
});
EventsOn('multi:done', async () => {
  overlay?.remove();
  overlay = null;
  ms.disks = (await App.GetMulti()) ?? [];
  ms.rules.clear();
  for (const d of ms.disks) ms.rules.set(d.root, new Set(d.suggested ?? []));
  ms.mode = 'recycle';
  render();
});
EventsOn('multi:canceled', () => {
  overlay?.remove();
  overlay = null;
  toast(t('ui.canceled'));
});

// ---- Screen ------------------------------------------------------------------------

function render() {
  cleanHooks.afterDone = afterClean;
  const app = document.getElementById('app')!;
  const home = h('button', { class: 'btn ghost' }, '⌂ ' + t('ui.new'));
  home.onclick = () => {
    cleanHooks.afterDone = null;
    goHome();
  };
  const ok = ms.disks.filter((d) => !d.error);
  const reclaim = ok.reduce((x, d) => x + d.reclaimable, 0);
  const adminOnly = ok.reduce((x, d) => x + (d.reclaimAdminOnly ?? 0), 0);
  const list = h('div', { class: 'multi-list' });
  for (const d of ms.disks) list.append(diskBlock(d));
  const foot = h('div', { class: 'panel-foot multi-foot' });
  footer(foot);
  app.replaceChildren(
    h('div', { class: 'main multi' },
      h('header', { class: 'toolbar' },
        h('span', { class: 'brand' }, h('span', { class: 'logo' }, '▦'), ' Cadastre'),
        h('span', { class: 'root' }, t('multi.title', ms.disks.length)),
        h('span', { class: 'spacer' }), home),
      h('div', { class: 'stats' },
        h('div', { class: 'stat' }, h('div', { class: 'k' }, t('multi.disks')), h('div', { class: 'v' }, String(ok.length)), h('div', { class: 's' }, ms.disks.length > ok.length ? t('multi.failed', ms.disks.length - ok.length) : ms.disks.map((d) => d.root).join(' '))),
        h('div', { class: 'stat accent' }, h('div', { class: 'k' }, t('rep.card.ghosts')), h('div', { class: 'v' }, bytes(reclaim)), h('div', { class: 's' }, t('multi.reclaim_sub'))),
        adminOnly ? h('div', { class: 'warnings' }, h('div', { class: 'warn-line' }, '🛡 ' + t('clean.admin_more', bytes(adminOnly)))) : ''),
      h('div', { class: 'multi-body' }, list),
      foot));
}

/** After a cleanup, show the volumes as they are now. */
async function afterClean() {
  ms.disks = (await App.GetMulti()) ?? [];
  // Keep only what can still be reclaimed in the selection.
  for (const d of ms.disks) {
    const keep = new Set<string>();
    for (const id of ms.rules.get(d.root) ?? []) if ((d.findings ?? []).some((f: any) => f.id === id && f.alloc > 0)) keep.add(id);
    ms.rules.set(d.root, keep);
  }
  render();
}

function checked(root: string) {
  let s = ms.rules.get(root);
  if (!s) ms.rules.set(root, (s = new Set()));
  return s;
}

/** The space a rule's cleanup really frees (0 when its gain is unknown). */
function worth(f: any) {
  return f.reclaimable ? f.reclaim : 0;
}

function diskBlock(d: any): HTMLElement {
  const used = d.total - d.free;
  const head = h('div', { class: 'disk-title' },
    h('b', {}, '💽 ' + d.root), d.label ? h('span', { class: 'muted' }, ' « ' + d.label + ' »') : '', d.fs ? h('span', { class: 'tag' }, d.fs) : '',
    h('span', { class: 'spacer' }),
    d.error ? '' : h('span', { class: 'small muted' }, t('ui.free_of', bytes(d.free), bytes(d.total))),
    d.error ? '' : h('span', { class: 'disk-reclaim' }, bytes(d.reclaimable)));
  const box = h('section', { class: 'disk' }, head);
  if (d.error) {
    box.append(h('div', { class: 'note' }, t('multi.scan_error', d.error)));
    return box;
  }
  box.append(h('div', { class: 'meter' }, h('span', { style: `width:${d.total ? (100 * used) / d.total : 0}%` })));
  const notes: string[] = [];
  if (d.reclaimPermanentOnly) notes.push(t('clean.card_perm_only', bytes(d.reclaimPermanentOnly)));
  if (d.reclaimAdminOnly) notes.push(t('clean.admin_more', bytes(d.reclaimAdminOnly)));
  if (notes.length) box.append(h('div', { class: 'small muted' }, notes.join(' · ')));
  const findings: any[] = d.findings ?? [];
  if (!findings.length) {
    box.append(h('p', { class: 'muted small' }, t('multi.nothing')));
    return box;
  }
  const groups: Record<Group, any[]> = { safe: [], care: [], official: [], info: [] };
  for (const f of findings) groups[groupOf(f)].push(f);
  const sel = checked(d.root);
  for (const g of ['safe', 'care', 'official', 'info'] as Group[]) {
    if (!groups[g].length) continue;
    const selectable = g !== 'info';
    const gh = h('div', { class: 'm-group' }, h('span', { class: 'g-ico' }, groupIcon[g]), h('b', {}, t('clean.group.' + g)), h('span', { class: 'muted small' }, ' · ' + t('clean.group.' + g + '_desc')));
    box.append(gh);
    for (const f of groups[g]) {
      const row = h('label', { class: 'm-rule' + (selectable ? '' : ' off') });
      if (selectable) {
        const cb = h('input', { type: 'checkbox' }) as HTMLInputElement;
        cb.checked = sel.has(f.id);
        cb.onchange = () => {
          if (cb.checked) sel.add(f.id);
          else sel.delete(f.id);
          const foot = document.querySelector('.multi-foot') as HTMLElement;
          footer(foot);
        };
        row.append(cb);
      } else row.append(h('span', { class: 'g-pad' }));
      const chips = h('span', { class: 'f-tags inline' });
      if (f.official && selectable) chips.append(h('span', { class: 'chip' }, '⚙ ' + t('clean.tag.official', f.official)));
      if (f.permanent && !f.official) chips.append(h('span', { class: 'chip warn' }, t('clean.tag.permanent')));
      if (f.system && !f.reclaim && f.reclaimable) chips.append(h('span', { class: 'chip warn' }, '🛡 ' + t('clean.tag.admin')));
      if (f.closeFirst?.length) chips.append(h('span', { class: 'chip' }, t('clean.tag.close', f.closeFirst.join(', '))));
      row.append(
        h('span', { class: 'risk ' + f.risk, title: t('risk.' + f.risk) }),
        h('span', { class: 'm-title', title: f.advice }, f.title),
        chips,
        sizeCell(f, 'span', f.reclaimable ? f.reclaim : f.alloc));
      box.append(row);
    }
  }
  return box;
}

function footer(foot: HTMLElement) {
  foot.replaceChildren();
  let n = 0;
  let disks = 0;
  let est = 0;
  let noGain = 0;
  for (const d of ms.disks) {
    const s = ms.rules.get(d.root);
    if (!s?.size) continue;
    disks++;
    n += s.size;
    for (const f of d.findings ?? []) {
      if (!s.has(f.id)) continue;
      est += worth(f);
      if (!f.reclaimable && f.clean === 'official') noGain++;
    }
  }
  const seg = h('div', { class: 'seg' });
  for (const [value, label, hint] of [['recycle', t('clean.recycle_short'), t('clean.recycle_desc')], ['permanent', t('clean.permanent_short'), t('clean.permanent_desc')]]) {
    const b = h('button', { class: 'seg-btn' + (ms.mode === value ? ' active' : ''), title: hint }, label);
    b.onclick = () => {
      ms.mode = value;
      footer(foot);
    };
    seg.append(b);
  }
  const go = h('button', { class: 'btn primary' }, '🧹 ' + t('clean.simulate')) as HTMLButtonElement;
  go.disabled = n === 0;
  go.onclick = () => plan(ms.mode);
  foot.append(
    h('div', { class: 'foot-sum' },
      n ? h('span', {}, h('b', {}, t('multi.selection', n, disks)), h('span', { class: 'muted' }, ' · ≈ ' + bytes(est) + (noGain ? ' · ' + t('clean.foot.no_gain', noGain) : ''))) : h('span', { class: 'muted' }, t('clean.foot.none')),
      h('span', { class: 'spacer' }), seg, go));
}

async function plan(mode: string) {
  const disks = ms.disks.filter((d) => ms.rules.get(d.root)?.size).map((d) => ({ root: d.root, rules: [...ms.rules.get(d.root)!] }));
  let v: any;
  try {
    v = await App.PlanMulti({ disks, mode } as any);
  } catch (e) {
    toast(errorMsg(e), 'error');
    return;
  }
  showConfirm(v.disks.map((d: any) => ({ title: d.root + (d.label ? ' « ' + d.label + ' »' : ''), plan: d.plan })), {
    mode: v.mode,
    run: (orange, restore) => App.ExecuteMulti(v.id, orange, restore),
    resimPermanent: () => {
      ms.mode = 'permanent';
      render();
      plan('permanent');
    },
  });
}
