#!/usr/bin/env bash
# Build the matched libc7zip + 7z shared libraries used by the cgo engine.
# Usage: build-libs.sh [build-dir] [output-dir]
set -euo pipefail

source_dir=${1:-${TMPDIR:-/tmp}/lib7zip-build}
output_dir=${2:-$(pwd)/lib7zip}
tag=v1.8.0

for tool in git cmake make g++ python3; do
	command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
done

if [ ! -d "$source_dir/.git" ]; then
	git clone --depth 1 --branch "$tag" https://github.com/NyaMisty/libc7zip.git "$source_dir"
fi

# The fork does not export the FILETIME property accessor. Add it so item
# modification times survive extraction; sz/glue.c treats it as optional.
python3 - "$source_dir" <<'PY'
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
header = root / "include/libc7zip.h"
source = root / "src/context.cc"
declaration = "MYEXPORT uint64_t item_get_filetime_property(item *i, int32_t property_index, int32_t *success);"
header_anchor = "MYEXPORT int32_t item_get_bool_property(item *i, int32_t property_index, int32_t *success);"
source_anchor = "MYEXPORT int32_t item_get_bool_property(item *i, int32_t property_index, int32_t *success) {"
implementation = """MYEXPORT uint64_t item_get_filetime_property(item *i, int32_t property_index, int32_t *success) {
\tunsigned __int64 ret = 0;
\tauto pi = (lib7zip::PropertyIndexEnum)(property_index);
\tauto success_in = i->itm->GetFileTimeProperty(pi, ret);
\tif (success) {
\t\t*success = success_in;
\t}
\treturn ret;
}

"""

h = header.read_text()
if "item_get_filetime_property" not in h:
    header.write_text(h.replace(header_anchor, declaration + "\n" + header_anchor, 1))

c = source.read_text()
if "MYEXPORT uint64_t item_get_filetime_property" not in c:
    source.write_text(c.replace(source_anchor, implementation + source_anchor, 1))
PY

cmake -S "$source_dir" -B "$source_dir/build"
cmake --build "$source_dir/build" --parallel

# libc7zip's CMake downloads p7zip 16.02 during configuration. The 7z plugin
# must come from that same tree: newer 7z.so builds crash in OpenArchiveEx.
engine_dir="$source_dir/build/upstream-7zip-src"
make -C "$engine_dir" common7z CXX="g++ -std=c++11"

mkdir -p "$output_dir"
cp "$source_dir/build/libc7zip.so" "$output_dir/"
cp "$engine_dir/bin/7z.so" "$output_dir/"
echo "lib7zip sidecars written to $output_dir"
