#!/usr/bin/env bash
# Builds the desktop app as a finished package for this OS, into dist/:
#   Windows  dist/Carta.exe             with its icon and version information
#   Linux    dist/Carta.tar.xz          with a .desktop entry and icon
#
# fyne package reads its metadata from FyneApp.toml and writes it back with
# the build number incremented, so the file is generated here from
# internal/version/VERSION on every run, never committed: one place for the
# version, and no file that changes because a build ran.
set -euo pipefail
cd "$(dirname "$0")/cmd/carta-desktop"

version=$(tr -d '[:space:]' < ../../../internal/version/VERSION)
IFS=. read -r major minor patch <<< "$version"
build=$((major * 10000 + minor * 100 + patch)) # the Android versionCode rule

cat > FyneApp.toml <<TOML
Website = "https://github.com/Tanmay-Anand/Carta"

[Details]
Icon = "Icon.png"
Name = "Carta"
ID = "io.github.tanmayanand.carta.desktop"
Version = "$version"
Build = $build
TOML

case "$(go env GOOS)" in
  windows) os=windows; out=Carta.exe ;;
  linux)   os=linux;   out=Carta.tar.xz ;;
  *) echo "package.sh: no desktop package for $(go env GOOS)" >&2; exit 1 ;;
esac
fyne package -os "$os" --app-version "$version" --app-build "$build" --release
rm -f FyneApp.toml
mkdir -p ../../../dist
mv -f "$out" ../../../dist/
echo "dist/$out ($version)"
