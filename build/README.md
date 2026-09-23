# Build Directory

The build directory is used to house all the build files and assets for your application.

The structure is:

- bin - Output directory
- darwin - macOS specific files
- windows - Windows specific files

## Mac

The `darwin` directory holds files specific to Mac builds.
These may be customised and used as part of the build. To package distribution artifacts,
use `bun run package`.

The directory contains the following files:

- `Info.plist` - the main plist file used for Mac builds. It is used when packaging for macOS.
- `Info.dev.plist` - plist file used when developing locally via `bun run dev`.

## Windows

The `windows` directory contains the manifest and rc files used for Windows builds.
These may be customised for your application. To package distribution artifacts,
use `bun run package`.

- `icon.ico` - The icon used for the application. This is used when packaging for Windows. If you wish to
  use a different icon, simply replace this file with your own. If it is missing, a new `icon.ico` file
  will be created using the `appicon.png` file in the build directory.
- `installer/*` - The files used to create the Windows installer (NSIS / WiX).
- `info.json` - Application details used for Windows builds. The data here will be used by the Windows installer,
  as well as the application itself (right click the exe -> properties -> details)
- `wails.exe.manifest` - The main application manifest file.
