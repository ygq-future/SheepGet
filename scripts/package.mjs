import {
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

const root = resolve(fileURLToPath(new URL('..', import.meta.url)));
process.chdir(root);

const platform = process.platform;
const isWin = platform === 'win32';
const isMac = platform === 'darwin';
const isLinux = !isWin && !isMac;

const platformName = isWin ? 'windows' : isMac ? 'darwin' : 'linux';
const arch = process.arch;
const archLabel = arch === 'arm64' ? 'arm64' : 'x64';
// Read version and metadata from build/config.yml under the `info:` block as Single Source of Truth (SSOT)
const buildConfigRaw = readFileSync(join(root, 'build', 'config.yml'), 'utf-8');
const infoBlockMatch = buildConfigRaw.match(/info:\s*([\s\S]*?)(?:\n\w+:|$)/);
const infoBlock = infoBlockMatch ? infoBlockMatch[1] : buildConfigRaw;
const versionMatch = infoBlock.match(/^\s*version:\s*['"]?([^'"\s]+)['"]?/m);
const version = versionMatch ? versionMatch[1] : '1.0.0';
const productNameMatch = infoBlock.match(/^\s*productName:\s*['"]?([^'"\s]+)['"]?/m);
const productName = productNameMatch ? productNameMatch[1] : 'SheepGet';
const companyNameMatch = infoBlock.match(/^\s*companyName:\s*['"]?([^'"\s]+)['"]?/m);
const companyName = companyNameMatch ? companyNameMatch[1] : 'SheepGet';
const descMatch = infoBlock.match(/^\s*description:\s*['"]?([^'"\r\n]+)['"]?/m);
const productDescription = descMatch ? descMatch[1] : 'Modern Desktop Download Manager';

const ext = isWin ? '.exe' : '';
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

async function main() {
  console.log(`[Package] Packaging SheepGet v${version} for ${platformName}-${archLabel}...`);

  if (isWin) {
    await ensureWindowsTools();
  }

  // 1. Build main desktop application
  const mainExe = join(binDir, 'SheepGet' + ext);
  console.log('[Package] Building main desktop application...');
  const sysoTarget = isWin
    ? join(root, `resource_windows_${arch === 'arm64' ? 'arm64' : 'amd64'}.syso`)
    : null;
  const tempInfoTarget = isWin
    ? join(toolsDir, `info_windows_${arch === 'arm64' ? 'arm64' : 'amd64'}.json`)
    : null;
  if (isWin) {
    const infoPayload = {
      fixed: {
        file_version: version,
      },
      info: {
        '0000': {
          ProductVersion: version,
          CompanyName: companyName,
          FileDescription: productName,
          LegalCopyright: `Copyright © ${new Date().getFullYear()} ${companyName}`,
          ProductName: productName,
          Comments: productDescription,
        },
      },
    };
    writeFileSync(tempInfoTarget, JSON.stringify(infoPayload, null, 2), 'utf-8');
    run(resolveWails3(), [
      'generate',
      'syso',
      '-arch',
      arch === 'arm64' ? 'arm64' : 'amd64',
      '-icon',
      'build/windows/icon.ico',
      '-info',
      tempInfoTarget,
      '-manifest',
      'build/windows/wails.exe.manifest',
      '-out',
      sysoTarget,
    ]);
  }
  const mainLdflags = isWin ? '-w -s -H=windowsgui' : '-w -s';
  try {
    run('go', [
      'build',
      '-tags=production',
      '-trimpath',
      `-ldflags=${mainLdflags}`,
      '-o',
      mainExe,
      '.',
    ]);
  } finally {
    if (sysoTarget && existsSync(sysoTarget)) {
      rmSync(sysoTarget, { force: true });
    }
    if (tempInfoTarget && existsSync(tempInfoTarget)) {
      rmSync(tempInfoTarget, { force: true });
    }
  }
  // 2. Build browser extension
  console.log('[Package] Building browser extension...');
  run('bun', ['run', 'build'], { cwd: join(root, 'extension'), quiet: true });

  const extSourceDir = join(root, 'build', 'dist-extension', 'chrome-mv3');
  const extTargetDir = join(distDir, 'extension');
  rmSync(extTargetDir, { recursive: true, force: true });
  cpSync(extSourceDir, extTargetDir, { recursive: true });
  cpSync(extTargetDir, join(binDir, 'extension'), { recursive: true });

  const artifacts = [];

  // 3. Build Portable Distribution
  console.log('[Package] Assembling portable distribution bundle...');

  const portableDirName = isWin
    ? `SheepGet_${version}_windows-${archLabel}-portable`
    : `SheepGet_${version}_${platformName}-${archLabel}-portable`;
  const portableStage = join(distDir, portableDirName);
  rmSync(portableStage, { recursive: true, force: true });
  mkdirSync(portableStage, { recursive: true });

  cpSync(mainExe, join(portableStage, 'SheepGet' + ext));
  cpSync(extTargetDir, join(portableStage, 'extension'), { recursive: true });
  cpSync(join(root, 'README.md'), join(portableStage, 'README.md'));

  writeFileSync(join(portableStage, 'portable'), '', 'utf8');
  mkdirSync(join(portableStage, 'data'), { recursive: true });

  if (isWin) {
    // Create zip for Windows portable
    const portableZipName = `SheepGet_${version}_windows-${archLabel}-portable.zip`;
    const portableZipPath = join(distDir, portableZipName);
    run('tar', ['-a', '-c', '-f', portableZipPath, '-C', distDir, portableDirName]);
    artifacts.push({ name: portableZipName, path: portableZipPath, type: 'Portable Zip' });
  } else {
    if (isLinux) {
      cpSync(
        join(root, 'build', 'linux', 'SheepGet.desktop'),
        join(portableStage, 'SheepGet.desktop'),
      );
    }
    const portableTarName = `SheepGet_${version}_${platformName}-${archLabel}.tar.gz`;
    const portableTarPath = join(distDir, portableTarName);
    run('tar', ['-czf', portableTarPath, '-C', distDir, portableDirName]);
    artifacts.push({ name: portableTarName, path: portableTarPath, type: 'Portable tar.gz' });
  }
  // 4. Windows: Build NSIS Setup Installer (.exe) and WiX Installer (.msi)
  if (isWin) {
    // NSIS Setup Installer
    const nsisExe = join(toolsDir, 'nsis', 'makensis.exe');
    const nsisScript = join(root, 'build', 'windows', 'installer', 'project.nsi');
    if (existsSync(nsisExe) && existsSync(nsisScript)) {
      console.log('[Package] Building Windows NSIS setup installer (.exe)...');
      run(nsisExe, [
        '-DINFO_PROJECTNAME=SheepGet',
        '-DINFO_PRODUCTNAME=SheepGet',
        '-DINFO_COMPANYNAME=SheepGet',
        `-DINFO_PRODUCTVERSION=${version}`,
        '-DPRODUCT_EXECUTABLE=SheepGet.exe',
        `-DARG_WAILS_AMD64_BINARY=${mainExe}`,
        nsisScript,
      ]);

      const nsisOut = join(binDir, 'SheepGet-amd64-installer.exe');
      const setupExeName = `SheepGet_${version}_${archLabel}-setup.exe`;
      const setupExePath = join(distDir, setupExeName);
      if (existsSync(nsisOut)) {
        cpSync(nsisOut, setupExePath);
        artifacts.push({ name: setupExeName, path: setupExePath, type: 'NSIS Setup Installer' });
      }
    }

    // WiX MSI Installer
    const wixCandle = join(toolsDir, 'wix', 'candle.exe');
    const wixLight = join(toolsDir, 'wix', 'light.exe');
    const wixScript = join(root, 'build', 'windows', 'installer', 'project.wxs');
    if (existsSync(wixCandle) && existsSync(wixLight) && existsSync(wixScript)) {
      console.log('[Package] Building Windows MSI installer (.msi)...');
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

      const msiName = `SheepGet_${version}_${archLabel}_en-US.msi`;
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
      rmSync(join(distDir, `SheepGet_${version}_${archLabel}_en-US.wixpdb`), { force: true });

      if (existsSync(msiPath)) {
        artifacts.push({ name: msiName, path: msiPath, type: 'MSI Installer' });
      }
    }
  }

  // 7. Generate SHA256SUMS.txt
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
  // Clean intermediate build scratch directory
  rmSync(binDir, { recursive: true, force: true });
}

main().catch((err) => {
  console.error('[Package Error]:', err);
  process.exitCode = 1;
});
