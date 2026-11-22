# Optional: park the head for a clean frame (G-code)

*Deutsche Fassung: [gcode-park.de.md](gcode-park.de.md)*

By default pcl-timelapse needs **no G-code**: for every layer it picks the frame in which the print head is least in the way. That works without touching your slicer, but the head can still show up in some layers, especially when it spends most of a layer over the object.

If you want every frame clean, let the slicer park the head out of the camera's view for a few seconds right after each layer change, and turn on `gcode_park` in the extension settings. The extension then keeps the frame in which the head stands still in that spot.

**Cost:** about 4 to 5 seconds per layer (lift, travel, wait, travel back). A print with 200 layers takes about 15 minutes longer.

## 1. Add the G-code in PrusaSlicer

*Printer Settings → Custom G-code → After layer change G-code*. Keep what is already there and add this below it:

```gcode
; --- pcl-timelapse: park the head for one clean frame ---
{if layer_num > 0}
G1 E-0.8 F2100                                   ; retract
G1 Z{layer_z + 0.6} F720                         ; lift above the print
G1 X{print_bed_max[0] / 2} Y{print_bed_max[1] - 5} F12000 ; park at the back, centred
G4 S3                                            ; stand still for 3 s
G1 Z{layer_z} F720                               ; back to the layer height
G1 E0.8 F2100                                    ; unretract
{endif}
; --- end pcl-timelapse ---
```

Save it as a separate printer profile (for example "… – timelapse") so that normal prints stay as fast as before.

What to check before you use it:

- **Where the head is out of view.** The snippet parks at the back of the bed, which suits a camera at the front. Move the park position if your camera sees the back of the bed.
- **Your printer's limits.** The park position has to be reachable and must not hit a purge area, wiper or clip. Watch the first layers of a test print.
- **Relative extrusion.** The snippet assumes relative E (`M83`), which is the default of the Prusa profiles. Match the retraction to your filament profile.
- **The wait.** 3 seconds are enough at `capture_fps` 2. With `performance: low` the extension only reads keyframes, so use at least the camera's keyframe interval plus one second (usually 3 to 4 seconds).

## 2. Turn it on in the extension

*Settings → Extensions → pcl-timelapse*:

| Option | Value |
|---|---|
| `gcode_park` | on |
| `park_window` | `20s` (default). The park has to happen within this time after the layer change. |

Turn `gcode_park` off again for prints without the snippet. Without a park the extension would pick the stillest frame at the start of each layer, which is not necessarily a clean one.

## How the frame is chosen

The extension already notices a layer change from the Z height the printer reports. With `gcode_park` it looks at the frames from the first `park_window` of every layer and keeps the frame that differs least from the frames before and after it. While the head waits at the park position, the picture does not change, so that frame wins. Frames from that window are not thinned out on long layers.
