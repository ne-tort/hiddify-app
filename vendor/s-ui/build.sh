#!/bin/sh

cd frontend
npm i
npm run build

cd ..
echo "Backend"

mkdir -p web/html
rm -fr web/html/*
cp -R frontend/dist/* web/html/

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HIDDIFY_CORE="$ROOT/hiddify-core"
if [ ! -d "$HIDDIFY_CORE" ]; then
  echo "Expected hiddify-core at $HIDDIFY_CORE (monorepo layout). Set BUILD_TAGS manually or clone hiddify-core." >&2
  exit 1
fi
# Single source of tags with hiddify client: hiddify-core/cmd/internal/build_shared/core_build_tags.go
BUILD_TAGS="$(cd "$HIDDIFY_CORE" && go run ./cmd/print_core_build_tags),with_musl"
go build -ldflags '-w -s -checklinkname=0 -extldflags "-Wl,-no_warn_duplicate_libraries"' -tags "$BUILD_TAGS" -o sui main.go
