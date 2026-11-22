# Changelog

What an operator sees change when upgrading this extension, newest release
first. `version:` in `module.yaml` is the newest heading below. The format
follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), versions
follow [Semantic Versioning](https://semver.org/).

## 0.2.2 - 2026-11-22

First release for the Prusa Plugin Marketplace.

### Added

- The "Zeitraffer" tab shows a notice when ffmpeg is missing or does not run,
  with the reason, before the first print. The check runs again before every
  print, so installing ffmpeg later needs no restart. A print without a usable
  ffmpeg is not recorded, and the server log says why.
- `require.core: ">=0.1.0-beta.1"`: the extension declares the Prusa Connect
  Local versions it runs on.

### Changed

- `camera_url` only accepts the ports the extension declares in `egress`:
  554, 8554 and 322 for RTSP/RTSPS, 80, 8080 and 443 for HTTP/HTTPS, and only
  the schemes `rtsp`, `rtsps`, `http` and `https`. Another port or scheme is
  refused when the settings are saved.
- A snapshot address that redirects to another scheme, host or port is no
  longer followed.
- Title and summary in the extension list are English ("Timelapse"). The tab
  and the setting descriptions stay German.

## 0.2.1 - 2026-11-22

### Fixed

- A timelapse could be stored under the file name of the previous print. The
  name now always comes from the printer's job record.

## 0.2.0 - 2026-11-22

### Added

- Setting `performance: low` for small computers such as a Raspberry Pi:
  decodes only keyframes, at most one frame a second at 1280 pixels, and
  builds the video with one thread at a lower priority.
- Settings `gcode_park` and `park_window` (off by default): with a park
  G-code snippet in the slicer, the frame in which the head stands still is
  used. See `docs/gcode-park.md`.

## 0.1.6 - 2026-11-22

### Fixed

- A print whose start carried no file name was stored as "druck". The name
  is now looked up in the printer's job record (permission
  `core-device.jobs`).
