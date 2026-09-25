import {
  chmodSync,
  cpSync,
  existsSync,
  mkdirSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { resolve, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { createHash } from 'node:crypto';
import { run } from './process.mjs';
import { readInfo } from './version.mjs';

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
process.chdir(root);

const platform = process.platform;
const isWin = platform === 'win32';
const isMac = platform === 'darwin';
const isLinux = !isWin && !isMac;

const platformName = isWin ? 'windows' : isMac ? 'darwin' : 'linux';
// Product metadata comes from build/config.yml `info:` (SSOT) through the same parser the
// version check uses; see scripts/version.mjs.
const { version, productName, companyName, description: productDescription } = readInfo(root);
if (!version || !productName || !companyName || !productDescription) {
  throw new Error(
    'build/config.yml info block must define version, productName, companyName and description',
  );
}

const binDir = join(root, 'build', 'bin');
const toolsDir = join(root, '.tools');
rmSync(binDir, { recursive: true, force: true });
mkdirSync(binDir, { recursive: true });
mkdirSync(toolsDir, { recursive: true });
function resolveWails3() {
  const localExe = join(toolsDir, 'wails3' + (isWin ? '.exe' : ''));
  if (existsSync(localExe)) return localExe;
  return 'wails3';
}

const distDir = join(root, 'build', 'dist');
rmSync(distDir, { recursive: true, force: true });
mkdirSync(distDir, { recursive: true });

function formatSize(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

async function downloadFile(url, destPath) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`Failed to download ${url}: ${res.statusText}`);
  const buf = Buffer.from(await res.arrayBuffer());
  writeFileSync(destPath, buf);
}

async function ensureWindowsTools() {
  if (!isWin) return;

  // 1. Ensure WebView2 Bootstrapper
  const webviewTmpDir = join(root, 'build', 'windows', 'installer', 'tmp');
  mkdirSync(webviewTmpDir, { recursive: true });
  const webviewSetup = join(webviewTmpDir, 'MicrosoftEdgeWebview2Setup.exe');
  if (!existsSync(webviewSetup)) {
    console.log('[Package] Fetching WebView2 Bootstrapper...');
    await downloadFile('https://go.microsoft.com/fwlink/p/?LinkId=2124703', webviewSetup);
  }

  // 2. Ensure portable NSIS (makensis.exe)
  const nsisDir = join(toolsDir, 'nsis');
  const nsisExe = join(nsisDir, 'makensis.exe');
  if (!existsSync(nsisExe)) {
    console.log('[Package] Fetching portable NSIS compiler into .tools/nsis...');
    mkdirSync(nsisDir, { recursive: true });
    const nsisZip = join(toolsDir, 'nsis-temp.zip');
    await downloadFile(
      'https://github.com/tauri-apps/binary-releases/releases/download/nsis-3/nsis-3.zip',
      nsisZip,
    );
    run('tar', ['-xf', nsisZip, '-C', toolsDir]);
    rmSync(nsisZip, { force: true });
    const extractedNsis = join(toolsDir, 'nsis-3.08');
    if (existsSync(extractedNsis)) {
      cpSync(extractedNsis, nsisDir, { recursive: true });
      rmSync(extractedNsis, { recursive: true, force: true });
    }
  }

  // 3. Ensure portable WiX (candle.exe / light.exe)
  const wixDir = join(toolsDir, 'wix');
  const wixCandle = join(wixDir, 'candle.exe');
  if (!existsSync(wixCandle)) {
    console.log('[Package] Fetching portable WiX Toolset into .tools/wix...');
    mkdirSync(wixDir, { recursive: true });
    const wixZip = join(toolsDir, 'wix-temp.zip');
    await downloadFile(
      'https://github.com/wixtoolset/wix3/releases/download/wix3141rtm/wix314-binaries.zip',
      wixZip,
    );
    run('tar', ['-xf', wixZip, '-C', wixDir]);
    rmSync(wixZip, { force: true });
  }
}

function generateWindowsSyso(targetArch) {
  const sysoTarget = join(root, `resource_windows_${targetArch}.syso`);
  const tempInfoTarget = join(toolsDir, `info_windows_${targetArch}.json`);
  const commonFields = {
    ProductVersion: version,
    FileVersion: version,
    CompanyName: companyName,
    FileDescription: productName,
    LegalCopyright: `Copyright © ${new Date().getFullYear()} ${companyName}`,
    ProductName: productName,
    Comments: productDescription,
  };
  const infoPayload = {
    fixed: {
      file_version: version,
      product_version: version,
    },
    info: {
      '0409': commonFields,
      '0804': commonFields,
      '0000': commonFields,
    },
  };
  writeFileSync(tempInfoTarget, JSON.stringify(infoPayload, null, 2), 'utf-8');
  run(resolveWails3(), [
    'generate',
    'syso',
    '-arch',
    targetArch,
    '-icon',
    'build/windows/icon.ico',
    '-info',
    tempInfoTarget,
    '-manifest',
    'build/windows/wails.exe.manifest',
    '-out',
    sysoTarget,
  ]);
  return { sysoTarget, tempInfoTarget };
}

function assembleMacAppBundle(binaryPath, appPath) {
  rmSync(appPath, { recursive: true, force: true });
  const contentsDir = join(appPath, 'Contents');
  const macOSDir = join(contentsDir, 'MacOS');
  const resourcesDir = join(contentsDir, 'Resources');
  mkdirSync(macOSDir, { recursive: true });
  mkdirSync(resourcesDir, { recursive: true });

  cpSync(binaryPath, join(macOSDir, 'SheepGet'));
  chmodSync(join(macOSDir, 'SheepGet'), 0o755);

  const rawPlist = readFileSync(join(root, 'build', 'darwin', 'Info.plist'), 'utf-8');
  const parsedPlist = rawPlist
    .replace(/\{\{\.Info\.ProductName\}\}/g, productName)
    .replace(/\{\{\.OutputFilename\}\}/g, 'SheepGet')
    .replace(/\{\{safeBundleID \.Name\}\}/g, 'com.sheepget.app')
    .replace(/\{\{\.Info\.ProductVersion\}\}/g, version)
    .replace(/\{\{\.Info\.Comments\}\}/g, productDescription)
    .replace(/\{\{\.Info\.Copyright\}\}/g, `Copyright © ${new Date().getFullYear()} ${companyName}`)
    .replace(/\{\{if \.Info\.FileAssociations\}\}[\s\S]*?\{\{end\}\}/g, '')
    .replace(/\{\{if \.Info\.Protocols\}\}[\s\S]*?\{\{end\}\}/g, '');
  writeFileSync(join(contentsDir, 'Info.plist'), parsedPlist, 'utf-8');

  writeFileSync(join(contentsDir, 'PkgInfo'), 'APPL????', 'utf-8');
  cpSync(join(root, 'build', 'darwin', 'icon.icns'), join(resourcesDir, 'iconfile.icns'));
}

async function main() {
  console.log(`[Package] Packaging SheepGet v${version} for ${platformName}...`);

  if (isWin) {
    await ensureWindowsTools();
  }

  // 1. Build browser extension
  console.log('[Package] Building browser extension...');
  run('bun', ['run', 'build'], { cwd: join(root, 'extension'), quiet: true });

  const extSourceDir = join(root, 'build', 'dist-extension', 'chrome-mv3');
  const extTargetDir = join(distDir, 'extension');
  rmSync(extTargetDir, { recursive: true, force: true });
  cpSync(extSourceDir, extTargetDir, { recursive: true });
  cpSync(extTargetDir, join(binDir, 'extension'), { recursive: true });

  const artifacts = [];

  // Standalone browser extension zip for decoupled extension updates
  const extZipName = `SheepGet_${version}_extension-chrome-mv3.zip`;
  const extZipPath = join(distDir, extZipName);
  run('tar', ['-a', '-c', '-f', extZipPath, '-C', extTargetDir, '.']);
  if (existsSync(extZipPath)) {
    artifacts.push({ name: extZipName, path: extZipPath, type: 'Browser Extension Zip' });
  }

  // =========================================================================
  // WINDOWS MATRIX: x64 + arm64
  // =========================================================================
  if (isWin) {
    // A. Windows x64 Build
    console.log('[Package] Building Windows x64 binaries & installers...');
    const x64Exe = join(binDir, 'SheepGet.exe');
    const { sysoTarget: x64Syso, tempInfoTarget: x64Info } = generateWindowsSyso('amd64');
    try {
      run(
        'go',
        [
          'build',
          '-tags=production',
          '-trimpath',
          '-ldflags=-w -s -H=windowsgui',
          '-o',
          x64Exe,
          '.',
        ],
        { env: { ...process.env, GOARCH: 'amd64' } },
      );
    } finally {
      if (existsSync(x64Syso)) rmSync(x64Syso, { force: true });
      if (existsSync(x64Info)) rmSync(x64Info, { force: true });
    }

    // Windows x64 Portable
    const x64PortDir = join(distDir, `SheepGet_${version}_windows-x64-portable`);
    mkdirSync(x64PortDir, { recursive: true });
    cpSync(x64Exe, join(x64PortDir, 'SheepGet.exe'));
    cpSync(extTargetDir, join(x64PortDir, 'extension'), { recursive: true });
    cpSync(join(root, 'README.md'), join(x64PortDir, 'README.md'));
    writeFileSync(join(x64PortDir, 'portable'), '', 'utf8');
    mkdirSync(join(x64PortDir, 'data'), { recursive: true });
    const x64ZipName = `SheepGet_${version}_windows-x64-portable.zip`;
    const x64ZipPath = join(distDir, x64ZipName);
    run('tar', [
      '-a',
      '-c',
      '-f',
      x64ZipPath,
      '-C',
      distDir,
      `SheepGet_${version}_windows-x64-portable`,
    ]);
    artifacts.push({ name: x64ZipName, path: x64ZipPath, type: 'Portable Zip' });

    // Windows x64 NSIS
    const nsisExe = join(toolsDir, 'nsis', 'makensis.exe');
    const nsisScript = join(root, 'build', 'windows', 'installer', 'project.nsi');
    if (existsSync(nsisExe) && existsSync(nsisScript)) {
      console.log('[Package] Building Windows x64 NSIS setup installer (.exe)...');
      run(nsisExe, [
        '-DINFO_PROJECTNAME=SheepGet',
        '-DINFO_PRODUCTNAME=SheepGet',
        '-DINFO_COMPANYNAME=SheepGet',
        `-DINFO_PRODUCTVERSION=${version}`,
        '-DPRODUCT_EXECUTABLE=SheepGet.exe',
        `-DARG_WAILS_AMD64_BINARY=${x64Exe}`,
        nsisScript,
      ]);
      const nsisOut = join(binDir, 'SheepGet-amd64-installer.exe');
      const setupExeName = `SheepGet_${version}_x64-setup.exe`;
      const setupExePath = join(distDir, setupExeName);
      if (existsSync(nsisOut)) {
        cpSync(nsisOut, setupExePath);
        artifacts.push({ name: setupExeName, path: setupExePath, type: 'NSIS Setup Installer' });
      }
    }

    // Windows x64 WiX MSI
    const wixCandle = join(toolsDir, 'wix', 'candle.exe');
    const wixLight = join(toolsDir, 'wix', 'light.exe');
    const wixScript = join(root, 'build', 'windows', 'installer', 'project.wxs');
    if (existsSync(wixCandle) && existsSync(wixLight) && existsSync(wixScript)) {
      console.log('[Package] Building Windows x64 MSI installer (.msi)...');
      const wixObj = join(binDir, 'project.wixobj');
      run(wixCandle, [
        '-arch',
        'x64',
        `-dProductVersion=${version}`,
        `-dSourceDir=${binDir}`,
        '-o',
        wixObj,
        wixScript,
      ]);
      const msiName = `SheepGet_${version}_x64_en-US.msi`;
      const msiPath = join(distDir, msiName);
      run(wixLight, [
        '-nologo',
        '-sice:ICE38',
        '-sice:ICE43',
        '-sice:ICE64',
        '-sice:ICE91',
        '-out',
        msiPath,
        wixObj,
      ]);
      rmSync(wixObj, { force: true });
      rmSync(join(distDir, `SheepGet_${version}_x64_en-US.wixpdb`), { force: true });
      if (existsSync(msiPath)) {
        artifacts.push({ name: msiName, path: msiPath, type: 'MSI Installer' });
      }
    }

    // B. Windows ARM64 Build
    console.log('[Package] Cross-compiling Windows ARM64 binary & portable...');
    const arm64Exe = join(binDir, 'SheepGet-arm64.exe');
    const { sysoTarget: armSyso, tempInfoTarget: armInfo } = generateWindowsSyso('arm64');
    try {
      run(
        'go',
        [
          'build',
          '-tags=production',
          '-trimpath',
          '-ldflags=-w -s -H=windowsgui',
          '-o',
          arm64Exe,
          '.',
        ],
        { env: { ...process.env, GOARCH: 'arm64' } },
      );
    } finally {
      if (existsSync(armSyso)) rmSync(armSyso, { force: true });
      if (existsSync(armInfo)) rmSync(armInfo, { force: true });
    }

    // Windows ARM64 Portable
    const armPortDir = join(distDir, `SheepGet_${version}_windows-arm64-portable`);
    mkdirSync(armPortDir, { recursive: true });
    cpSync(arm64Exe, join(armPortDir, 'SheepGet.exe'));
    cpSync(extTargetDir, join(armPortDir, 'extension'), { recursive: true });
    cpSync(join(root, 'README.md'), join(armPortDir, 'README.md'));
    writeFileSync(join(armPortDir, 'portable'), '', 'utf8');
    mkdirSync(join(armPortDir, 'data'), { recursive: true });
    const armZipName = `SheepGet_${version}_windows-arm64-portable.zip`;
    const armZipPath = join(distDir, armZipName);
    run('tar', [
      '-a',
      '-c',
      '-f',
      armZipPath,
      '-C',
      distDir,
      `SheepGet_${version}_windows-arm64-portable`,
    ]);
    artifacts.push({ name: armZipName, path: armZipPath, type: 'Portable Zip' });

    // Windows ARM64 NSIS
    if (existsSync(nsisExe) && existsSync(nsisScript)) {
      console.log('[Package] Building Windows ARM64 NSIS setup installer (.exe)...');
      run(nsisExe, [
        '-DINFO_PROJECTNAME=SheepGet',
        '-DINFO_PRODUCTNAME=SheepGet',
        '-DINFO_COMPANYNAME=SheepGet',
        `-DINFO_PRODUCTVERSION=${version}`,
        '-DPRODUCT_EXECUTABLE=SheepGet.exe',
        `-DARG_WAILS_ARM64_BINARY=${arm64Exe}`,
        nsisScript,
      ]);
      const nsisArmOut = join(binDir, 'SheepGet-arm64-installer.exe');
      const armSetupName = `SheepGet_${version}_arm64-setup.exe`;
      const armSetupPath = join(distDir, armSetupName);
      if (existsSync(nsisArmOut)) {
        cpSync(nsisArmOut, armSetupPath);
        artifacts.push({ name: armSetupName, path: armSetupPath, type: 'NSIS Setup Installer' });
      }
    }
  }

  // =========================================================================
  // MACOS MATRIX: aarch64 + x64 + universal DMGs
  // =========================================================================
  if (isMac) {
    console.log('[Package] Building macOS binaries for Apple Silicon & Intel...');
    const macArmBinary = join(binDir, 'SheepGet_arm64');
    const macX64Binary = join(binDir, 'SheepGet_x64');
    const macUniBinary = join(binDir, 'SheepGet_universal');

    // 1. Build arm64
    run(
      'go',
      ['build', '-tags=production', '-trimpath', '-ldflags=-w -s', '-o', macArmBinary, '.'],
      {
        env: { ...process.env, CGO_ENABLED: '1', GOARCH: 'arm64' },
      },
    );

    // 2. Build x86_64
    let hasX64 = false;
    try {
      run(
        'go',
        ['build', '-tags=production', '-trimpath', '-ldflags=-w -s', '-o', macX64Binary, '.'],
        {
          env: {
            ...process.env,
            CGO_ENABLED: '1',
            GOARCH: 'amd64',
            CC: 'clang -target x86_64-apple-macos10.13',
            CXX: 'clang++ -target x86_64-apple-macos10.13',
          },
        },
      );
      hasX64 = true;
    } catch (e) {
      console.warn('[Package Warning] Intel x64 compilation skipped:', e.message);
    }

    // 3. Create Universal Binary if both exist
    let hasUniversal = false;
    if (hasX64) {
      try {
        run('lipo', ['-create', '-output', macUniBinary, macArmBinary, macX64Binary]);
        hasUniversal = true;
      } catch (e) {
        console.warn('[Package Warning] lipo create failed:', e.message);
      }
    }

    // Helper: package a DMG from a binary
    function createDmg(binaryPath, dmgFileName) {
      const dmgStage = join(distDir, 'dmg-stage');
      rmSync(dmgStage, { recursive: true, force: true });
      mkdirSync(dmgStage, { recursive: true });

      const appPath = join(dmgStage, 'SheepGet.app');
      assembleMacAppBundle(binaryPath, appPath);

      // Create Applications symlink for drag-and-drop install
      run('ln', ['-s', '/Applications', join(dmgStage, 'Applications')]);

      const dmgOutPath = join(distDir, dmgFileName);
      run('hdiutil', [
        'create',
        '-volname',
        'SheepGet',
        '-srcfolder',
        dmgStage,
        '-ov',
        '-format',
        'UDZO',
        dmgOutPath,
      ]);
      rmSync(dmgStage, { recursive: true, force: true });

      if (existsSync(dmgOutPath)) {
        artifacts.push({ name: dmgFileName, path: dmgOutPath, type: 'macOS DMG' });
      }
    }

    // Generate macOS DMGs
    console.log('[Package] Assembling Apple Silicon DMG (.dmg)...');
    createDmg(macArmBinary, `SheepGet_${version}_aarch64.dmg`);

    if (hasX64) {
      console.log('[Package] Assembling Intel x64 DMG (.dmg)...');
      createDmg(macX64Binary, `SheepGet_${version}_x64.dmg`);
    }
    if (hasUniversal) {
      console.log('[Package] Assembling Universal DMG (.dmg)...');
      createDmg(macUniBinary, `SheepGet_${version}_universal.dmg`);
    }
  }

  // =========================================================================
  // LINUX MATRIX: tar.gz + deb + AppImage
  // =========================================================================
  if (isLinux) {
    console.log('[Package] Building Linux x86_64 binary...');
    const linuxExe = join(binDir, 'SheepGet');
    run('go', ['build', '-tags=production', '-trimpath', '-ldflags=-w -s', '-o', linuxExe, '.']);

    // 1. Linux Portable tar.gz
    console.log('[Package] Assembling Linux portable tar.gz...');
    const linuxPortDir = join(distDir, `SheepGet_${version}_linux-x64-portable`);
    mkdirSync(linuxPortDir, { recursive: true });
    cpSync(linuxExe, join(linuxPortDir, 'SheepGet'));
    cpSync(
      join(root, 'build', 'linux', 'SheepGet.desktop'),
      join(linuxPortDir, 'SheepGet.desktop'),
    );
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(linuxPortDir, 'sheepget.png'));
    cpSync(extTargetDir, join(linuxPortDir, 'extension'), { recursive: true });
    cpSync(join(root, 'README.md'), join(linuxPortDir, 'README.md'));
    writeFileSync(join(linuxPortDir, 'portable'), '', 'utf8');
    mkdirSync(join(linuxPortDir, 'data'), { recursive: true });

    const tarName = `SheepGet_${version}_linux-x64.tar.gz`;
    const tarPath = join(distDir, tarName);
    run('tar', ['-czf', tarPath, '-C', distDir, `SheepGet_${version}_linux-x64-portable`]);
    artifacts.push({ name: tarName, path: tarPath, type: 'Portable tar.gz' });

    // 2. Linux Debian Package (.deb)
    console.log('[Package] Building Linux Debian package (.deb)...');
    const debStage = join(distDir, 'deb-stage');
    rmSync(debStage, { recursive: true, force: true });
    const debBinDir = join(debStage, 'usr', 'bin');
    const debAppDir = join(debStage, 'usr', 'share', 'applications');
    const debIconDir = join(debStage, 'usr', 'share', 'icons', 'hicolor', '256x256', 'apps');
    const debControlDir = join(debStage, 'DEBIAN');
    mkdirSync(debBinDir, { recursive: true });
    mkdirSync(debAppDir, { recursive: true });
    mkdirSync(debIconDir, { recursive: true });
    mkdirSync(debControlDir, { recursive: true });

    cpSync(linuxExe, join(debBinDir, 'SheepGet'));
    chmodSync(join(debBinDir, 'SheepGet'), 0o755);
    cpSync(join(root, 'build', 'linux', 'SheepGet.desktop'), join(debAppDir, 'SheepGet.desktop'));
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(debIconDir, 'sheepget.png'));

    const controlContent = [
      'Package: sheepget',
      `Version: ${version}`,
      'Section: utils',
      'Priority: optional',
      'Architecture: amd64',
      `Maintainer: ${companyName}`,
      `Description: ${productDescription}`,
      '',
    ].join('\n');
    writeFileSync(join(debControlDir, 'control'), controlContent, 'utf-8');

    const debName = `SheepGet_${version}_amd64.deb`;
    const debPath = join(distDir, debName);
    run('dpkg-deb', ['--build', debStage, debPath]);
    rmSync(debStage, { recursive: true, force: true });
    if (existsSync(debPath)) {
      artifacts.push({ name: debName, path: debPath, type: 'Debian Package' });
    }

    // 3. Linux AppImage
    console.log('[Package] Building Linux AppImage (.AppImage)...');
    const appDir = join(distDir, 'AppDir');
    rmSync(appDir, { recursive: true, force: true });
    const appUsrBin = join(appDir, 'usr', 'bin');
    mkdirSync(appUsrBin, { recursive: true });
    cpSync(linuxExe, join(appUsrBin, 'SheepGet'));
    chmodSync(join(appUsrBin, 'SheepGet'), 0o755);
    cpSync(join(root, 'build', 'linux', 'SheepGet.desktop'), join(appDir, 'SheepGet.desktop'));
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(appDir, 'SheepGet.png'));
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(appDir, 'sheepget.png'));
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(appDir, '.DirIcon'));
    const appIconShare = join(appDir, 'usr', 'share', 'icons', 'hicolor', '256x256', 'apps');
    mkdirSync(appIconShare, { recursive: true });
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(appIconShare, 'SheepGet.png'));
    cpSync(join(root, 'build', 'icons', 'icon.png'), join(appIconShare, 'sheepget.png'));
    // AppRun launcher
    const appRunScript = [
      '#!/bin/sh',
      'HERE="$(dirname "$(readlink -f "${0}")")"',
      'exec "${HERE}/usr/bin/SheepGet" "$@"',
      '',
    ].join('\n');
    writeFileSync(join(appDir, 'AppRun'), appRunScript, 'utf-8');
    chmodSync(join(appDir, 'AppRun'), 0o755);

    const appImageName = `SheepGet_${version}_amd64.AppImage`;
    const appImagePath = join(distDir, appImageName);

    // Check appimagetool in tools or path
    const localAppImageTool = join(toolsDir, 'appimagetool');
    const appImageToolCmd = existsSync(localAppImageTool) ? localAppImageTool : 'appimagetool';
    try {
      run(appImageToolCmd, [appDir, appImagePath], {
        env: { ...process.env, ARCH: 'x86_64', APPIMAGE_EXTRACT_AND_RUN: '1' },
      });
      if (existsSync(appImagePath)) {
        artifacts.push({ name: appImageName, path: appImagePath, type: 'AppImage' });
      }
    } catch (e) {
      console.warn('[Package Warning] appimagetool execution failed:', e.message);
    }
    rmSync(appDir, { recursive: true, force: true });
  }

  // =========================================================================
  // INTEGRITY CHECKSUM: SHA256SUMS.txt
  // =========================================================================
  const shaLines = [];
  for (const art of artifacts) {
    const data = readFileSync(art.path);
    const hash = createHash('sha256').update(data).digest('hex');
    shaLines.push(`${hash}  ${art.name}`);
  }
  const shaFile = join(distDir, 'SHA256SUMS.txt');
  writeFileSync(shaFile, shaLines.join('\n') + '\n', 'utf8');

  console.log('\n=====================================================================');
  console.log('            SheepGet Multi-Platform Artifacts Summary                ');
  console.log('=====================================================================');
  console.log(`Output Directory:  ${distDir}`);
  for (const art of artifacts) {
    const size = formatSize(statSync(art.path).size);
    console.log(`- ${art.name.padEnd(46)} [${art.type.padEnd(22)}] (${size})`);
  }
  console.log(`- SHA256SUMS.txt                                 [Integrity Checksum   ]`);
  console.log('External FFmpeg:   0 B (Pure Native Go Media Processing)');
  console.log('=====================================================================\n');

  // Keep binDir intact for CI verification and native-build archiver
}

main().catch((err) => {
  console.error('[Package Error]:', err);
  process.exitCode = 1;
});
