#!/usr/bin/env python3
"""Build lib/lzma: XZ Utils' liblzma of its release tarball, for lib/c.

    make.py <cc> <ar> <tarball> <libc-include> <liblzma.a> <include.tar>

liblzma.a is the library as configure builds it for one thread: every
encoder and decoder of every filter, the match finders, the checks CRC32,
CRC64 and SHA-256, its own; config.h below is configure's answers for
lib/c. include.tar is include/, lzma.h and lzma/.
"""
import os
import shutil
import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent.parent / "dev"))

import wasm  # noqa: E402

FILTERS = ["ARM", "ARM64", "ARMTHUMB", "DELTA", "IA64", "LZMA1", "LZMA2", "POWERPC", "RISCV", "SPARC", "X86"]
CONFIG = "\n".join([
    "#define ASSUME_RAM 128",
    "#define HAVE_CHECK_CRC32 1",
    "#define HAVE_CHECK_CRC64 1",
    "#define HAVE_CHECK_SHA256 1",
    "#define HAVE_DECODERS 1",
    "#define HAVE_ENCODERS 1",
    *(f"#define HAVE_{side}_{name} 1" for side in ("DECODER", "ENCODER") for name in FILTERS),
    "#define HAVE_LZIP_DECODER 1",
    *(f"#define HAVE_MF_{name} 1" for name in ("BT2", "BT3", "BT4", "HC3", "HC4")),
    "#define HAVE_INTTYPES_H 1",
    "#define HAVE_STDBOOL_H 1",
    "#define HAVE__BOOL 1",
    "#define HAVE_STDINT_H 1",
    "#define HAVE_STDLIB_H 1",
    "#define HAVE_STRING_H 1",
    "#define HAVE_STRINGS_H 1",
    "#define HAVE_SYS_PARAM_H 1",
    "#define HAVE_SYS_STAT_H 1",
    "#define HAVE_SYS_TYPES_H 1",
    "#define HAVE_UNISTD_H 1",
    "#define HAVE_VISIBILITY 0",
    "#define HAVE___BUILTIN_BSWAPXX 1",
    "#define HAVE___BUILTIN_ASSUME_ALIGNED 1",
    "#define SIZEOF_SIZE_T 4",
    "#define STDC_HEADERS 1",
    "#define TUKLIB_CPUCORES_SYSCONF 1",
    "#define TUKLIB_PHYSMEM_SYSCONF 1",
    "#define TUKLIB_SYMBOL_PREFIX lzma_",
    "",
])
SOURCES = [
    *(f"src/liblzma/common/{name}.c" for name in (
        "alone_decoder", "alone_encoder", "auto_decoder", "block_buffer_decoder", "block_buffer_encoder", "block_decoder",
        "block_encoder", "block_header_decoder", "block_header_encoder", "block_util", "common", "easy_buffer_encoder",
        "easy_decoder_memusage", "easy_encoder", "easy_encoder_memusage", "easy_preset", "file_info", "filter_buffer_decoder",
        "filter_buffer_encoder", "filter_common", "filter_decoder", "filter_encoder", "filter_flags_decoder",
        "filter_flags_encoder", "hardware_cputhreads", "hardware_physmem", "index", "index_decoder", "index_encoder",
        "index_hash", "lzip_decoder", "microlzma_decoder", "microlzma_encoder", "stream_buffer_decoder",
        "stream_buffer_encoder", "stream_decoder", "stream_encoder", "stream_flags_common", "stream_flags_decoder",
        "stream_flags_encoder", "string_conversion", "vli_decoder", "vli_encoder", "vli_size")),
    *(f"src/liblzma/check/{name}.c" for name in ("check", "crc32_fast", "crc64_fast", "sha256")),
    *(f"src/liblzma/delta/{name}.c" for name in ("delta_common", "delta_decoder", "delta_encoder")),
    *(f"src/liblzma/lz/{name}.c" for name in ("lz_decoder", "lz_encoder", "lz_encoder_mf")),
    *(f"src/liblzma/lzma/{name}.c" for name in (
        "fastpos_table", "lzma2_decoder", "lzma2_encoder", "lzma_decoder", "lzma_encoder", "lzma_encoder_optimum_fast",
        "lzma_encoder_optimum_normal", "lzma_encoder_presets")),
    "src/liblzma/rangecoder/price_table.c",
    *(f"src/liblzma/simple/{name}.c" for name in (
        "arm", "arm64", "armthumb", "ia64", "powerpc", "riscv", "simple_coder", "simple_decoder", "simple_encoder", "sparc",
        "x86")),
    "src/common/tuklib_cpucores.c",
    "src/common/tuklib_physmem.c",
]
INCLUDES = ["api", "common", "check", "delta", "lz", "lzma", "rangecoder", "simple"]


def main():
    cc, ar, tarball, libc_include, library, include_tar = sys.argv[1:7]
    work = Path(os.environ.get("TMPDIR") or "/tmp") / "liblzma"
    shutil.rmtree(work, ignore_errors=True)
    tree = wasm.unpack(tarball, work / "src")
    (work / "config").mkdir()
    (work / "config" / "config.h").write_text(CONFIG)
    flags = ["-O2", "-w", "-DHAVE_CONFIG_H", "-DNDEBUG", "-I", str(work / "config"), "-I", "src/common",
             *(arg for name in INCLUDES for arg in ("-I", f"src/liblzma/{name}")), "-isystem", str(Path(libc_include).resolve())]
    wasm.library(cc, ar, tree, SOURCES, flags, library, work)
    api = tree / "src" / "liblzma" / "api"
    wasm.headers(include_tar, work / "out" / "include",
                 [(api / "lzma.h", "lzma.h"), *((path, f"lzma/{path.name}") for path in sorted((api / "lzma").glob("*.h")))])


if __name__ == "__main__":
    main()
