#!/bin/sh
# How the fixtures in this directory were made. Not run by the tests: the
# dumps are of real faults on real hardware, so they are committed.
#
#   gen.sh arm <toolchain prefix>   e.g. .../bin/arm-openipc-linux-musleabi-
#   gen.sh x86_64
#
# Then, on the target, in an empty environment -- the stack slice reaches
# the top of the stack, where the environment and argv are:
#   cd /tmp/toy && env -i LD_LIBRARY_PATH=/tmp/toy ./toy <mode> /tmp/toy/<mode>
# for each mode (own, lib, libc, sent), and keep each majestic.dump as
# <arch>-<mode>.dump.
set -eu
arch=$1
cc=${2:-}gcc
objcopy=${2:-}objcopy
strip=${2:-}strip
# Paths in the debuginfo say nothing about where it was built.
map="-fdebug-prefix-map=$(pwd)=."
sysroot=$($cc -print-sysroot)
[ -n "$sysroot" ] && map="$map -fdebug-prefix-map=$sysroot=/sysroot"
case $arch in
  arm) flags="-mthumb -Os $map" ;;
  *) flags="-Os $map" ;;
esac
out=$arch
mkdir -p "$out"
# libtoy.so: no unwind tables, stripped -- a vendor library.
$cc $flags -fPIC -shared -fno-asynchronous-unwind-tables -fno-unwind-tables \
  -Wl,--build-id=none -o "$out/libtoy.so" libtoy.c
$strip "$out/libtoy.so"
# toy: a PIE with .debug_frame and DWARF lines in its debuginfo, none in the
# executable -- majestic's release build.
$cc $flags -g -fPIE -pie -fno-asynchronous-unwind-tables -fno-unwind-tables \
  -Wl,--build-id=sha1 -o "$out/toy.full" toy.c -L"$out" -ltoy
$objcopy --only-keep-debug "$out/toy.full" "$out/toy.debug"
$strip -o "$out/toy" "$out/toy.full"
rm "$out/toy.full"
