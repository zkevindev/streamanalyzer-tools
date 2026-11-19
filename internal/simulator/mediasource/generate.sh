#!/bin/sh
set -eu
mkdir -p fixtures
for name in regular long-gop; do
    keyframes=10
    duration=7
    if [ "$name" = long-gop ]; then
        keyframes=120
        duration=14
    fi
    ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=160x96:rate=10 \
        -f lavfi -i sine=frequency=440:sample_rate=48000 -t "$duration" -c:v libx264 -threads 2 \
        -preset veryfast -crf 34 -pix_fmt yuv420p -bf 0 -g "$keyframes" -sc_threshold 0 \
        -c:a aac -b:a 24k -f flv "fixtures/$name.flv"
done
ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=160x96:rate=10 \
    -f lavfi -i sine=frequency=440:sample_rate=48000 -t 7 -c:v libx264 -threads 2 \
    -preset veryfast -crf 32 -pix_fmt yuv420p -bf 0 -g 10 -sc_threshold 0 -c:a aac -b:a 24k \
    -streamid 0:256 -streamid 1:257 -muxrate 260000 -pcr_period 20 -f mpegts fixtures/regular.ts
