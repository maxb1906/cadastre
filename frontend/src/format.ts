import { lang } from './i18n';

export function bytes(n: number): string {
  const u = lang === 'fr' ? ['o', 'Ko', 'Mo', 'Go', 'To', 'Po'] : ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
  let k = 0;
  let v = n;
  while (v >= 1024 && k < u.length - 1) {
    v /= 1024;
    k++;
  }
  const s = k === 0 ? String(v) : v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2);
  return (lang === 'fr' ? s.replace('.', ',') : s) + ' ' + u[k];
}

export function count(n: number): string {
  return n.toLocaleString(lang === 'fr' ? 'fr-FR' : 'en-US');
}

export function pct(part: number, total: number): string {
  if (!total) return '–';
  const v = (100 * part) / total;
  const s = v.toFixed(1);
  return lang === 'fr' ? s.replace('.', ',') + ' %' : s + '%';
}

export function date(unix: number): string {
  if (!unix) return '';
  return new Date(unix * 1000).toLocaleDateString(lang === 'fr' ? 'fr-FR' : 'en-CA');
}

export function esc(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[c]!);
}

export function h<K extends keyof HTMLElementTagNameMap>(
  tag: K,
  attrs: Record<string, string> = {},
  ...children: (Node | string)[]
): HTMLElementTagNameMap[K] {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs)) {
    if (k === 'class') el.className = v;
    else el.setAttribute(k, v);
  }
  for (const c of children) el.append(c);
  return el;
}
