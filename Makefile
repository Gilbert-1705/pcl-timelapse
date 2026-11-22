# Timelapse for Prusa Connect Local.
#
#   make core      fetches the Prusa Connect Local source tree into .core
#                  (Go SDK, component library for the browser part)
#   make test      Go tests, including an end-to-end test over the module protocol
#   make check     type check of the browser part
#   make ui-build  builds only the browser bundle into ui/dist (module.yaml
#                  at its root, the bundle under ui/). The marketplace
#                  builder runs this; it works when .core is already there.
#   make release   builds the extension into release/
#
# Needs: Go 1.26, Node 22, pnpm, git, sha256sum, ffmpeg (for the tests).

# The Prusa Connect Local release this is built and tested against.
CORE_REPO   ?= https://github.com/prusa3d/prusa-connect.git
CORE_COMMIT ?= v0.1.0-beta.1

PNPM ?= pnpm
CORE := .core

# No interactive prompts from pnpm or corepack, here or in CI.
export CI ?= true
export COREPACK_ENABLE_DOWNLOAD_PROMPT ?= 0

.PHONY: core test check ui-build release clean

# The checkout is cloned once. A .core that is already there (the
# marketplace builder checks it out itself, or a symlink) is used as it is.
$(CORE)/sdk/go.mod:
	@if [ ! -e $(CORE) ]; then \
		git -c advice.detachedHead=false clone -q --depth 1 --branch $(CORE_COMMIT) $(CORE_REPO) $(CORE); \
	fi
	@test -f $@ || { echo "$(CORE) is there but holds no sdk/go.mod"; exit 1; }

# The core's typed client and the component library the bundle builds
# against. Each is built once; delete its dist/ to build it again.
$(CORE)/packages/typescript/dist/index.js: | $(CORE)/sdk/go.mod
	cd $(CORE)/packages/typescript && $(PNPM) install --frozen-lockfile && $(PNPM) run build

$(CORE)/packages/svelte/dist/build/module.js: | $(CORE)/sdk/go.mod
	cd $(CORE)/packages/svelte && $(PNPM) install --frozen-lockfile && $(PNPM) run build

core: $(CORE)/packages/typescript/dist/index.js $(CORE)/packages/svelte/dist/build/module.js

ui-build: core
	cd ui && $(PNPM) install --frozen-lockfile && $(PNPM) run build
	@test -f ui/dist/module.yaml && test -d ui/dist/ui || { echo "ui/dist lacks module.yaml or ui/"; exit 1; }

test:
	go vet ./... && go test -count=1 ./...

check:
	cd ui && CI=true $(PNPM) install && $(PNPM) run check

release:
	sh scripts/pack.sh

clean:
	rm -rf release ui/dist
