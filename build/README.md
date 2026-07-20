# Desktop build assets

`appicon.svg` is the editable source for the application icon. Wails consumes
the 1024 x 1024 `appicon.png` file and generates the platform-specific icon
container during a native package build.

- `darwin/`: production and development bundle metadata.
- `windows/`: executable metadata and application manifest. `icon.ico` is
  generated from `appicon.png` by `wails build` when it is absent.
- `linux/`: desktop-entry metadata for distribution packages. Wails itself
  emits a Linux binary but does not create a `.deb`, `.rpm`, or AppImage.

Keep the product version in `wails.json` aligned with release tags and the
version injected into `main.version` by the release build command.
