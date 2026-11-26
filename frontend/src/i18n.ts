// Translations come from Go (internal/i18n), the single source of truth.
let catalog: Record<string, string> = {};
export let lang = 'fr';

export function setCatalog(l: string, c: Record<string, string>) {
  lang = l;
  catalog = c;
  document.documentElement.lang = l;
}

/** t formats like Go's fmt for %s, %d and %%. */
export function t(key: string, ...args: unknown[]): string {
  const msg = catalog[key] ?? key;
  let k = 0;
  return msg.replace(/%%|%[sdv]/g, (m) => (m === '%%' ? '%' : String(args[k++] ?? '')));
}
