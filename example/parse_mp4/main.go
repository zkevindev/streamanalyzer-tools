// parse_mp4：展示 stsd、meta、样例时间线；可选 -video/-audio 导出 Annex-B（H.264 / H.265）与 ADTS AAC（与 parse_flv 对齐）。
// 逻辑在 internal/mp4parse，本目录为命令行入口。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"streamanalyzer/internal/mp4parse"
)

func main() {
	input := flag.String("i", "", "输入 MP4 文件路径")
	maxSamples := flag.Int("n", 40, "每轨道最多打印的样例条数（0=全部，大文件慎用）")
	videoOut := flag.String("video", "", "导出 Annex-B（avcC/hvcC 参数集；第 1 帧与各 I 帧(stss)前插入，便于 ffplay 直接打开）")
	audioOut := flag.String("audio", "", "导出音频为 ADTS AAC（空则跳过；需 mp4a + esds）")
	flag.Parse()

	if strings.TrimSpace(*input) == "" {
		fmt.Fprintln(os.Stderr, "请使用 -i 指定 MP4，例如: go run . -i /path/to/bbb_30fps_gop_60_3mbps.mp4")
		os.Exit(2)
	}

	if err := mp4parse.Run(mp4parse.Config{
		InputPath:      *input,
		MaxSampleLines: *maxSamples,
		VideoOut:       strings.TrimSpace(*videoOut),
		AudioOut:       strings.TrimSpace(*audioOut),
		ReportWriter:   os.Stdout,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}
