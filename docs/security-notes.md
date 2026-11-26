# Security notes

For operators and reviewers: what this extension runs, where it connects, what
it reads and writes, and why. The line numbers move; search for the names.

## Network: only the camera

`module.yaml` declares one egress entry: the host in the setting `camera_url`,
from the process, on ports 554, 8554, 322, 80, 8080 and 443.

- **Why:** while a print runs, the extension needs frames from the camera that
  films the printer. RTSP (`rtsp://`, `rtsps://`) is the live stream of cameras
  such as the Prusa Buddy3D, HTTP(S) is a snapshot address that answers one JPEG.
- **What is sent:** the RTSP or HTTP request for the stream or image, with the
  camera username and password when the operator set them. No print data,
  nothing from Prusa Connect.
- **Enforced, not only declared** (`camera.go`, `checkCameraURL`): the settings
  are refused unless `camera_url` has one of the schemes `rtsp`, `rtsps`, `http`,
  `https`, a host, and a port from exactly the declared list. A test
  (`TestTheDeclaredCameraPortsAreTheEnforcedOnes`) keeps `module.yaml` and the
  code in step.
- **Redirects:** the HTTP client for snapshots (`cameraClient`) follows a
  redirect only to the same scheme, host and port, at most twice. A camera
  cannot send the process, or the credentials, elsewhere.
- **RTSP over TCP, RTSP only:** ffmpeg is started with `-rtsp_transport tcp`,
  so it opens one TCP connection to the address in `camera_url` and no UDP
  ports, and with `-protocol_whitelist rtsp,rtp,tcp,udp,tls`
  (`ffmpegSource.inputArgs`), so nothing the camera answers can make ffmpeg
  open a file, an HTTP address or any other protocol.
- **RTSP redirects (known limit):** ffmpeg's RTSP client follows a 3xx answer
  of the camera to the address in its `Location` header, and ffmpeg has no
  option to turn that off. The whitelist keeps such a redirect on RTSP, but
  not on the host or port in `camera_url`, and the camera credentials may go
  along. Only the camera itself can send such an answer, so this
  is a camera the operator already trusts with the stream; point
  `camera_url` at the camera directly and use a camera account that can only
  view the stream (see the known limit below).

There is no other connection: no update check, no telemetry, no third-party
service. Everything else goes through the module protocol to the host
(`observe`, `write_storage`, `core-device.*`, `core-access.check`).

## The ffmpeg child process

ffmpeg is the one external program. Prusa Connect Local does not include it;
the operator installs it (see README, "ffmpeg on the server").

| Where | What it does | Input | Output |
|---|---|---|---|
| `frames.go`, `ffmpegSource.run` | decodes the RTSP stream into JPEG frames | `camera_url` | JPEGs on stdout, read by the process |
| `session.go`, `render` | builds the MP4 from the chosen frames | JPEGs in the module's data folder | `zeitraffer.mp4` in the same folder |
| `ffmpegcheck.go`, `checkFFmpeg` | `ffmpeg -hide_banner -version`, to tell the operator when ffmpeg is missing | none | first line of the version |

Why this is not a command injection path:

- **No shell.** Every call is `exec.CommandContext(path, args...)`. Nothing is
  passed through `sh -c` or similar.
- **Fixed arguments.** The argument lists are written in the code. The only
  values from settings are numbers that are parsed and range-checked first
  (`capture_fps`, `video_fps`, `hold_last_s`, the profile's width and threads)
  and `camera_url`, which has passed `checkCameraURL` and therefore starts
  with `rtsp://` or `rtsps://` (never with `-`). File paths are built by the
  process inside its own data folder.
- **The program path** comes from the setting `ffmpeg` (default `ffmpeg`,
  looked up in `PATH`). Only an administrator of Prusa Connect Local can change
  settings, and the same administrator decides which native extensions run.
  The setting gives no capability that administrator does not already have.
- **Bounded:** every call runs under a context that ends with the print or the
  extension; the stream reader has a `WaitDelay`, the version check a 10 s
  timeout. Rendering uses at most two threads (one in the `low` profile) and,
  in that profile, a lower process priority.
- **Logs without secrets:** ffmpeg's stderr goes to the host log with the
  camera password replaced by `xxxxx` (`prefixed`, `secretsOf`).

Known limit: ffmpeg accepts RTSP credentials only inside the address, so the
camera password is part of ffmpeg's command line while the stream is open.
Other processes of the same user on the same host (in Docker: in the same
container) can read it there. Use a camera account that can only view the
stream.

## Data the extension writes

- **Its data folder** (`needs.data`): the event cursor and, per print, the
  frames of the current layer, the chosen frames and, when the operator turns
  on `diagnose_layers`, candidate frames of a few layers. A print's folder is
  deleted once its files are in the storage; it stays only when storing
  failed.
- **The storage in `storage`, folder `folder`** (`write_storage`): per print a
  folder with `zeitraffer.mp4`, `vorschau.jpg`, `info.json` and, when turned
  on, `bilder/` and `diagnose/`. These are camera pictures of the print room;
  the README asks operators to check them before sharing.

## HTTP routes

Two read-only routes under the module's prefix, served through the host and
behind its authentication: `GET /status` (recording state, JSON) and
`GET /preview` (the last chosen frame, a re-encoded JPEG with
`X-Content-Type-Options: nosniff`). Every other path answers 404.

The host lets in everyone who may open the extension. Both routes then ask
the server (`access.check`, `access.go`) whether the signed-in person may
`observe` the printer the recorder follows. Someone who may not gets no
picture (204) and a status without anything of that printer: no name, no
print, no folder. The check fails closed: no person, no answer from the
server or a refusal all count as "may not". `observe` is in `permissions`, so
the module may ask about it without a `checks` entry; it asks about nothing
else.

## Reporting

Please report vulnerabilities privately, see [SECURITY.md](../SECURITY.md).
