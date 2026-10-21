# Build Directory

The build directory is used to house all the build files and assets for your application. 

The structure is:

* bin - Output directory
* darwin - macOS specific files
* windows - Windows specific files

## Mac

The `darwin` directory holds files specific to Mac builds.
These may be customised and used as part of the build. To return these files to the default state, simply delete them
and
build with `wails build`.

The directory contains the following files:

- `Info.plist` - the main plist file used for Mac builds. It is used when building using `wails build`.
- `Info.dev.plist` - same as the main plist file but used when building using `wails dev`.

The macOS-only Wails post-build hook (`scripts/macos-icon.swift`) renders the
shared artwork onto an 824px tile, centered in a transparent 1024px canvas. It
uses sRGB, includes a subtle shadow, supplies all standard/Retina ICNS sizes,
and restores the ad-hoc signature before optional release signing. Windows
and Linux retain their existing icon sizing.

Apple’s current [app icon guidance](https://developer.apple.com/design/human-interface-guidelines/app-icons)
uses full-bleed layers with Icon Composer for Liquid Glass. Those layers are
not interchangeable with a flattened ICNS icon. This Wails build retains a
legacy ICNS for macOS 12+; a layered migration requires modern Xcode/asset
catalog compilation and testing on macOS 26+. The latest
[Icon Composer](https://developer.apple.com/icon-composer/) requires macOS 26.4+.

## Windows

The `windows` directory contains the manifest and rc files used when building with `wails build`.
These may be customised for your application. To return these files to the default state, simply delete them and
build with `wails build`.

- `icon.ico` - The icon used for the application. This is used when building using `wails build`. If you wish to
  use a different icon, simply replace this file with your own. If it is missing, a new `icon.ico` file
  will be created using the `appicon.png` file in the build directory.
- `installer/*` - The files used to create the Windows installer. These are used when building using `wails build`.
- `info.json` - Application details used for Windows builds. The data here will be used by the Windows installer,
  as well as the application itself (right click the exe -> properties -> details)
- `wails.exe.manifest` - The main application manifest file.

---

## Local builds

Run the platform-specific helper script:

- **macOS:** `bash scripts/build-macos.sh`
- **Linux:** `bash scripts/build-linux.sh`
- **Windows:** `pwsh scripts/build-windows.ps1`

Build helpers default to the version in the root `VERSION` file. Set
`VERSION=v1.8.1` to override it. Keep `wails.json` and the frontend package
version aligned when preparing a release; ordinary `wails dev` still uses `dev`.

Signing requires the env vars documented in `.github/SIGNING.md`. Without
them the script produces an unsigned binary (clearly logged).

## CI

- `test.yml` — runs on every PR (Go + frontend tests, build).
- `release.yml` — runs on `v*.*.*` tag push; produces the matrix of
  signed/notarized artifacts and drafts a GitHub release.
