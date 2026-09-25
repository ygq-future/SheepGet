export class AppUpdateResult {
    "hasUpdate": boolean;
    "currentVersion": string;
    "latestVersion": string;
    "releaseTitle": string;
    "releaseNotes": string;
    "releaseUrl": string;
    "assetName": string;
    "assetUrl": string;
    "assetSize": number;
    "isPortable": boolean;

    constructor($$source: Partial<AppUpdateResult> = {}) {
        this["hasUpdate"] = $$source["hasUpdate"] ?? false;
        this["currentVersion"] = $$source["currentVersion"] ?? "";
        this["latestVersion"] = $$source["latestVersion"] ?? "";
        this["releaseTitle"] = $$source["releaseTitle"] ?? "";
        this["releaseNotes"] = $$source["releaseNotes"] ?? "";
        this["releaseUrl"] = $$source["releaseUrl"] ?? "";
        this["assetName"] = $$source["assetName"] ?? "";
        this["assetUrl"] = $$source["assetUrl"] ?? "";
        this["assetSize"] = $$source["assetSize"] ?? 0;
        this["isPortable"] = $$source["isPortable"] ?? false;
        Object.assign(this, $$source);
    }
}

export class ExtensionUpdateResult {
    "hasUpdate": boolean;
    "currentVersion": string;
    "latestVersion": string;
    "releaseTitle": string;
    "releaseNotes": string;
    "assetName": string;
    "assetUrl": string;
    "assetSize": number;

    constructor($$source: Partial<ExtensionUpdateResult> = {}) {
        this["hasUpdate"] = $$source["hasUpdate"] ?? false;
        this["currentVersion"] = $$source["currentVersion"] ?? "";
        this["latestVersion"] = $$source["latestVersion"] ?? "";
        this["releaseTitle"] = $$source["releaseTitle"] ?? "";
        this["releaseNotes"] = $$source["releaseNotes"] ?? "";
        this["assetName"] = $$source["assetName"] ?? "";
        this["assetUrl"] = $$source["assetUrl"] ?? "";
        this["assetSize"] = $$source["assetSize"] ?? 0;
        Object.assign(this, $$source);
    }
}

export class DownloadProgress {
    "downloadedBytes": number;
    "totalBytes": number;
    "percentage": number;
    "speedBps": number;

    constructor($$source: Partial<DownloadProgress> = {}) {
        this["downloadedBytes"] = $$source["downloadedBytes"] ?? 0;
        this["totalBytes"] = $$source["totalBytes"] ?? 0;
        this["percentage"] = $$source["percentage"] ?? 0;
        this["speedBps"] = $$source["speedBps"] ?? 0;
        Object.assign(this, $$source);
    }
}
