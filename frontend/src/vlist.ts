// Virtualized list: only the visible rows are in the DOM, rows are fetched
// by pages from Go as the user scrolls.

export interface VListSource<T> {
  total: number;
  fetch(offset: number, limit: number): Promise<T[]>;
  render(item: T, index: number): HTMLElement;
}

const PAGE = 400;

export class VList<T> {
  private el: HTMLElement;
  private inner: HTMLElement;
  private rows: HTMLElement;
  private src: VListSource<T> | null = null;
  private pages = new Map<number, T[] | Promise<void>>();
  private gen = 0;

  constructor(container: HTMLElement, private rowHeight = 26) {
    this.el = container;
    this.el.classList.add('vlist');
    this.inner = document.createElement('div');
    this.inner.className = 'vlist-inner';
    this.rows = document.createElement('div');
    this.rows.className = 'vlist-rows';
    this.inner.append(this.rows);
    this.el.append(this.inner);
    this.el.addEventListener('scroll', () => this.draw());
    new ResizeObserver(() => this.draw()).observe(this.el);
  }

  setSource(src: VListSource<T>, first?: T[]) {
    this.gen++;
    this.src = src;
    this.pages.clear();
    if (first) this.pages.set(0, first);
    this.inner.style.height = src.total * this.rowHeight + 'px';
    this.el.scrollTop = 0;
    this.draw();
  }

  refresh() {
    this.draw();
  }

  private item(i: number): T | undefined {
    const p = this.pages.get(Math.floor(i / PAGE));
    return Array.isArray(p) ? p[i % PAGE] : undefined;
  }

  private load(page: number) {
    if (!this.src || this.pages.has(page)) return;
    const gen = this.gen;
    const pr = this.src.fetch(page * PAGE, PAGE).then((items) => {
      if (gen !== this.gen) return;
      this.pages.set(page, items);
      this.draw();
    });
    this.pages.set(page, pr);
  }

  private draw() {
    if (!this.src) return;
    const top = this.el.scrollTop;
    const first = Math.max(0, Math.floor(top / this.rowHeight) - 5);
    const last = Math.min(this.src.total, Math.ceil((top + this.el.clientHeight) / this.rowHeight) + 5);
    this.rows.style.transform = `translateY(${first * this.rowHeight}px)`;
    const frag = document.createDocumentFragment();
    for (let i = first; i < last; i++) {
      const it = this.item(i);
      if (it === undefined) {
        this.load(Math.floor(i / PAGE));
        const ph = document.createElement('div');
        ph.className = 'row placeholder';
        ph.style.height = this.rowHeight + 'px';
        frag.append(ph);
        continue;
      }
      const row = this.src.render(it, i);
      row.style.height = this.rowHeight + 'px';
      frag.append(row);
    }
    this.rows.replaceChildren(frag);
  }
}
