export namespace index {
	
	export class ExtStat {
	    Ext: string;
	    Files: number;
	    Size: number;
	    Alloc: number;
	
	    static createFrom(source: any = {}) {
	        return new ExtStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Ext = source["Ext"];
	        this.Files = source["Files"];
	        this.Size = source["Size"];
	        this.Alloc = source["Alloc"];
	    }
	}
	export class Totals {
	    Files: number;
	    Dirs: number;
	    Size: number;
	    Alloc: number;
	    ADSFiles: number;
	    ADSStreams: number;
	    ADSSize: number;
	    ADSAlloc: number;
	    MetaFiles: number;
	    MetaSize: number;
	    MetaAlloc: number;
	    Hardlinks: number;
	    Reparse: number;
	    Cloud: number;
	    Orphans: number;
	    Denied: number;
	    Folded: number;
	    Compressed: number;
	    Sparse: number;
	    WOF: number;
	    CompressedSaved: number;
	
	    static createFrom(source: any = {}) {
	        return new Totals(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Files = source["Files"];
	        this.Dirs = source["Dirs"];
	        this.Size = source["Size"];
	        this.Alloc = source["Alloc"];
	        this.ADSFiles = source["ADSFiles"];
	        this.ADSStreams = source["ADSStreams"];
	        this.ADSSize = source["ADSSize"];
	        this.ADSAlloc = source["ADSAlloc"];
	        this.MetaFiles = source["MetaFiles"];
	        this.MetaSize = source["MetaSize"];
	        this.MetaAlloc = source["MetaAlloc"];
	        this.Hardlinks = source["Hardlinks"];
	        this.Reparse = source["Reparse"];
	        this.Cloud = source["Cloud"];
	        this.Orphans = source["Orphans"];
	        this.Denied = source["Denied"];
	        this.Folded = source["Folded"];
	        this.Compressed = source["Compressed"];
	        this.Sparse = source["Sparse"];
	        this.WOF = source["WOF"];
	        this.CompressedSaved = source["CompressedSaved"];
	    }
	}

}

export namespace main {
	
	export class Info {
	    version: string;
	    lang: string;
	    admin: boolean;
	    os: string;
	    catalog: Record<string, string>;
	    rules: number;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.lang = source["lang"];
	        this.admin = source["admin"];
	        this.os = source["os"];
	        this.catalog = source["catalog"];
	        this.rules = source["rules"];
	    }
	}
	export class Summary {
	    root: string;
	    rootId: number;
	    label: string;
	    fs: string;
	    engine: string;
	    duration: number;
	    admin: boolean;
	    fromSnapshot: boolean;
	    // Go type: time
	    scannedAt: any;
	    totals: index.Totals;
	    warnings: string[];
	    recon?: reclaim.Reconciliation;
	    reclaimable: number;
	    entries: number;
	    memoryMb: number;
	
	    static createFrom(source: any = {}) {
	        return new Summary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.rootId = source["rootId"];
	        this.label = source["label"];
	        this.fs = source["fs"];
	        this.engine = source["engine"];
	        this.duration = source["duration"];
	        this.admin = source["admin"];
	        this.fromSnapshot = source["fromSnapshot"];
	        this.scannedAt = this.convertValues(source["scannedAt"], null);
	        this.totals = this.convertValues(source["totals"], index.Totals);
	        this.warnings = source["warnings"];
	        this.recon = this.convertValues(source["recon"], reclaim.Reconciliation);
	        this.reclaimable = source["reclaimable"];
	        this.entries = source["entries"];
	        this.memoryMb = source["memoryMb"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class VolumeView {
	    root: string;
	    label: string;
	    fs: string;
	    kind: string;
	    total: number;
	    free: number;
	    canMft: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VolumeView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.label = source["label"];
	        this.fs = source["fs"];
	        this.kind = source["kind"];
	        this.total = source["total"];
	        this.free = source["free"];
	        this.canMft = source["canMft"];
	    }
	}

}

export namespace reclaim {
	
	export class Detail {
	    name: string;
	    bytes: number;
	
	    static createFrom(source: any = {}) {
	        return new Detail(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.bytes = source["bytes"];
	    }
	}
	export class Component {
	    key: string;
	    bytes: number;
	    count: number;
	    details?: Detail[];
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new Component(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.bytes = source["bytes"];
	        this.count = source["count"];
	        this.details = this.convertValues(source["details"], Detail);
	        this.source = source["source"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class VSSInfo {
	    known: boolean;
	    used: number;
	    allocated: number;
	    max: number;
	    copies: number;
	
	    static createFrom(source: any = {}) {
	        return new VSSInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.known = source["known"];
	        this.used = source["used"];
	        this.allocated = source["allocated"];
	        this.max = source["max"];
	        this.copies = source["copies"];
	    }
	}
	export class Reconciliation {
	    total: number;
	    free: number;
	    used: number;
	    seen: number;
	    missing: number;
	    components: Component[];
	    unattributed: number;
	    engine: string;
	    vss: VSSInfo;
	    denied: number;
	    anomaly: boolean;
	    notes: string[];
	
	    static createFrom(source: any = {}) {
	        return new Reconciliation(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.free = source["free"];
	        this.used = source["used"];
	        this.seen = source["seen"];
	        this.missing = source["missing"];
	        this.components = this.convertValues(source["components"], Component);
	        this.unattributed = source["unattributed"];
	        this.engine = source["engine"];
	        this.vss = this.convertValues(source["vss"], VSSInfo);
	        this.denied = source["denied"];
	        this.anomaly = source["anomaly"];
	        this.notes = source["notes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace view {
	
	export class Item {
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
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.dir = source["dir"];
	        this.size = source["size"];
	        this.alloc = source["alloc"];
	        this.mtime = source["mtime"];
	        this.files = source["files"];
	        this.ext = source["ext"];
	        this.flags = source["flags"];
	        this.grouped = source["grouped"];
	    }
	}
	export class Listing {
	    dir: Item;
	    path: Item[];
	    items: Item[];
	    total: number;
	
	    static createFrom(source: any = {}) {
	        return new Listing(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dir = this.convertValues(source["dir"], Item);
	        this.path = this.convertValues(source["path"], Item);
	        this.items = this.convertValues(source["items"], Item);
	        this.total = source["total"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Node {
	    i: number;
	    n: string;
	    s: number;
	    d?: boolean;
	    e?: string;
	    o?: number;
	    c?: Node[];
	
	    static createFrom(source: any = {}) {
	        return new Node(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.i = source["i"];
	        this.n = source["n"];
	        this.s = source["s"];
	        this.d = source["d"];
	        this.e = source["e"];
	        this.o = source["o"];
	        this.c = this.convertValues(source["c"], Node);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

