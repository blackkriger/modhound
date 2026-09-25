export namespace backup {
	
	export class Item {
	    key: string;
	    name: string;
	    dir: string;
	    oldFile: string;
	    stored: string;
	    newFile: string;
	    from: string;
	    to: string;
	    restored: boolean;
	    time?: string;
	    remote?: string;
	
	    static createFrom(source: any = {}) {
	        return new Item(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.dir = source["dir"];
	        this.oldFile = source["oldFile"];
	        this.stored = source["stored"];
	        this.newFile = source["newFile"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.restored = source["restored"];
	        this.time = source["time"];
	        this.remote = source["remote"];
	    }
	}
	export class Result {
	    key: string;
	    name: string;
	    from: string;
	    to: string;
	    file: string;
	    ok: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.name = source["name"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.file = source["file"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	    }
	}

}

export namespace install {
	
	export class Result {
	    id: string;
	    name: string;
	    from: string;
	    to: string;
	    fileFrom: string;
	    fileTo: string;
	    ok: boolean;
	    error: string;
	    warning: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.from = source["from"];
	        this.to = source["to"];
	        this.fileFrom = source["fileFrom"];
	        this.fileTo = source["fileTo"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	        this.warning = source["warning"];
	    }
	}

}

export namespace jarinfo {
	
	export class ModInfo {
	    modid: string;
	    name: string;
	    description: string;
	    version: string;
	    mcversion: string;
	    url: string;
	    authorList: string[];
	    authors: string[];
	    logoFile: string;
	    requiredMods: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.modid = source["modid"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.version = source["version"];
	        this.mcversion = source["mcversion"];
	        this.url = source["url"];
	        this.authorList = source["authorList"];
	        this.authors = source["authors"];
	        this.logoFile = source["logoFile"];
	        this.requiredMods = source["requiredMods"];
	    }
	}
	export class Jar {
	    Size: number;
	    SHA1: string;
	    SHA512: string;
	    Murmur2: number;
	    Info?: ModInfo;
	    Logo: number[];
	    LogoType: string;
	    ModIDs: string[];
	    Requires: string[];
	    ClassMajor: number;
	    Valid: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Jar(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Size = source["Size"];
	        this.SHA1 = source["SHA1"];
	        this.SHA512 = source["SHA512"];
	        this.Murmur2 = source["Murmur2"];
	        this.Info = this.convertValues(source["Info"], ModInfo);
	        this.Logo = source["Logo"];
	        this.LogoType = source["LogoType"];
	        this.ModIDs = source["ModIDs"];
	        this.Requires = source["Requires"];
	        this.ClassMajor = source["ClassMajor"];
	        this.Valid = source["Valid"];
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

export namespace main {
	
	export class LastUpdate {
	    restorable: backup.Item[];
	
	    static createFrom(source: any = {}) {
	        return new LastUpdate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.restorable = this.convertValues(source["restorable"], backup.Item);
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
	export class Manual {
	    id: string;
	    name: string;
	    reason: string;
	    pageUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new Manual(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.reason = source["reason"];
	        this.pageUrl = source["pageUrl"];
	    }
	}
	export class ServerSync {
	    root: string;
	    remote: boolean;
	    results: server.Result[];
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerSync(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.remote = source["remote"];
	        this.results = this.convertValues(source["results"], server.Result);
	        this.error = source["error"];
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
	export class Report {
	    time: string;
	    pack: string;
	    installed: install.Result[];
	    failed: install.Result[];
	    upToDate: number;
	    skipped: string[];
	    remaining: string[];
	    manual: Manual[];
	    unknown: string[];
	    server?: ServerSync;
	    path: string;
	    rechecked: resolve.Replaced[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.time = source["time"];
	        this.pack = source["pack"];
	        this.installed = this.convertValues(source["installed"], install.Result);
	        this.failed = this.convertValues(source["failed"], install.Result);
	        this.upToDate = source["upToDate"];
	        this.skipped = source["skipped"];
	        this.remaining = source["remaining"];
	        this.manual = this.convertValues(source["manual"], Manual);
	        this.unknown = source["unknown"];
	        this.server = this.convertValues(source["server"], ServerSync);
	        this.path = source["path"];
	        this.rechecked = this.convertValues(source["rechecked"], resolve.Replaced);
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
	
	export class Settings {
	    curseforgeKey: string;
	    theme: string;
	    debug: boolean;
	    lastPack: string;
	    server: string;
	    serverKey: string;
	    sort: Record<string, string>;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.curseforgeKey = source["curseforgeKey"];
	        this.theme = source["theme"];
	        this.debug = source["debug"];
	        this.lastPack = source["lastPack"];
	        this.server = source["server"];
	        this.serverKey = source["serverKey"];
	        this.sort = source["sort"];
	        this.version = source["version"];
	    }
	}
	export class UndoResult {
	    results: backup.Result[];
	    mods: resolve.Replaced[];
	
	    static createFrom(source: any = {}) {
	        return new UndoResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.results = this.convertValues(source["results"], backup.Result);
	        this.mods = this.convertValues(source["mods"], resolve.Replaced);
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

export namespace resolve {
	
	export class Choice {
	    id: string;
	    version: string;
	    fileName: string;
	    date: string;
	    installed: boolean;
	    pre: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Choice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.version = source["version"];
	        this.fileName = source["fileName"];
	        this.date = source["date"];
	        this.installed = source["installed"];
	        this.pre = source["pre"];
	    }
	}
	export class ModDebug {
	    path: string;
	    sha1: string;
	    fingerprint: number;
	    classMajor: number;
	    gtnh?: string;
	    curseforge?: string;
	    modrinth?: string;
	
	    static createFrom(source: any = {}) {
	        return new ModDebug(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.sha1 = source["sha1"];
	        this.fingerprint = source["fingerprint"];
	        this.classMajor = source["classMajor"];
	        this.gtnh = source["gtnh"];
	        this.curseforge = source["curseforge"];
	        this.modrinth = source["modrinth"];
	    }
	}
	export class Target {
	    version: string;
	    fileName: string;
	    date: string;
	    pageUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new Target(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.fileName = source["fileName"];
	        this.date = source["date"];
	        this.pageUrl = source["pageUrl"];
	    }
	}
	export class Mod {
	    id: string;
	    key: string;
	    fileName: string;
	    relPath: string;
	    name: string;
	    version: string;
	    authors: string[];
	    description: string;
	    icon: string;
	    url: string;
	    source: string;
	    status: string;
	    reason: string;
	    target?: Target;
	    skipped: boolean;
	    size: number;
	    debug?: ModDebug;
	
	    static createFrom(source: any = {}) {
	        return new Mod(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.key = source["key"];
	        this.fileName = source["fileName"];
	        this.relPath = source["relPath"];
	        this.name = source["name"];
	        this.version = source["version"];
	        this.authors = source["authors"];
	        this.description = source["description"];
	        this.icon = source["icon"];
	        this.url = source["url"];
	        this.source = source["source"];
	        this.status = source["status"];
	        this.reason = source["reason"];
	        this.target = this.convertValues(source["target"], Target);
	        this.skipped = source["skipped"];
	        this.size = source["size"];
	        this.debug = this.convertValues(source["debug"], ModDebug);
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
	
	export class Pack {
	    root: string;
	    modsDir: string;
	    mcVersion: string;
	    loader: string;
	    mods: Mod[];
	    warnings: string[];
	    curseforgeOk: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Pack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.root = source["root"];
	        this.modsDir = source["modsDir"];
	        this.mcVersion = source["mcVersion"];
	        this.loader = source["loader"];
	        this.mods = this.convertValues(source["mods"], Mod);
	        this.warnings = source["warnings"];
	        this.curseforgeOk = source["curseforgeOk"];
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
	export class Replaced {
	    oldId: string;
	    mod?: Mod;
	
	    static createFrom(source: any = {}) {
	        return new Replaced(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.oldId = source["oldId"];
	        this.mod = this.convertValues(source["mod"], Mod);
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

export namespace server {
	
	export class Result {
	    name: string;
	    file: string;
	    ok: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.file = source["file"];
	        this.ok = source["ok"];
	        this.error = source["error"];
	    }
	}

}

