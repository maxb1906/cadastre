#!/usr/bin/env bash
# Cross oracle on Linux: scans the NTFS fixture with engine A (raw MFT) and
# compares it with a walk of the same image mounted read-only by ntfs-3g.
# Expected remaining differences are ntfs-3g artifacts: the junction and the
# Interix symlink are presented as POSIX symlinks.
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
trap 'sudo umount "$work/mnt" 2>/dev/null || true; rm -rf "$work"' EXIT
mkdir "$work/mnt"
zstd -dqc testdata/ntfs-fixture.img.zst > "$work/ntfs.img"
go build -o "$work/cadastre" .
sudo ntfs-3g -o ro,streams_interface=windows "$work/ntfs.img" "$work/mnt"
sudo "$work/cadastre" check --image "$work/ntfs.img" --against "$work/mnt" "$@"
