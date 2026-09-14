export namespace engine {
	
	export class ConsistencyResult {
	    consistent: boolean;
	    reason?: string;
	    totalBytes: number;
	    etag: string;
	    resumable: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ConsistencyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.consistent = source["consistent"];
	        this.reason = source["reason"];
	        this.totalBytes = source["totalBytes"];
	        this.etag = source["etag"];
	        this.resumable = source["resumable"];
	    }
	}
	export class ProbeResult {
	    url: string;
	    filename: string;
	    totalBytes: number;
	    resumable: boolean;
	    etag: string;
	    lastModified: string;
	    contentType: string;
	    duplicateTask?: task.Task;
	
	    static createFrom(source: any = {}) {
	        return new ProbeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.filename = source["filename"];
	        this.totalBytes = source["totalBytes"];
	        this.resumable = source["resumable"];
	        this.etag = source["etag"];
	        this.lastModified = source["lastModified"];
	        this.contentType = source["contentType"];
	        this.duplicateTask = this.convertValues(source["duplicateTask"], task.Task);
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
	
	export class FileConflictResult {
	    exists: boolean;
	    suggestedFilename: string;
	
	    static createFrom(source: any = {}) {
	        return new FileConflictResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.exists = source["exists"];
	        this.suggestedFilename = source["suggestedFilename"];
	    }
	}

}

export namespace task {
	
	export class Chunk {
	    index: number;
	    start: number;
	    end: number;
	    downloaded: number;
	    assisted: boolean;
	    completed: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Chunk(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.start = source["start"];
	        this.end = source["end"];
	        this.downloaded = source["downloaded"];
	        this.assisted = source["assisted"];
	        this.completed = source["completed"];
	    }
	}
	export class Task {
	    id: string;
	    url: string;
	    filename: string;
	    directory: string;
	    totalBytes: number;
	    downloaded: number;
	    speed: number;
	    status: string;
	    errorMsg?: string;
	    maxConcurrency: number;
	    resumable: boolean;
	    etag?: string;
	    lastModified?: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    chunks?: Chunk[];
	    requestHeaders?: Record<string, string>;
	
	    static createFrom(source: any = {}) {
	        return new Task(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.filename = source["filename"];
	        this.directory = source["directory"];
	        this.totalBytes = source["totalBytes"];
	        this.downloaded = source["downloaded"];
	        this.speed = source["speed"];
	        this.status = source["status"];
	        this.errorMsg = source["errorMsg"];
	        this.maxConcurrency = source["maxConcurrency"];
	        this.resumable = source["resumable"];
	        this.etag = source["etag"];
	        this.lastModified = source["lastModified"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.chunks = this.convertValues(source["chunks"], Chunk);
	        this.requestHeaders = source["requestHeaders"];
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

