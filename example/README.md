# Examples

## RTMP Pull Client

Run:

```bash
go run ./example/rtmp_pull_client -url "rtmp://127.0.0.1/live/stream"
```

Run with timeout:

```bash
go run ./example/rtmp_pull_client -url "rtmp://127.0.0.1/live/stream" -duration 30s
```

## HTTP-FLV Client

Run:

```bash
go run ./example/http_flv_client -url "http://127.0.0.1/live/stream.flv"
```

Run with timeout and timezone:

```bash
go run ./example/http_flv_client -url "http://127.0.0.1/live/stream.flv" -duration 30s -tz Asia/Shanghai
```
