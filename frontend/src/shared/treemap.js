/*
 * Cadastre treemap — shared by the Wails GUI and the standalone HTML report.
 * Plain script (no module syntax, no dependency): the report inlines this
 * file verbatim, the GUI imports it for its side effect. It exposes
 * globalThis.CadastreTreemap.
 *
 * Data: { i: id, n: name, s: size, d: isDir, e: ext, o: othersCount, c: [children] }
 *
 * License: GPL-3.0-or-later.
 */
(function (g) {
  'use strict';

  // ---- Squarified layout (Bruls, Huizing, van Wijk 2000) -----------------

  function worst(row, sum, side) {
    var max = 0, min = Infinity;
    for (var k = 0; k < row.length; k++) {
      var v = row[k].area;
      if (v > max) max = v;
      if (v < min) min = v;
    }
    var s2 = sum * sum, w2 = side * side;
    return Math.max((w2 * max) / s2, s2 / (w2 * min));
  }

  /**
   * Lays out items (objects with a numeric "value", sorted by decreasing
   * value) in the rectangle; sets x, y, w, h on each item.
   */
  function squarify(items, x, y, w, h) {
    var total = 0;
    for (var k = 0; k < items.length; k++) total += items[k].value;
    if (total <= 0 || w <= 0 || h <= 0) {
      for (var z = 0; z < items.length; z++) { items[z].x = x; items[z].y = y; items[z].w = 0; items[z].h = 0; }
      return;
    }
    var scale = (w * h) / total;
    for (var j = 0; j < items.length; j++) items[j].area = items[j].value * scale;
    var i = 0;
    while (i < items.length) {
      var side = Math.min(w, h);
      var row = [items[i]], sum = items[i].area;
      i++;
      while (i < items.length) {
        var next = items[i];
        var cand = row.concat([next]);
        if (worst(cand, sum + next.area, side) > worst(row, sum, side)) break;
        row = cand;
        sum += next.area;
        i++;
      }
      // Lay the row along the shorter side.
      var thick = sum / side;
      var pos = 0;
      for (var r = 0; r < row.length; r++) {
        var len = row[r].area / thick;
        if (w >= h) { row[r].x = x; row[r].y = y + pos; row[r].w = thick; row[r].h = len; }
        else { row[r].x = x + pos; row[r].y = y; row[r].w = len; row[r].h = thick; }
        pos += len;
      }
      if (w >= h) { x += thick; w -= thick; } else { y += thick; h -= thick; }
    }
  }

  // ---- Colors by family of extension -------------------------------------

  var FAMILIES = {
    video: ['mp4', 'mkv', 'avi', 'mov', 'wmv', 'webm', 'm4v', 'mpg', 'mpeg', 'ts', 'flv'],
    image: ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'tif', 'tiff', 'heic', 'webp', 'raw', 'cr2', 'nef', 'psd', 'svg', 'ico'],
    audio: ['mp3', 'wav', 'flac', 'aac', 'ogg', 'm4a', 'wma'],
    archive: ['zip', '7z', 'rar', 'gz', 'tgz', 'tar', 'bz2', 'xz', 'cab', 'zst', 'lz4'],
    disk: ['vhd', 'vhdx', 'avhdx', 'iso', 'vmdk', 'vdi', 'wim', 'esd', 'swm', 'qcow2', 'img'],
    binary: ['exe', 'dll', 'sys', 'msi', 'msp', 'ocx', 'drv', 'mui', 'cpl', 'efi', 'so', 'lib', 'pdb', 'node'],
    code: ['js', 'ts', 'go', 'c', 'h', 'cpp', 'cs', 'py', 'java', 'rs', 'json', 'xml', 'yml', 'yaml', 'html', 'css', 'ps1', 'psm1', 'bat', 'cmd', 'sh', 'sql', 'map'],
    logs: ['log', 'etl', 'evtx', 'trc', 'dmp', 'mdmp', 'hdmp', 'wer', 'tmp', 'temp', 'old', 'bak', 'trn'],
    data: ['db', 'sqlite', 'mdf', 'ldf', 'ndf', 'edb', 'ost', 'pst', 'dat', 'bin', 'pak', 'cache', 'blob'],
    docs: ['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'txt', 'rtf', 'md', 'csv', 'msg', 'eml']
  };
  var COLORS = {
    video: [226, 104, 70], image: [232, 170, 60], audio: [226, 122, 190], archive: [142, 110, 196],
    disk: [150, 44, 92], binary: [76, 132, 196], code: [70, 168, 160], logs: [150, 150, 96],
    data: [96, 120, 168], docs: [110, 168, 96], other: [128, 140, 152], others: [90, 98, 108], dir: [70, 80, 92]
  };
  var byExt = {};
  Object.keys(FAMILIES).forEach(function (f) { FAMILIES[f].forEach(function (e) { byExt[e] = f; }); });

  function familyOf(node) {
    if (node.o) return 'others';
    if (node.d) return 'dir';
    return byExt[node.e] || 'other';
  }

  // ---- Rendering ----------------------------------------------------------

  var HEADER = 15;   // folder title strip height
  var PAD = 2;       // inner padding of folders
  var MIN_LABEL_W = 46, MIN_LABEL_H = 13;

  function defaultFormat(n) {
    var u = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'], k = 0;
    while (n >= 1024 && k < u.length - 1) { n /= 1024; k++; }
    return (k === 0 ? n : n.toFixed(n >= 100 ? 0 : n >= 10 ? 1 : 2)) + ' ' + u[k];
  }

  /**
   * new Treemap(container, {onClick(node, event), onContext(node, event),
   * format(bytes), othersLabel(count), dark})
   */
  function Treemap(container, opts) {
    this.opts = opts || {};
    this.container = container;
    this.canvas = document.createElement('canvas');
    this.canvas.className = 'cad-treemap-canvas';
    this.canvas.style.display = 'block';
    this.canvas.style.width = '100%';
    this.canvas.style.height = '100%';
    this.tip = document.createElement('div');
    this.tip.className = 'cad-treemap-tip';
    this.tip.style.cssText = 'position:fixed;pointer-events:none;display:none;z-index:50;padding:6px 8px;border-radius:6px;' +
      'font:12px system-ui,sans-serif;background:rgba(20,24,30,.94);color:#eef;box-shadow:0 4px 14px rgba(0,0,0,.35);max-width:520px;word-break:break-all';
    container.appendChild(this.canvas);
    document.body.appendChild(this.tip);
    this.rects = [];
    this.hover = null;
    this.selected = null;
    var self = this;
    this.canvas.addEventListener('mousemove', function (e) { self._move(e); });
    this.canvas.addEventListener('mouseleave', function () { self.hover = null; self.tip.style.display = 'none'; self.draw(); });
    this.canvas.addEventListener('click', function (e) {
      var r = self._hit(e);
      if (r && self.opts.onClick) self.opts.onClick(r.node, e, r.path);
    });
    this.canvas.addEventListener('contextmenu', function (e) {
      var r = self._hit(e);
      if (self.opts.onContext) { e.preventDefault(); self.opts.onContext(r ? r.node : null, e, r ? r.path : null); }
    });
    if (g.ResizeObserver) {
      this._ro = new g.ResizeObserver(function () { self.render(); });
      this._ro.observe(container);
    } else {
      g.addEventListener('resize', function () { self.render(); });
    }
  }

  Treemap.prototype.setData = function (root) {
    this.root = root;
    this.hover = null;
    this.render();
  };

  Treemap.prototype.select = function (id) {
    this.selected = id;
    this.draw();
  };

  Treemap.prototype.destroy = function () {
    if (this._ro) this._ro.disconnect();
    this.tip.remove();
    this.canvas.remove();
  };

  Treemap.prototype.render = function () {
    var w = this.container.clientWidth, h = this.container.clientHeight;
    var dpr = g.devicePixelRatio || 1;
    this.canvas.width = Math.max(1, Math.round(w * dpr));
    this.canvas.height = Math.max(1, Math.round(h * dpr));
    this.dpr = dpr;
    this.w = w;
    this.h = h;
    this.rects = [];
    if (this.root && w > 0 && h > 0) this._layout(this.root, 0, 0, w, h, 0, []);
    this.draw();
  };

  Treemap.prototype._layout = function (node, x, y, w, h, depth, path) {
    var rect = { node: node, x: x, y: y, w: w, h: h, depth: depth, path: path, header: false };
    this.rects.push(rect);
    var kids = node.c;
    if (!kids || !kids.length || w < 6 || h < 6) return;
    var ix = x + PAD, iy = y + PAD, iw = w - 2 * PAD, ih = h - 2 * PAD;
    if (depth > 0 && h > HEADER * 2.2 && w > 50) {
      rect.header = true;
      iy += HEADER - PAD;
      ih -= HEADER - PAD;
    }
    if (iw <= 2 || ih <= 2) return;
    var items = [];
    for (var k = 0; k < kids.length; k++) if (kids[k].s > 0) items.push({ value: kids[k].s, node: kids[k] });
    items.sort(function (a, b) { return b.value - a.value; });
    squarify(items, ix, iy, iw, ih);
    var sub = path.concat([node]);
    for (var j = 0; j < items.length; j++) {
      var it = items[j];
      if (it.w < 0.5 || it.h < 0.5) continue;
      this._layout(it.node, it.x, it.y, it.w, it.h, depth + 1, sub);
    }
  };

  Treemap.prototype.draw = function () {
    var ctx = this.canvas.getContext('2d');
    ctx.setTransform(this.dpr, 0, 0, this.dpr, 0, 0);
    var dark = this.opts.dark !== false;
    ctx.fillStyle = dark ? '#151a21' : '#f4f5f7';
    ctx.fillRect(0, 0, this.w, this.h);
    ctx.font = '11px system-ui, -apple-system, Segoe UI, sans-serif';
    ctx.textBaseline = 'middle';
    var fmt = this.opts.format || defaultFormat;
    for (var k = 0; k < this.rects.length; k++) {
      var r = this.rects[k], n = r.node;
      if (r.w < 1 || r.h < 1) continue;
      var fam = familyOf(n);
      var col = COLORS[fam];
      var isFolder = n.d && n.c && n.c.length;
      if (isFolder) {
        if (r.depth === 0) continue;
        var shade = Math.max(0, 34 - r.depth * 4);
        ctx.fillStyle = dark ? 'rgb(' + (40 + shade) + ',' + (46 + shade) + ',' + (56 + shade) + ')' : 'rgb(' + (222 - shade) + ',' + (226 - shade) + ',' + (232 - shade) + ')';
        ctx.fillRect(r.x, r.y, r.w, r.h);
        if (r.header) {
          ctx.fillStyle = dark ? 'rgba(230,236,245,.85)' : 'rgba(20,24,30,.85)';
          this._label(ctx, n.n + '  ' + fmt(n.s), r.x + 4, r.y + HEADER / 2 + 1, r.w - 8);
        }
      } else {
        var gr = ctx.createLinearGradient(r.x, r.y, r.x + r.w, r.y + r.h);
        var l = n.o ? 0.85 : 1.08;
        gr.addColorStop(0, 'rgb(' + Math.min(255, col[0] * l + 26) + ',' + Math.min(255, col[1] * l + 26) + ',' + Math.min(255, col[2] * l + 26) + ')');
        gr.addColorStop(1, 'rgb(' + col[0] * 0.72 + ',' + col[1] * 0.72 + ',' + col[2] * 0.72 + ')');
        ctx.fillStyle = gr;
        ctx.fillRect(r.x, r.y, r.w, r.h);
        if (n.o) {
          ctx.save();
          ctx.beginPath();
          ctx.rect(r.x, r.y, r.w, r.h);
          ctx.clip();
          ctx.strokeStyle = 'rgba(255,255,255,.08)';
          for (var d = -r.h; d < r.w; d += 7) { ctx.beginPath(); ctx.moveTo(r.x + d, r.y + r.h); ctx.lineTo(r.x + d + r.h, r.y); ctx.stroke(); }
          ctx.restore();
        }
        ctx.strokeStyle = dark ? 'rgba(0,0,0,.45)' : 'rgba(255,255,255,.7)';
        ctx.lineWidth = 1;
        ctx.strokeRect(r.x + 0.5, r.y + 0.5, r.w - 1, r.h - 1);
        if (r.w > MIN_LABEL_W && r.h > MIN_LABEL_H) {
          ctx.fillStyle = 'rgba(255,255,255,.92)';
          var name = n.o ? this._othersLabel(n) : n.n;
          this._label(ctx, name, r.x + 4, r.y + Math.min(r.h / 2, 10), r.w - 8);
          if (r.h > 30) {
            ctx.fillStyle = 'rgba(255,255,255,.7)';
            this._label(ctx, fmt(n.s), r.x + 4, r.y + 23, r.w - 8);
          }
        }
      }
    }
    var mark = function (r, color, width) {
      ctx.strokeStyle = color;
      ctx.lineWidth = width;
      ctx.strokeRect(r.x + width / 2, r.y + width / 2, r.w - width, r.h - width);
    };
    if (this.selected != null) {
      for (var s = 0; s < this.rects.length; s++) {
        if (this.rects[s].node.i === this.selected && !this.rects[s].node.o && this.rects[s].depth > 0) mark(this.rects[s], '#ffd54a', 2);
      }
    }
    if (this.hover) mark(this.hover, dark ? '#ffffff' : '#111', 1.5);
  };

  Treemap.prototype._othersLabel = function (n) {
    return this.opts.othersLabel ? this.opts.othersLabel(n.o) : n.o + ' others';
  };

  Treemap.prototype._label = function (ctx, text, x, y, maxW) {
    if (maxW < 12) return;
    var t = text;
    if (ctx.measureText(t).width > maxW) {
      var lo = 0, hi = t.length;
      while (lo < hi) {
        var mid = (lo + hi + 1) >> 1;
        if (ctx.measureText(t.slice(0, mid) + '…').width <= maxW) lo = mid; else hi = mid - 1;
      }
      t = lo > 0 ? t.slice(0, lo) + '…' : '';
    }
    if (t) ctx.fillText(t, x, y);
  };

  Treemap.prototype._hit = function (e) {
    var b = this.canvas.getBoundingClientRect();
    var x = e.clientX - b.left, y = e.clientY - b.top;
    for (var k = this.rects.length - 1; k >= 0; k--) {
      var r = this.rects[k];
      if (r.depth > 0 && x >= r.x && x < r.x + r.w && y >= r.y && y < r.y + r.h) return r;
    }
    return null;
  };

  Treemap.prototype._move = function (e) {
    var r = this._hit(e);
    if (r !== this.hover) {
      this.hover = r;
      this.draw();
    }
    if (!r) { this.tip.style.display = 'none'; return; }
    var fmt = this.opts.format || defaultFormat;
    var parts = [];
    for (var k = 1; k < r.path.length; k++) parts.push(r.path[k].n);
    var name = r.node.o ? this._othersLabel(r.node) : r.node.n;
    parts.push(name);
    var esc = function (s) { return String(s).replace(/[&<>"]/g, function (c) { return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]; }); };
    this.tip.innerHTML = '<b>' + esc(name) + '</b><br>' + esc(fmt(r.node.s)) + '<br><span style="opacity:.7">' + esc(parts.join(' › ')) + '</span>';
    this.tip.style.display = 'block';
    var tx = e.clientX + 14, ty = e.clientY + 14;
    var tw = this.tip.offsetWidth, th = this.tip.offsetHeight;
    if (tx + tw > g.innerWidth - 4) tx = e.clientX - tw - 10;
    if (ty + th > g.innerHeight - 4) ty = e.clientY - th - 10;
    this.tip.style.left = tx + 'px';
    this.tip.style.top = ty + 'px';
  };

  /** Finds the node with id in the tree (depth-first). */
  function find(root, id) {
    var stack = [root];
    while (stack.length) {
      var n = stack.pop();
      if (n.i === id && !n.o) return n;
      if (n.c) for (var k = 0; k < n.c.length; k++) stack.push(n.c[k]);
    }
    return null;
  }

  g.CadastreTreemap = {
    Treemap: Treemap,
    squarify: squarify,
    familyOf: familyOf,
    colors: COLORS,
    families: Object.keys(FAMILIES).concat(['other']),
    find: find
  };
})(typeof window !== 'undefined' ? window : globalThis);
