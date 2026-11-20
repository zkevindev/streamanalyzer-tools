package diagnostic

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const reportAssessmentVersion = "report-assessment/1"

type ReportAssessment struct {
	Version     string           `json:"version"`
	Conclusion  string           `json:"conclusion"`
	Directions  []StallDirection `json:"directions"`
	Limitations []string         `json:"limitations"`
}

func assessReport(report Report) *ReportAssessment {
	result := &ReportAssessment{Version: reportAssessmentVersion, Directions: []StallDirection{},
		Limitations: []string{"单次、单接收点采样用于选择验证方向，不能单独确定源端或中间网络根因。", "未采集或未观测不等于数值为零；协议支持与浏览器能力限制需单独核对。"}}
	idsFor := func(match func(string) bool) []string {
		var ids []string
		for _, finding := range report.Findings {
			if match(finding.Code) {
				ids = append(ids, finding.ID)
			}
		}
		return ids
	}
	add := func(code, status, title, message string, ids []string, checks ...string) {
		if len(ids) == 0 {
			return
		}
		result.Directions = append(result.Directions, StallDirection{Code: code, Status: status, Title: title, Message: message, EvidenceIDs: ids, NextChecks: checks})
	}
	connectionIDs := idsFor(func(code string) bool {
		return code == "http_status" || code == "request_error" || code == "rtmp_error" || code == "rtsp_error" || strings.HasPrefix(code, "connection_")
	})
	observation := report.Manifest.Observation
	connectionCheck := "核对目标服务状态、端口、防火墙及源站日志后，从同一设备复测。"
	switch observation.FailureCode {
	case "dns_error":
		connectionCheck = "核对本机 DNS 解析结果、搜索域和代理配置，并比较另一解析器或接收点。"
	case "tls_error":
		connectionCheck = "核对证书链、主机名、有效期和客户端 CA；不要用跳过证书校验替代修复。"
	case "rtsp_auth_error":
		connectionCheck = "核对设备凭据、Digest/Basic 认证支持和账号权限，认证原文不会写入报告。"
	case "http_status":
		connectionCheck = "结合 HTTP 状态核对播放鉴权、Referer/Cookie、源站可用性和 CDN 返回记录。"
	case "connection_refused":
		connectionCheck = "核对目标端口是否监听、服务是否运行以及本机到目标端口的防火墙策略。"
	}
	add("connection_or_negotiation", "observed", "已观测连接或协议协商失败",
		"失败发生在媒体持续接收之前，应先修复对应连接或协商阶段，再评价卡顿和音视频质量。", connectionIDs, connectionCheck)

	noMediaIDs := idsFor(func(code string) bool {
		return code == "no_media" || code == "hls_no_media" || code == "ts_no_media" || code == "rtsp_no_media" || code == "rtsp_no_rtp"
	})
	add("no_media", "observed", "已观测采样窗口内无完整媒体",
		"当前证据不能区分源端未发送、封装不支持或媒体传输未建立；连接成功也不代表已经收到媒体。", noMediaIDs,
		"确认发送端正在输出，核对 SDP/播放列表、UDP 端口与来源过滤，并延长采样复测。")

	slowIDs := idsFor(func(code string) bool {
		return strings.Contains(code, "slow_first") || strings.Contains(code, "first_frame") && strings.Contains(code, "slow")
	})
	add("slow_start", "observed", "已观测首媒体或首屏偏慢",
		"首屏由连接协商、关键帧到达、解码和自动播放策略共同影响，应结合阶段耗时定位等待区间。", slowIDs,
		"对照 DNS/TCP/TLS、首个关键帧和播放器首次呈帧时间，检查 GOP 与浏览器自动播放状态。")

	transportIDs := idsFor(func(code string) bool {
		return strings.Contains(code, "arrival_gap") || strings.Contains(code, "tail_gap") || strings.Contains(code, "continuity") ||
			strings.Contains(code, "sequence_gap") || strings.Contains(code, "packet_loss") || strings.Contains(code, "playlist_failure") || strings.Contains(code, "segment_failed")
	})
	add("source_or_transport", "candidate", "优先核对媒体供给与传输连续性",
		"接收间隔、序号或连续计数异常定位到当前接收点；源端漏发、链路丢包和本机接收缓冲仍需对照验证。", transportIDs,
		"比较源端发送记录和另一接收点，检查网卡丢包、socket 缓冲、CDN/代理及服务端发送队列。")

	playerIDs := idsFor(func(code string) bool {
		return strings.HasPrefix(code, "player_") && (strings.Contains(code, "error") || strings.Contains(code, "unsupported") || strings.Contains(code, "blocked") || strings.Contains(code, "no_stream"))
	})
	add("local_playback", "candidate", "优先核对本机播放器与解码能力",
		"播放器失败或不支持是本地观测，不能据此推断原始流在所有客户端均不可播放。", playerIDs,
		"核对浏览器编码支持、自动播放限制、媒体错误和硬件解码能力，并用另一播放器对照。")

	timingIDs := idsFor(func(code string) bool {
		return strings.Contains(code, "dts") || strings.Contains(code, "timestamp") || strings.Contains(code, "pcr") || strings.Contains(code, "duration_drift")
	})
	add("media_timeline", "candidate", "核对源端媒体时间线与封装切换",
		"时间戳回退、空洞或漂移可能来自编码器、断流重连、流切换和封装转换，需要结合原始事件复核。", timingIDs,
		"对照异常前后的原始时间戳、编码器时钟、断流重连和码流切换记录。")

	switch {
	case len(connectionIDs) > 0:
		result.Conclusion = "优先处理已观测的连接或协议协商失败，再复测媒体接收。"
	case len(noMediaIDs) > 0:
		result.Conclusion = "当前窗口未取得完整媒体，先核对源端输出与媒体传输建立情况。"
	case len(slowIDs) > 0:
		result.Conclusion = "已观测起播偏慢，按连接、关键帧与首次呈帧阶段拆分等待时间。"
	case len(transportIDs) > 0:
		result.Conclusion = "已观测媒体供给或传输连续性异常，需用源端和另一接收点证据区分环节。"
	case len(playerIDs) > 0:
		result.Conclusion = "已观测本机播放异常，优先核对播放器、自动播放与解码能力。"
	case len(timingIDs) > 0:
		result.Conclusion = "已观测媒体时间线异常，优先核对源端编码时钟与封装切换。"
	case len(report.Findings) == 0:
		result.Conclusion = "当前采样窗口未触发规则异常；这不等同于长期可用性或所有客户端播放正常。"
	default:
		result.Conclusion = "已列出本次规则证据，按异常时间段和下一步检查继续验证。"
	}
	if report.Player != nil && report.Player.Source == "independent_pull" {
		result.Limitations = append(result.Limitations, "HLS 独立采样与播放器使用不同连接，采样连接异常不能直接证明播放连接同样异常。")
	}
	return result
}

func importedReportAssessment(dir string, report *Report, entry *zip.File) error {
	if entry == nil {
		return nil
	}
	reader, err := entry.Open()
	if err != nil {
		return err
	}
	defer reader.Close()
	var stored Report
	if err := json.NewDecoder(reader).Decode(&stored); err != nil {
		return fmt.Errorf("%w: 报告 JSON 不可读", ErrInvalid)
	}
	if stored.Assessment != nil {
		if err := validReportAssessment(*stored.Assessment, *report); err != nil {
			return err
		}
		report.Assessment = stored.Assessment
	}
	if err := decodeStoredContentQuality(stored, report); err != nil {
		return err
	}
	throughput, err := validateStoredThroughput(dir, stored, *report)
	if err != nil {
		return err
	}
	report.Throughput = throughput
	control, err := validateStoredRTMPControl(dir, stored, *report)
	if err != nil {
		return err
	}
	report.RTMPControl = control
	return nil
}

func validReportAssessment(assessment ReportAssessment, report Report) error {
	if assessment.Version != reportAssessmentVersion {
		return fmt.Errorf("%w: 整报告分层规则版本无效", ErrInvalid)
	}
	expected := assessReport(report)
	if assessment.Version != expected.Version || assessment.Conclusion != expected.Conclusion ||
		!slices.Equal(assessment.Limitations, expected.Limitations) || !slices.EqualFunc(assessment.Directions, expected.Directions, func(a, b StallDirection) bool {
		return a.Code == b.Code && a.Status == b.Status && a.Title == b.Title && a.Message == b.Message &&
			slices.Equal(a.EvidenceIDs, b.EvidenceIDs) && slices.Equal(a.NextChecks, b.NextChecks)
	}) {
		return fmt.Errorf("%w: 整报告分层判断与规则证据不一致", ErrInvalid)
	}
	return nil
}
