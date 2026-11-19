#!/bin/sh
set -eu
for variant in low high; do
    size=160x90
    crf=30
    if [ "$variant" = high ]; then
        size=320x180
        crf=27
    fi
    media_dir="abrmedia/$variant"
    mkdir -p "$media_dir"
    ffmpeg -hide_banner -loglevel error -y -f lavfi -i "testsrc2=size=${size}:rate=15" \
        -f lavfi -i sine=frequency=440:sample_rate=48000 -t 12 -c:v libx264 -threads 2 \
        -preset fast -crf "$crf" -pix_fmt yuv420p -profile:v baseline -level 3.0 -g 30 -sc_threshold 0 \
        -c:a aac -b:a 32k -hls_time 2 -hls_list_size 0 -hls_playlist_type vod -hls_segment_type fmp4 \
        -hls_fmp4_init_filename init.mp4 -start_number 1 -hls_segment_filename "$media_dir/seg-%02d.m4s" "$media_dir/source.m3u8"
    rm "$media_dir/source.m3u8"
done
