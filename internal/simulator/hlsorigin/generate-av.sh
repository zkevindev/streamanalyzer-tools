#!/bin/sh
set -eu
mkdir -p avmedia/audio avmedia/video
ffmpeg -hide_banner -loglevel error -y -f lavfi -i testsrc2=size=320x180:rate=15 -t 12 \
  -c:v libx264 -threads 2 -preset fast -crf 30 -pix_fmt yuv420p -profile:v baseline -g 30 -sc_threshold 0 \
  -hls_time 2 -hls_list_size 0 -start_number 1 -hls_segment_type fmp4 -hls_fmp4_init_filename init.mp4 \
  -hls_segment_filename avmedia/video/seg-%02d.m4s avmedia/video/playlist.m3u8
ffmpeg -hide_banner -loglevel error -y -f lavfi -i sine=frequency=440:sample_rate=48000 -t 12 \
  -c:a aac -b:a 48k -hls_time 2 -hls_list_size 0 -start_number 1 -hls_segment_type fmp4 \
  -hls_fmp4_init_filename init.mp4 -hls_segment_filename avmedia/audio/seg-%02d.m4s avmedia/audio/playlist.m3u8
rm -f avmedia/video/playlist.m3u8 avmedia/audio/playlist.m3u8 avmedia/audio/seg-07.m4s
