# Optional: Druckkopf für ein sauberes Bild parken (G-Code)

*Deutsche Übersetzung. Maßgeblich ist die englische Fassung [gcode-park.md](gcode-park.md).*

Ab Werk braucht pcl-timelapse **keinen G-Code**: Pro Schicht nimmt die Erweiterung das Bild, auf dem der Druckkopf am wenigsten im Weg ist. Das funktioniert, ohne den Slicer anzufassen. In manchen Schichten kann der Kopf aber trotzdem zu sehen sein, vor allem wenn er fast die ganze Schicht über dem Objekt steht.

Wenn jedes Bild frei sein soll, lässt du den Slicer den Kopf nach jedem Schichtwechsel für ein paar Sekunden aus dem Bild parken und schaltest in der Erweiterung `gcode_park` ein. Die Erweiterung behält dann das Bild, auf dem der Kopf dort still steht.

**Kosten:** etwa 4 bis 5 Sekunden pro Schicht (anheben, hinfahren, warten, zurück). Ein Druck mit 200 Schichten dauert rund 15 Minuten länger.

## 1. G-Code in PrusaSlicer eintragen

*Druckereinstellungen → Benutzerdefinierter G-Code → G-Code nach Schichtwechsel*. Den vorhandenen Inhalt stehen lassen und darunter einfügen:

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

Speichere das als eigenes Druckerprofil (etwa „… – Zeitraffer“), damit normale Drucke so schnell bleiben wie bisher.

Vorher prüfen:

- **Wo der Kopf aus dem Bild ist.** Der Schnipsel parkt hinten auf dem Bett, das passt zu einer Kamera vorne. Sieht deine Kamera die Rückseite, verschieb die Parkposition.
- **Grenzen deines Druckers.** Die Parkposition muss erreichbar sein und darf keinen Spülbereich, Abstreifer oder Klemme treffen. Beobachte die ersten Schichten eines Testdrucks.
- **Relative Extrusion.** Der Schnipsel geht von relativem E aus (`M83`), das ist Standard in den Prusa-Profilen. Den Rückzug an dein Filamentprofil anpassen.
- **Die Wartezeit.** 3 Sekunden reichen bei `capture_fps` 2. Mit `performance: low` liest die Erweiterung nur Keyframes, dann mindestens den Keyframe-Abstand der Kamera plus eine Sekunde nehmen (meist 3 bis 4 Sekunden).

## 2. In der Erweiterung einschalten

*Einstellungen → Erweiterungen → pcl-timelapse*:

| Option | Wert |
|---|---|
| `gcode_park` | an |
| `park_window` | `20s` (Standard). Das Parken muss innerhalb dieser Zeit nach dem Schichtwechsel passieren. |

Für Drucke ohne den Schnipsel `gcode_park` wieder ausschalten. Ohne Parken nähme die Erweiterung sonst das ruhigste Bild am Anfang jeder Schicht, und das ist nicht unbedingt ein freies.

## Wie das Bild gewählt wird

Den Schichtwechsel erkennt die Erweiterung schon an der Z-Höhe, die der Drucker meldet. Mit `gcode_park` schaut sie sich die Bilder aus den ersten `park_window` jeder Schicht an und behält das Bild, das sich am wenigsten von seinen Nachbarn unterscheidet. Solange der Kopf an der Parkposition wartet, ändert sich das Bild nicht, deshalb gewinnt dieses. Bilder aus diesem Zeitraum werden bei langen Schichten nicht ausgedünnt.
