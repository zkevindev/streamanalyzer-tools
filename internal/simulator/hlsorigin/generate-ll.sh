#!/bin/sh
set -eu
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p llmedia
ffmpeg -hide_banner -loglevel error -f lavfi -i testsrc2=size=320x180:rate=15 -t 12 \
  -c:v libx264 -threads 2 -preset fast -crf 34 -pix_fmt yuv420p -profile:v baseline \
  -g 30 -keyint_min 30 -sc_threshold 0 -movflags +empty_moov+default_base_moof+skip_trailer \
  -frag_duration 400000 "$work/stream.mp4"
python3 - "$work/stream.mp4" <<'PY'
from pathlib import Path
import struct
import sys
data = Path(sys.argv[1]).read_bytes()
offset, start, fragments = 0, 0, []
while offset < len(data):
    size, kind = struct.unpack_from('>I4s', data, offset)
    if not 8 <= size <= len(data) - offset:
        raise ValueError("invalid MP4 box size")
    if kind == b'moof':
        if not start:
            Path('llmedia/init.mp4').write_bytes(data[:offset])
        start = offset
    elif kind == b'mdat':
        if not start:
            raise ValueError("missing moof")
        fragments.append(data[start:offset+size])
    offset += size
if len(fragments) != 30:
    raise ValueError(f"expected 30 parts, found {len(fragments)}")
for i, part in enumerate(fragments, 1):
    Path(f'llmedia/part-{i:02}.m4s').write_bytes(part)
PY
