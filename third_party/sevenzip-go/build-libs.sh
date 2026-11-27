#!/usr/bin/env bash
set -euo pipefail

source_dir=${1:-${TMPDIR:-/tmp}/lib7zip-build}
output_dir=${2:-$(pwd)/lib7zip}
tag=v1.8.0
cxx=${LIB7ZIP_CXX:-g++}
case "$cxx" in
	*clang++) cc=${LIB7ZIP_CC:-${cxx%++}} ;;
	*) cc=${LIB7ZIP_CC:-${cxx/++/cc}} ;;
esac
prebuilt_7z=${LIB7ZIP_7Z_SO:-}
codec_dir=${LIB7ZIP_CODECS_DIR:-}

for tool in git cmake make "$cxx" python3; do
	command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
done

if [ -z "$prebuilt_7z" ] && [ -z "${LIB7ZIP_CXX:-}" ] && [ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ]; then
	for tool in curl unzip sha256sum; do
		command -v "$tool" >/dev/null || { echo "missing required tool: $tool" >&2; exit 1; }
	done
	# shellcheck disable=SC1091
	. "$(dirname "$0")/PREBUILT"
	cache_dir="$source_dir/prebuilt"
	mkdir -p "$cache_dir"
	zip_path="$cache_dir/prebuilt.zip"
	if [ ! -f "$zip_path" ] || ! echo "$PREBUILT_SHA256  $zip_path" | sha256sum -c - >/dev/null 2>&1; then
		curl -fsSL "$PREBUILT_URL" -o "$zip_path"
		echo "$PREBUILT_SHA256  $zip_path" | sha256sum -c -
	fi
	rm -rf "$cache_dir/extracted"
	mkdir -p "$cache_dir/extracted"
	unzip -q "$zip_path" '7z.so' 'Codecs/*' -d "$cache_dir/extracted"
	prebuilt_7z="$cache_dir/extracted/7z.so"
	codec_dir="$cache_dir/extracted/Codecs"
fi

if [ ! -d "$source_dir/.git" ]; then
	git clone --depth 1 --branch "$tag" https://github.com/NyaMisty/libc7zip.git "$source_dir"
fi

# libc7zip does not export the FILETIME property accessor; sz/glue.c treats it as optional.
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

# 7-Zip reads a RAR5 extraction password from the archive or item, not from the
# password passed to OpenArchiveEx.
password_declaration = "MYEXPORT int archive_extract_item_with_password(archive *a, item *i, out_stream *os, const char *password);"
password_header_anchor = "MYEXPORT int archive_extract_item(archive *a, item *i, out_stream *os);"
password_source_anchor = "MYEXPORT int archive_extract_item(archive *a, item *i, out_stream *os) {"
password_implementation = """MYEXPORT int archive_extract_item_with_password(archive *a, item *i, out_stream *os, const char *password) {
\treturn a->arch->Extract((unsigned int)i->itm->GetArchiveIndex(), os->strm, FromCString((char *)password));
}

"""

h = header.read_text()
if "item_get_filetime_property" not in h:
    h = h.replace(header_anchor, declaration + "\n" + header_anchor, 1)
if "archive_extract_item_with_password" not in h:
    h = h.replace(password_header_anchor, password_declaration + "\n" + password_header_anchor, 1)
header.write_text(h)

null_anchor = """\t\tin_stream *is = m_def.open_current_volume_stream_cb(m_def.id);
\t\treturn is->strm;"""
null_fixed = """\t\tin_stream *is = m_def.open_current_volume_stream_cb(m_def.id);
\t\tif (is == NULL || is->strm == NULL) {
\t\t\treturn NULL;
\t\t}
\t\treturn is->strm;"""

c = source.read_text()
if "MYEXPORT uint64_t item_get_filetime_property" not in c:
    c = c.replace(source_anchor, implementation + source_anchor, 1)
if "archive_extract_item_with_password" not in c:
    c = c.replace(password_source_anchor, password_implementation + password_source_anchor, 1)
if null_anchor in c:
    c = c.replace(null_anchor, null_fixed, 1)
source.write_text(c)

# A failed volume callback must report a failing HRESULT. Returning false
# reports S_OK and dereferences a null archive in C7ZipLibrary.
volumes = root / "vendor/lib7zip/Lib7Zip/7ZipOpenArchive.cpp"
v = volumes.read_text()
for anchor in ("if (!pMultiVolumes->MoveToVolume(firstVolumeName))", "if (pInStream == NULL)"):
    old = anchor + "\n\t\treturn false;"
    if old in v:
        v = v.replace(old, anchor + "\n\t\treturn E_FAIL;", 1)
volumes.write_text(v)

callback = root / "vendor/lib7zip/Lib7Zip/7ZipArchiveOpenCallback.cpp"
cb = callback.read_text()
anchor = "\t\tpInStream = m_pMultiVolumes->OpenCurrentVolumeStream();"
old = anchor + "\n\t} else {\n\t\treturn S_FALSE;\n\t}\n\n    C7ZipInStreamWrapper * pArchiveStream = new C7ZipInStreamWrapper(pInStream);"
if old in cb:
    cb = cb.replace(old, anchor + "\n\t} else {\n\t\treturn S_FALSE;\n\t}\n\tif (pInStream == NULL)\n\t\treturn S_FALSE;\n\n    C7ZipInStreamWrapper * pArchiveStream = new C7ZipInStreamWrapper(pInStream);", 1)
callback.write_text(cb)
PY

export CMAKE_POLICY_VERSION_MINIMUM=3.5
CXX="$cxx" cmake -S "$source_dir" -B "$source_dir/build" ${LIB7ZIP_CMAKE_ARGS:-}
cmake --build "$source_dir/build" --parallel

mkdir -p "$output_dir"
cp "$source_dir/build/libc7zip.so" "$output_dir/"

if [ -n "$prebuilt_7z" ]; then
	[ -f "$prebuilt_7z" ] || { echo "prebuilt 7z.so not found: $prebuilt_7z" >&2; exit 1; }
	cp "$prebuilt_7z" "$output_dir/7z.so"
	if [ -n "$codec_dir" ]; then
		[ -d "$codec_dir" ] || { echo "codec directory not found: $codec_dir" >&2; exit 1; }
		cp -a "$codec_dir" "$output_dir/Codecs"
	fi
else
	# libc7zip's CMake downloads p7zip 16.02 during configuration.
	engine_dir="$source_dir/build/upstream-7zip-src"
	make -C "$engine_dir" common7z CC="$cc" CXX="$cxx -std=c++11"
	cp "$engine_dir/bin/7z.so" "$output_dir/"
fi

echo "lib7zip sidecars written to $output_dir"
