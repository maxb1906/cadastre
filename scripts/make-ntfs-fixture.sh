#!/usr/bin/env bash
# Builds testdata/ntfs-fixture.img.zst: a real NTFS volume formatted by mkntfs
# and populated through ntfs-3g, used as an oracle for the MFT parser on Linux.
# Requires: ntfs-3g, mkntfs, zstd, sudo (for the FUSE mount).
set -euo pipefail
cd "$(dirname "$0")/.."
work=$(mktemp -d)
img="$work/ntfs.img"
mnt="$work/mnt"
mkdir -p "$mnt"
trap 'cd /; sudo umount "$mnt" 2>/dev/null || true; sleep 1; rm -rf "$work"' EXIT

truncate -s 48M "$img"
mkntfs -F -Q -q -H 0 -S 0 -p 0 -c 4096 -s 512 -L CADTEST "$img"
sudo ntfs-3g -o streams_interface=windows,compression,uid=$(id -u),gid=$(id -g) "$img" "$mnt"

cd "$mnt"
mkdir -p Users/alice/AppData/Local/Temp Windows/Temp "Données été" deep
head -c 10000 /dev/zero > Users/alice/notes.txt                 # 10000 bytes
head -c 300 < <(yes cadastre) > Users/alice/AppData/Local/Temp/small.tmp  # resident
head -c 1048576 < <(yes cadastre) > Windows/Temp/big.log             # 1 MiB
ln Windows/Temp/big.log Users/alice/hardlink.log                # hard link
echo -n "stream payload of 30 bytes!!!!" > "Users/alice/notes.txt:secret"  # ADS
head -c 200000 < <(yes cadastre) > "Users/alice/notes.txt:bigads"    # non-resident ADS
truncate -s 100M sparse.bin                                     # sparse, 0 alloc
printf 'x' | dd of=sparse.bin bs=1 seek=50000000 conv=notrunc status=none
echo "unicode" > "Données été/日本語 😀.txt"
ln -s ../Windows/Temp/big.log deep/link-to-big                   # interix symlink
mkdir deep/junction                                             # junction (reparse point)
setfattr -n system.ntfs_reparse_data -v "$(python3 -c '
import struct
sub = "\\??\\C:\\Windows".encode("utf-16-le"); prt = "C:\\Windows".encode("utf-16-le")
body = struct.pack("<HHHH", 0, len(sub), len(sub) + 2, len(prt)) + sub + b"\0\0" + prt + b"\0\0"
print("0x" + (struct.pack("<IHH", 0xA0000003, len(body), 0) + body).hex())')" deep/junction
mkdir compressed
setfattr -n system.ntfs_attrib_be -v 0x00000800 compressed       # compressed dir
head -c 262144 /dev/zero > compressed/zeros.bin                   # compresses well
p=deep
for i in $(seq 1 30); do p="$p/niveau-$i-abcdefghij"; done      # path > 260 chars
mkdir -p "$p"
echo "deep" > "$p/fond.txt"
mkdir many
# Grow the MFT in two phases separated by a large allocation, so that the
# second extension of $MFT lands in another fragment.
for i in $(seq 1 1500); do echo "$i" > "many/f$i.txt"; done
head -c 20000000 < <(yes filler) > filler.bin
for i in $(seq 1501 6000); do echo "$i" > "many/f$i.txt"; done
cd - >/dev/null
sync
sudo umount "$mnt"
zstd -q -19 -f "$img" -o testdata/ntfs-fixture.img.zst
ls -l testdata/ntfs-fixture.img.zst
