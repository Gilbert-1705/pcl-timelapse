# pcl-timelapse – Zeitraffer für Prusa Connect Local

*Deutsche Übersetzung. Maßgeblich ist die englische [README.md](README.md).*

Eine Erweiterung für [Prusa Connect Local](https://github.com/prusa3d/prusa-connect), den selbst gehosteten Prusa-Connect-Server. Sie nimmt jeden Druck als Zeitraffer auf, mit einem Bild pro Schicht, und braucht dafür **keinen zusätzlichen G-Code**.

- Die Schicht kommt aus der Z-Höhe, die der Drucker beim Drucken ohnehin meldet.
- Das Bild kommt aus dem Livebild der Kamera (RTSP) oder von einer Einzelbild-Adresse (HTTP). Pro Schicht wird das Bild gewählt, auf dem der Druckkopf am wenigsten im Weg ist.
- Am Ende des Drucks entsteht ein H.264-MP4 im Speicher von Prusa Connect Local. Am Drucker gibt es einen Tab „Zeitraffer“ zum Ansehen, Herunterladen und Löschen.

Status: frühe Version (0.2.x), gebaut und getestet gegen Prusa Connect Local **v0.1.0-beta.1** mit einem Prusa CORE One und einer Buddy3D-Kamera. Die Oberfläche ist deutsch.

> **Braucht ffmpeg auf dem Server.** Prusa Connect Local und sein offizielles Docker-Image bringen kein ffmpeg mit. Ohne ffmpeg wird nichts aufgenommen, der Tab „Zeitraffer“ sagt das dann. Siehe [ffmpeg auf dem Server](#ffmpeg-auf-dem-server).

## So funktioniert es

1. **Druckstart:** Die Erweiterung öffnet den Videostrom der Kamera. Bei `rtsp://` dekodiert ffmpeg ihn mit `capture_fps` Bildern pro Sekunde, bei `http://` wird eine Einzelbild-Adresse abgefragt.
2. **Schichtwechsel:** Der Drucker meldet beim Drucken etwa alle 5 s seine Z-Höhe. Steigt Z und bleibt `layer_settle` lang oben, beginnt eine neue Schicht. Kurze Ausschläge durch Z-Hops fallen so heraus.
3. **Bildwahl:** Jedes Bild der Schicht wird mit einem Referenzbild ohne Druckkopf verglichen, dem pixelweisen Median aus den Bildern der Schicht und den zuletzt gewählten Bildern. Das ähnlichste Bild gewinnt. Helligkeitsschwankungen der Kamera werden vorher ausgeglichen.
4. **Druckende:** ffmpeg baut aus den gewählten Bildern ein MP4 (zwei Threads, höchstens 1920 Pixel breit) und legt es ab:

   ```
   <storage>/<folder>/<drucker>/<JJJJ-MM-TT_HH-MM>_<modellname>/
       zeitraffer.mp4   vorschau.jpg   info.json   [bilder/]   [diagnose/]
   ```

   Der Modellname ist der Dateiname ohne die Slicer-Angaben ab der Düse (`_0.4n_0.2mm_PETG_…`), höchstens 40 Zeichen. Bringt der Start eines Drucks den Dateinamen nicht mit, schlägt die Erweiterung ihn im Job-Datensatz des Servers nach.

Einzelbilder liegen bis zum Ende einer Schicht im Datenordner des Moduls auf der Platte, nicht im Arbeitsspeicher. Startet das Modul während eines Drucks neu, nimmt es dieselbe Aufnahme wieder auf. Schlägt das Speichern fehl, bleiben die gewählten Bilder erhalten, und beim nächsten Start der Erweiterung wird das Video daraus neu gebaut.

## Voraussetzungen

- Prusa Connect Local v0.1.0-beta.1 oder neuer.
- `ffmpeg` auf dem Server, mit libx264 (siehe [ffmpeg auf dem Server](#ffmpeg-auf-dem-server)).
- Eine Kamera mit RTSP-Strom oder einer Adresse, die ein einzelnes JPEG liefert, auf einem der Ports 554, 8554, 322 (RTSP/RTSPS), 80, 8080 oder 443 (HTTP/HTTPS). Andere Ports lehnt die Erweiterung ab, weil sie genau diese gegenüber Prusa Connect angibt.
- Der Zielspeicher muss `.mp4` und `.json` annehmen. Der Dateisystem-Speicher erlaubt ab Werk nur Druckdateien, Modelle, Firmware und Bilder. In der `config.yaml` von Prusa Connect Local braucht es deshalb eine eigene Liste, die die Standardliste enthält:

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

  Fehlt das, meldet die Erweiterung beim Speichern nur „that storage cannot be read right now“. Den eigentlichen Grund (`EXTENSION_NOT_ALLOWED`) zeigt das Server-Log.

## ffmpeg auf dem Server

ffmpeg dekodiert den RTSP-Strom und baut das MP4. Es muss dort installiert sein, wo der Prozess von Prusa Connect Local läuft, und für dessen Benutzer ausführbar sein.

- **Direkt auf einem Linux-Rechner:** das Paket der Distribution installieren, zum Beispiel `sudo apt install ffmpeg` unter Debian/Ubuntu oder `sudo dnf install ffmpeg` (RPM Fusion) unter Fedora. Die Standard-Einstellung `ffmpeg` findet es dann.
- **Docker (offizielles Image):** Das Image ist Debian trixie-slim ohne ffmpeg und läuft als Benutzer `prusa`. Ein kleines Image darauf bauen und stattdessen verwenden:

  ```dockerfile
  FROM <das Prusa-Connect-Local-Image, das du betreibst>
  USER root
  RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
  USER prusa
  ```

- **Anders:** einen ffmpeg-Build für die Plattform des Servers an einen Ort legen, den der Prozess lesen kann, und die Einstellung `ffmpeg` auf den vollen Pfad setzen.

Beim Start der Erweiterung und vor jedem Druck ruft sie `ffmpeg -version` auf. Klappt das nicht, zeigt der Tab „Zeitraffer“ einen Hinweis mit dem Grund, und der Druck wird nicht aufgenommen. Im Server-Log steht dieselbe Zeile. Sobald ffmpeg da ist, wird der nächste Druck ohne Neustart aufgenommen.

## Installation

Sobald die Erweiterung im Prusa Plugin Marketplace veröffentlicht ist, unter **Einstellungen → Erweiterungen** installieren. Bis dahin, oder um eine Änderung zu testen, ein Release bauen (siehe unten) und installieren:

```sh
prusa-connect module add github.com/gilbert-1705/pcl-timelapse@0.2.2 -from release/timelapse -approve
```

Alternativ das Release-Verzeichnis in `core.modules_local` der `config.yaml` eintragen:

```yaml
core:
  modules_local:
    github.com/gilbert-1705/pcl-timelapse: /opt/modules/timelapse
```

Danach unter **Einstellungen → Erweiterungen** aktivieren und konfigurieren. Nach einem Update den Server einmal neu starten, weil er den Browser-Teil nur beim Start lädt.

Berechtigungen: `observe` (Druckerzustand und Ereignisse lesen), `write_storage` (Videos ablegen), der Job-Datensatz eines Druckers (`core-device.jobs`, für den Dateinamen) und ein Datenordner. Netzwerkzugriff nur auf die Kamera in `camera_url`, auf den oben genannten Ports. Leitet eine Einzelbild-Adresse auf einen anderen Host oder Port weiter, folgt die Erweiterung nicht.

## Einstellungen

| Option | Standard | Bedeutung |
|---|---|---|
| `camera_url` | leer | `rtsp://…` für den Videostrom, `http://…` für ein Einzelbild. Ports 554, 8554, 322, 80, 8080 oder 443. Leer nimmt nichts auf. Beispiel: `rtsp://192.0.2.50/live` |
| `camera_username`, `camera_password` | leer | Zugangsdaten der Kamera, falls nötig. Das Passwort wird in Logs ausgeblendet. |
| `printer` | leer | Name oder ID des Druckers, den die Kamera filmt. Leer nimmt den einzigen Drucker oder den ersten, der druckt. |
| `storage` / `folder` | `local` / `zeitraffer` | Wohin die Zeitraffer geschrieben werden. |
| `capture_fps` | 2 | Bilder pro Sekunde aus dem Strom, aus denen pro Schicht gewählt wird. |
| `video_fps` | 30 | Bilder pro Sekunde im Video. 300 Schichten ergeben 10 Sekunden. |
| `hold_last_s` | 1.5 | Wie lange das letzte Bild am Ende stehen bleibt. |
| `layer_settle` | 12s | Wie lange eine neue Z-Höhe halten muss. Muss über dem Telemetrie-Abstand des Druckers liegen. |
| `min_layers` | 10 | Drucke mit weniger Schichten bekommen kein Video. |
| `max_frames_per_layer` | 240 | Obergrenze an Kandidaten pro Schicht. Lange Schichten werden ausgedünnt. |
| `keep_frames` | aus | Die gewählten Einzelbilder zusätzlich unter `bilder/` ablegen. |
| `timezone` | Europe/Berlin | Zeitzone für Datum und Uhrzeit im Ordnernamen. |
| `diagnose_layers` / `diagnose_every` | 0 / 20 | Diagnose der Bildwahl, siehe unten. |
| `performance` | `normal` | `low` für kleine Rechner wie einen Raspberry Pi, siehe unten. |
| `gcode_park` / `park_window` | aus / `20s` | Das Bild aus einer G-Code-Parkposition nehmen, siehe unten. |
| `ffmpeg` | `ffmpeg` | Pfad zu ffmpeg. Gehört nicht zu Prusa Connect Local, siehe [ffmpeg auf dem Server](#ffmpeg-auf-dem-server). |

## Kleine Rechner (Raspberry Pi)

Stell `performance` auf `low`. Die Erweiterung:

- dekodiert vom Kamerastrom nur die Keyframes (ffmpeg `-skip_frame nokey`) statt jedes Bild. Das spart am meisten CPU.
- behält höchstens ein Bild pro Sekunde, verkleinert auf 1280 Pixel Breite.
- baut das Video mit einem Thread, in 1280 Pixeln und mit niedrigerer Prozess-Priorität.

Die Aufnahme braucht weniger CPU und Speicher, das Video am Ende dauert länger, und pro Schicht gibt es weniger Bilder zur Auswahl. Wie viele, hängt vom Keyframe-Abstand der Kamera ab, meist eins alle ein bis zwei Sekunden. Bei RTSP-Kameras wirkt `capture_fps` über 1 in diesem Profil nicht. Die Release-Builds enthalten `linux_arm64`.

## Optional: Druckkopf per G-Code parken

Ab Werk braucht es keinen G-Code. Soll in jeder Schicht ein freies Bild entstehen, lässt du den Slicer den Kopf nach jedem Schichtwechsel ein paar Sekunden aus dem Bild parken und schaltest `gcode_park` ein. Die Erweiterung nimmt dann das Bild, auf dem der Kopf still steht. Das kostet etwa 4 bis 5 Sekunden pro Schicht. Schnipsel und Details stehen in [docs/gcode-park.de.md](docs/gcode-park.de.md).

## Diagnose der Bildwahl

Mit `diagnose_layers` größer 0 legt die Erweiterung für einzelne Schichten alle Kandidatenbilder ab, jede `diagnose_every`-te Schicht. Pro Schicht kommen unter `diagnose/schicht-NNNN/` hinzu:

- `kandidat-NNN.jpg`, 960 Pixel breit
- `auswahl.json` mit Zeit und Abweichung jedes Bildes und dem gewählten Bild
- `referenz.png`, das Vergleichsbild

Das sind etwa 5 bis 10 MB pro Schicht. Die Bilder zeigen den Druckraum. Bevor du sie weitergibst, prüfe, ob darauf etwas zu sehen ist, das du nicht teilen willst.

## Grenzen

- In den ersten Schichten gibt es noch keine Vergleichsbilder. Dort kann der Druckkopf im Bild bleiben.
- Steht der Kopf während einer Schicht überwiegend über dem Objekt, kann er in die Referenz geraten und im gewählten Bild sichtbar bleiben. Ohne G-Code lässt sich das nicht vollständig vermeiden. Das Parken per G-Code vermeidet es, die Diagnose hilft beim Untersuchen.
- Sehr kurze Schichten (unter `layer_settle`) verschmelzen. Das Video wird kürzer, aber nicht falsch.

## Selbst bauen

```sh
make core      # Prusa Connect Local am getesteten Tag nach .core holen, Bibliotheken bauen
make test      # Go-Tests, inklusive Ende-zu-Ende-Test über das Modulprotokoll (braucht ffmpeg)
make check     # Typprüfung des Browser-Teils
make ui-build  # nur das Seitenbündel nach ui/dist (das baut der Marketplace-Builder)
make release   # release/timelapse/ und release/timelapse-<version>.tar.gz
```

Nötig sind Go 1.26, Node 22, pnpm 11 und ffmpeg. `PLATFORMS=linux_amd64 make release` baut nur eine Plattform.

## Sicherheitshinweise

Was die Erweiterung startet, wohin sie sich verbindet und was sie schreibt, mit Begründung (englisch): [docs/security-notes.md](docs/security-notes.md).

## Releases

Releases baut und veröffentlicht der Builder des Prusa Plugin Marketplace aus einem GitHub-Release mit dem Tag `v<version>` (`.github/workflows/release.yml`). Jeder Pull Request lässt denselben Builder als Probelauf laufen (`.github/workflows/release-check.yml`): Er baut und prüft alles, lädt aber nichts hoch. Änderungen stehen in [CHANGELOG.md](CHANGELOG.md).

## Sicherheit

Sicherheitslücken bitte nicht als öffentliches Issue melden, sondern wie in [SECURITY.md](SECURITY.md) beschrieben.

## Lizenz

MIT, siehe [LICENSE](LICENSE). Aufgebaut auf Prusa Connect Local von Prusa Research a.s.
