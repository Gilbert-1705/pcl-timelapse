#!/bin/sh
# Builds the extension as a release folder and archive, the way
# `prusa-connect module add -from <folder>` expects it:
#
#   release/timelapse/module.yaml
#   release/timelapse/ui/index.js (+ ui/locales)
#   release/timelapse/timelapse_<os>_<arch>   one per platform in module.yaml
#   release/timelapse/checksums.txt
#
# PLATFORMS=linux_amd64 builds only that platform, otherwise all from module.yaml.
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)

[ -d "$ROOT/.core/sdk" ] || { echo "MISSING .core: run 'make core' first"; exit 1; }

NAME=$(sed -n 's/^name:[[:space:]]*//p' "$ROOT/module.yaml" | head -1)
VERSION=$(sed -n 's/^version:[[:space:]]*//p' "$ROOT/module.yaml" | head -1)
PLATFORMS=${PLATFORMS:-$(awk '/^platforms:/{f=1;next} f&&/^[[:space:]]*-/{print $2;next} f{exit}' "$ROOT/module.yaml")}
OUT=$ROOT/release/$NAME

echo "==> $NAME $VERSION"

echo "    Go tests"
(cd "$ROOT" && go vet ./... && go test -count=1 -short ./...)

echo "    browser part"
mkdir -p "$ROOT/release"
(cd "$ROOT/ui" && CI=true pnpm install && pnpm run check && pnpm run build) > "$ROOT/release/.ui.log" 2>&1 ||
	{ cat "$ROOT/release/.ui.log"; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT"
cp -R "$ROOT/ui/dist/." "$OUT/"

for platform in $PLATFORMS; do
	os=${platform%_*}
	arch=${platform#*_}
	echo "    $platform"
	(cd "$ROOT" && CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags "-s -w" \
		-o "$OUT/${NAME}_${platform}" .)
	chmod +x "$OUT/${NAME}_${platform}"
done

(
	cd "$OUT"
	find . -type f ! -name checksums.txt | sed 's|^\./||' | LC_ALL=C sort | while IFS= read -r f; do
		sha256sum "$f"
	done > checksums.txt
)

(cd "$OUT" && tar -czf "$ROOT/release/$NAME-$VERSION.tar.gz" -- *)

echo "    done: release/$NAME/ and release/$NAME-$VERSION.tar.gz"
