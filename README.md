# pcl-timelapse – timelapse for Prusa Connect Local

*Deutsche Fassung: [README.de.md](README.de.md)*

An extension for [Prusa Connect Local](https://github.com/prusa3d/prusa-connect), the self-hosted Prusa Connect server. It records every print as a timelapse with one frame per layer and needs **no extra G-code**.

- The layer comes from the Z height the printer reports while printing anyway.
- The frame comes from the camera's live stream (RTSP) or from a snapshot URL (HTTP). For every layer the extension keeps the frame in which the print head is least in the way.
- When the print ends, an H.264 MP4 is stored in Prusa Connect Local's storage. The printer page gets a "Zeitraffer" tab to watch, download and delete the videos.

Status: early version (0.1.x), built and tested against Prusa Connect Local **v0.1.0-beta.1** with a Prusa CORE One and a Buddy3D camera. The user interface is German.

## How it works

1. **Print starts:** the extension opens the camera stream. For `rtsp://` ffmpeg decodes it at `capture_fps` frames per second, for `http://` a snapshot URL is polled.
2. **Layer change:** while printing, the printer reports its Z height about every 5 s. When Z rises and stays up for `layer_settle`, a new layer begins. Short Z hops are filtered out this way.
3. **Frame choice:** every frame of a layer is compared with a reference picture without the print head: the per-pixel median of the layer's frames and the frames chosen last. The most similar frame wins. Brightness changes of the camera are evened out first.
4. **Print ends:** ffmpeg builds an MP4 from the chosen frames (two threads, at most 1920 pixels wide) and stores it:

   ```
   <storage>/<folder>/<printer>/<YYYY-MM-DD_HH-MM>_<model>/
       zeitraffer.mp4   vorschau.jpg   info.json   [bilder/]   [diagnose/]
   ```

   The model name is the file name without the slicer settings from the nozzle on (`_0.4n_0.2mm_PETG_…`), at most 40 characters. If the start of a print does not carry the file name, the extension looks it up in the server's job record.

Frames of the current layer are kept on disk in the module's data folder, not in memory. If the module restarts during a print, it continues the same recording. If storing fails, the chosen frames are kept and the video is rebuilt the next time the extension starts.

## Requirements

- Prusa Connect Local v0.1.0-beta.1 or newer.
- `ffmpeg` on the server, with libx264.
- A camera with an RTSP stream or a URL that returns a single JPEG.
- The target storage has to accept `.mp4` and `.json`. Out of the box the filesystem storage only accepts print files, models, firmware and images. Prusa Connect Local's `config.yaml` therefore needs its own list that includes the default list:

  ```yaml
  resources:
    storage:
      local:
        type: filesystem
        config:
          root: ./storage
          allowed_extensions: [".gcode", ".bgcode", ".gco", ".g", ".stl", ".3mf", ".obj", ".bbf",
                               ".png", ".jpg", ".jpeg", ".webp", ".gif", ".mp4", ".json"]
  ```

  Without it, the extension only reports "that storage cannot be read right now" when storing. The actual reason (`EXTENSION_NOT_ALLOWED`) is in the server log.

## Installation

Build a release (see below) and install it:

```sh
prusa-connect module add github.com/gilbert-1705/pcl-timelapse@0.1.6 -from release/timelapse -approve
```

Or add the release folder to `core.modules_local` in `config.yaml`:

```yaml
core:
  modules_local:
    github.com/gilbert-1705/pcl-timelapse: /opt/modules/timelapse
```

Then enable and configure it under **Settings → Extensions**. After an update, restart the server once, because it loads the browser part only at start.

Permissions: `observe` (read printer state and events), `write_storage` (store the videos), the job record of a printer (`core-device.jobs`, for the file name) and a data folder. Network access only to the camera.

## Settings

| Option | Default | Meaning |
|---|---|---|
| `camera_url` | empty | `rtsp://…` for the stream, `http://…` for a snapshot. Empty records nothing. Example: `rtsp://192.0.2.50/live` |
| `camera_username`, `camera_password` | empty | Camera credentials, if needed. The password is masked in logs. |
| `printer` | empty | Name or ID of the printer the camera films. Empty takes the only printer, or the first one that starts printing. |
| `storage` / `folder` | `local` / `zeitraffer` | Where the timelapses are written. |
| `capture_fps` | 2 | Frames per second taken from the stream to choose from per layer. |
| `video_fps` | 30 | Frames per second in the video. 300 layers make 10 seconds. |
| `hold_last_s` | 1.5 | How long the last frame stays at the end. |
| `layer_settle` | 12s | How long a new Z height has to hold. Must be longer than the printer's telemetry interval. |
| `min_layers` | 10 | Prints with fewer layers get no video. |
| `max_frames_per_layer` | 240 | Upper limit of candidates per layer. Long layers are thinned out. |
| `keep_frames` | off | Also store the chosen frames under `bilder/`. |
| `timezone` | Europe/Berlin | Time zone for date and time in the folder name. |
| `diagnose_layers` / `diagnose_every` | 0 / 20 | Diagnosis of the frame choice, see below. |
| `ffmpeg` | `ffmpeg` | Path to ffmpeg. |

## Diagnosis of the frame choice

With `diagnose_layers` above 0 the extension keeps all candidate frames of some layers, every `diagnose_every`-th layer. Each of these layers gets `diagnose/schicht-NNNN/` with:

- `kandidat-NNN.jpg`, 960 pixels wide
- `auswahl.json` with time and distance of every frame and the chosen one
- `referenz.png`, the reference picture

That is about 5 to 10 MB per layer. The frames show your printer enclosure. Before sharing them, check that they show nothing you do not want to share.

## Limits

- The first layers have no reference frames yet, so the print head may stay in the picture there.
- If the head stays over the object for most of a layer, it can end up in the reference and stay visible in the chosen frame. Without G-code this cannot be avoided completely. The diagnosis helps to look into it.
- Very short layers (shorter than `layer_settle`) merge. The video gets shorter, not wrong.

## Building

```sh
make core      # fetch Prusa Connect Local at the tested tag into .core, build its libraries
make test      # Go tests, including an end-to-end test over the module protocol (needs ffmpeg)
make check     # type check of the browser part
make release   # release/timelapse/ and release/timelapse-<version>.tar.gz
```

Needs Go 1.26, Node 22, pnpm 11 and ffmpeg. `PLATFORMS=linux_amd64 make release` builds a single platform.

## Security

Please do not report vulnerabilities in public issues. See [SECURITY.md](SECURITY.md).

## License

MIT, see [LICENSE](LICENSE). Built on Prusa Connect Local by Prusa Research a.s.
