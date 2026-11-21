# Timelapse for Prusa Connect Local.
#
#   make core      fetches the Prusa Connect Local source tree into .core
#                  (Go SDK, component library for the browser part)
#   make test      Go tests, including an end-to-end test over the module protocol
#   make check     type check of the browser part
#   make release   builds the extension into release/
#
# Needs: Go 1.26, Node 22, pnpm, git, sha256sum, ffmpeg (for the tests).

# The Prusa Connect Local release this is built and tested against.
CORE_REPO   ?= https://github.com/prusa3d/prusa-connect.git
CORE_COMMIT ?= v0.1.0-beta.1

PNPM ?= pnpm

.PHONY: core test check release clean

core:
	@if [ ! -d .core/.git ] && [ ! -L .core ]; then \
		git clone -q --depth 1 --branch $(CORE_COMMIT) $(CORE_REPO) .core; \
	fi
	cd .core/packages/typescript && CI=true $(PNPM) install --frozen-lockfile && $(PNPM) run build
	cd .core/packages/svelte && CI=true $(PNPM) install --frozen-lockfile && $(PNPM) run build

test:
	go vet ./... && go test -count=1 ./...

check:
	cd ui && CI=true $(PNPM) install && $(PNPM) run check

release:
	sh scripts/pack.sh

clean:
	rm -rf release ui/dist
