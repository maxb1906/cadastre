export namespace clean {
	
	export class Item {
	    entry: number;
	    path: string;
	    rule?: string;
	    risk: string;
	    dir?: boolean;
	    reparse?: boolean;
	    size: number;
	    alloc: number;
	    mtime: number;
	    fileId?: number;
	    hardlink?: boolean;
	    permanentOnly?: boolean;
	    system?: boolean;
	    closeFirst?: string[];
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.entry = source["entry"];
	        this.path = source["path"];
	        this.rule = source["rule"];
	        this.risk = source["risk"];
	        this.dir = source["dir"];
	        this.reparse = source["reparse"];
	        this.size = source["size"];
	        this.alloc = source["alloc"];
	        this.mtime = source["mtime"];
	        this.fileId = source["fileId"];
	        this.hardlink = source["hardlink"];
	        this.permanentOnly = source["permanentOnly"];
	        this.system = source["system"];
	        this.closeFirst = source["closeFirst"];
	    }
	}

}

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
	
	export class ActionView {
	    rule: string;
	    kind: string;
	    risk: string;
	    files: number;
	    bytes: number;
	    title: string;
	    official?: string;
	    commands?: string[];
	    targets?: string[];
	    restorePoint?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ActionView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule = source["rule"];
	        this.kind = source["kind"];
	        this.risk = source["risk"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.title = source["title"];
	        this.official = source["official"];
	        this.commands = source["commands"];
	        this.targets = source["targets"];
	        this.restorePoint = source["restorePoint"];
	    }
	}
	export class CleanRequest {
	    rules: string[];
	    entries: number[];
	    mode: string;
	    official: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CleanRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rules = source["rules"];
	        this.entries = source["entries"];
	        this.mode = source["mode"];
	        this.official = source["official"];
	    }
	}
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
	export class OfficialView {
	    rule: string;
	    title: string;
	    risk: string;
	    commands: string[];
	    items: number;
	    alloc: number;
	    restorePoint: boolean;
	    targets: string[];
	
	    static createFrom(source: any = {}) {
	        return new OfficialView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rule = source["rule"];
	        this.title = source["title"];
	        this.risk = source["risk"];
	        this.commands = source["commands"];
	        this.items = source["items"];
	        this.alloc = source["alloc"];
	        this.restorePoint = source["restorePoint"];
	        this.targets = source["targets"];
	    }
	}
	export class SkipGroup {
	    reason: string;
	    label: string;
	    count: number;
	    alloc: number;
	    examples: string[];
	
	    static createFrom(source: any = {}) {
	        return new SkipGroup(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.reason = source["reason"];
	        this.label = source["label"];
	        this.count = source["count"];
	        this.alloc = source["alloc"];
	        this.examples = source["examples"];
	    }
	}
	export class PlanView {
	    id: string;
	    mode: string;
	    files: number;
	    bytes: number;
	    orange: boolean;
	    fromSnapshot: boolean;
	    items: clean.Item[];
	    moreItems: number;
	    skipped: SkipGroup[];
	    official: OfficialView[];
	    actions: ActionView[];
	    restorePoint: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PlanView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.mode = source["mode"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.orange = source["orange"];
	        this.fromSnapshot = source["fromSnapshot"];
	        this.items = this.convertValues(source["items"], clean.Item);
	        this.moreItems = source["moreItems"];
	        this.skipped = this.convertValues(source["skipped"], SkipGroup);
	        this.official = this.convertValues(source["official"], OfficialView);
	        this.actions = this.convertValues(source["actions"], ActionView);
	        this.restorePoint = source["restorePoint"];
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

