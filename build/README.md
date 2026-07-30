# Desktop build assets

`appicon.svg` is the editable source for the application icon. Wails consumes
the 1024 x 1024 `appicon.png` file and generates the platform-specific icon
container during a native package build.

- `darwin/`: production and development bundle metadata.
- `windows/`: executable metadata and application manifest. `icon.ico` is
  generated from `appicon.png` by `wails build` when it is absent.
- `linux/`: desktop-entry metadata for distribution packages. Wails itself
  emits a Linux binary; the release workflow packages it as DEB and AppImage.

The root `VERSION` file is the version source of truth. Use
`node scripts/version.mjs set MAJOR.MINOR.PATCH` to update all tracked version
fields, and use `scripts/wails-build.mjs` so release metadata is injected into
the native binary consistently.
