package volcengine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	relaychannel "github.com/QuantumNous/new-api/relay/channel"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Volcengine v3 协议事件码（与 tts_v3_ws.go 共用，但这里只用 SessionFinished 做计费）。
const (
	v3EventSessionFinished int32 = 152
)

// v3 frame header bits — 仅解析 SessionFinished 用，不做完整协议解码。
const (
	v3MsgTypeFullServer = 0b1001 // Full-server response
	v3FlagWithEvent     = 0b0100
)

// realtimeTTSUpstreamURL 是火山 v3 双向流式 TTS 端点。
// 协议帧由客户端按火山官方文档构造（StartConnection / StartSession / TaskRequest /
// FinishSession / FinishConnection），项目仅做透传。
const realtimeTTSUpstreamURL = "wss://openspeech.bytedance.com/api/v3/tts/bidirection"

// realtimeDialTimeout 限制建连阶段的最大耗时；火山实际握手通常在数百 ms 内。
const realtimeDialTimeout = 30 * time.Second

// realtimeUpstreamIdleTimeout 是上游帧到达的最长间隔；若超过此值仍无任何帧，
// 多半上游已挂起，客户端宁可看到 502 也不要协程长期阻塞。
//
// 客户端侧不强加 idle 限制：客户端可能在收完一句音频后保留连接、等待下一句
// 用户输入文本，这段空闲是合理的；只在 ctx 取消时主动断开。
const realtimeUpstreamIdleTimeout = 60 * time.Second

// realtimeWriteTimeout 给每次 WriteMessage 的硬上限，防止某一边的 TCP buffer
// 卡死时把对端 goroutine 也拖住。
const realtimeWriteTimeout = 10 * time.Second

// HandleRealtimeTTSPassthrough 把客户端 WebSocket（已 upgrade）与火山 v3 双向
// 流式 TTS 之间做纯字节透传。
//
// 职责：
//   - 加载渠道的 VolcTTSConfig（resource_id / auth_mode / require_usage）
//   - 用 buildV3Headers 注入 X-Api-Key / X-Api-Resource-Id / X-Api-Connect-Id /
//     X-Control-Require-Usage-Tokens-Return（与现有 tts_v3_ws.go 路径同款逻辑）
//   - 与上游建立 WS 连接
//   - 启两个 goroutine 双向泵帧；任一方向出错或客户端断开都会拉倒另一边
//   - 在上游→客户端方向上识别 Event_SessionFinished (152) 帧，抠出
//     usage.text_words 累加；同一个 WS 连接可能跑多个 session，需累计
//
// 返回的 *dto.Usage 仅 PromptTokens / TotalTokens / PromptTokensDetails.TextTokens
// 三处赋值（=累计 text_words）。火山以"输入字符数"计费，所以把它视作输入文本 token；
// 由调用方决定按哪个倍率结算。
//
// 注意：返回 (*dto.Usage, error) — 即使 error != nil 也可能有部分 usage 累计
// （比如完成了几轮 session 后客户端异常断开），调用方仍应基于 usage 写消费日志。
func HandleRealtimeTTSPassthrough(c *gin.Context, clientWS *websocket.Conn, channel *model.Channel) (*dto.Usage, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}

	otherSettings := channel.GetOtherSettings()
	volcCfg := otherSettings.ResolvedVolcTTS()

	apiKey := strings.TrimSpace(channel.Key)
	if apiKey == "" {
		return nil, errors.New("channel api key is empty")
	}

	connectID := uuid.NewString()
	upstreamHeader, err := buildV3Headers(volcCfg, apiKey, connectID)
	if err != nil {
		return nil, fmt.Errorf("build volcengine v3 headers: %w", err)
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), realtimeDialTimeout)
	defer cancelDial()

	upstreamWS, dialResp, dialErr := websocket.DefaultDialer.DialContext(dialCtx, realtimeTTSUpstreamURL, upstreamHeader)
	if dialErr != nil {
		hint := ""
		statusCode := http.StatusBadGateway
		if dialResp != nil {
			statusCode = dialResp.StatusCode
			if upID := relaychannel.ExtractUpstreamRequestIDFromHeader(dialResp.Header); upID != "" {
				c.Set(common.UpstreamRequestIdKey, upID)
			}
			if logID := dialResp.Header.Get("X-Tt-Logid"); logID != "" {
				hint = fmt.Sprintf(" logid=%s", logID)
			}
		}
		// 通过 close frame 把建连失败的原因传给客户端，方便排错
		writeCloseWithReason(clientWS, websocket.CloseInternalServerErr,
			fmt.Sprintf("upstream dial failed (status=%d): %v%s", statusCode, dialErr, hint))
		return nil, fmt.Errorf("dial volcengine v3: %w%s", dialErr, hint)
	}
	defer upstreamWS.Close()

	// 把上游的 X-Tt-Logid 通过文本帧告知客户端便于排查（火山的 logid 是定位
	// 服务端问题的关键）。客户端可忽略此帧（不是火山协议帧，type=1 文本帧）。
	if dialResp != nil {
		if upID := relaychannel.ExtractUpstreamRequestIDFromHeader(dialResp.Header); upID != "" {
			c.Set(common.UpstreamRequestIdKey, upID)
		}
		if logID := dialResp.Header.Get("X-Tt-Logid"); logID != "" {
			_ = clientWS.WriteMessage(websocket.TextMessage,
				[]byte(`{"_proxy_meta":{"upstream_logid":"`+logID+`"}}`))
		}
	}

	// 全流程取消信号：客户端断开 / 上游断开 / 任一方向出错都触发 cancel
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	// ctx 一旦取消，立刻 close 两端 conn 来打断阻塞中的 ReadMessage。
	// gorilla/websocket 的 ReadMessage 没有原生 ctx 支持，只能靠 close socket 中断。
	go func() {
		<-ctx.Done()
		_ = clientWS.Close()
		_ = upstreamWS.Close()
	}()

	errCh := make(chan error, 2)

	// totalTextWords 在两个 goroutine 中只由 upstream→client pump 写入，
	// 但用 atomic 保证读侧的可见性（连接结束时由调用方读）。
	var totalTextWords int64

	// 上游 → 客户端：对 src 加 idle deadline，超时即视为上游卡住；
	// 同时识别 SessionFinished 帧抠 text_words 累加做计费。
	go func() {
		errCh <- pumpRealtimeFramesWithUsage(upstreamWS, clientWS,
			"upstream→client", realtimeUpstreamIdleTimeout, &totalTextWords)
	}()

	// 客户端 → 上游：不加 idle 限制（用户可能正在思考或停顿），不计费。
	go func() {
		errCh <- pumpRealtimeFrames(clientWS, upstreamWS, "client→upstream", 0)
	}()

	// 任一方向出错或正常结束都退出；让 defer cancel 把另一边带下来。
	firstErr := <-errCh
	cancel()
	// 等第二个 goroutine 退出，避免泄露
	<-errCh

	usage := buildRealtimeTTSUsage(int(atomic.LoadInt64(&totalTextWords)))
	return usage, firstErr
}

// pumpRealtimeFrames 把 src 收到的每条 WS 消息原样转发到 dst（无计费旁路）。
// idleTimeout==0 表示不限制 src 的读超时；客户端侧适用此值。
//
// 返回 nil 表示正常关闭（CloseNormalClosure / CloseGoingAway），其他错误代表异常。
func pumpRealtimeFrames(src, dst *websocket.Conn, label string, idleTimeout time.Duration) error {
	for {
		if idleTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		msgType, payload, err := src.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived) {
				return nil
			}
			return fmt.Errorf("%s read: %w", label, err)
		}

		if err := dst.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout)); err != nil {
			return fmt.Errorf("%s set write deadline: %w", label, err)
		}
		if err := dst.WriteMessage(msgType, payload); err != nil {
			return fmt.Errorf("%s write: %w", label, err)
		}
	}
}

// pumpRealtimeFramesWithUsage 与 pumpRealtimeFrames 行为相同，但额外把每帧的
// 字节按 SessionFinished (152) 检查并把 usage.text_words 累加到 totalTextWords。
//
// 透传不破坏：先写到 dst 再做计费解析（解析失败也不影响转发），保证客户端永远
// 收到完整的、原样的火山下行帧。
func pumpRealtimeFramesWithUsage(src, dst *websocket.Conn, label string,
	idleTimeout time.Duration, totalTextWords *int64) error {
	for {
		if idleTimeout > 0 {
			_ = src.SetReadDeadline(time.Now().Add(idleTimeout))
		}
		msgType, payload, err := src.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err,
				websocket.CloseNormalClosure,
				websocket.CloseGoingAway,
				websocket.CloseNoStatusReceived) {
				return nil
			}
			return fmt.Errorf("%s read: %w", label, err)
		}

		if err := dst.SetWriteDeadline(time.Now().Add(realtimeWriteTimeout)); err != nil {
			return fmt.Errorf("%s set write deadline: %w", label, err)
		}
		if err := dst.WriteMessage(msgType, payload); err != nil {
			return fmt.Errorf("%s write: %w", label, err)
		}

		// 透传完成后做"非破坏式"计费解析：只对二进制帧 + 长度合法的帧解析，
		// 解析失败/不是 SessionFinished 都安静跳过，绝不影响透传链路。
		if msgType == websocket.BinaryMessage {
			if textWords, ok := tryParseSessionFinishedTextWords(payload); ok {
				atomic.AddInt64(totalTextWords, int64(textWords))
			}
		}
	}
}

// tryParseSessionFinishedTextWords 在火山 v3 二进制帧里识别 SessionFinished (152)
// 并解析 payload JSON 里的 usage.text_words。其他帧返回 0, false。
//
// 协议帧格式（仅本函数关心的字段）：
//
//	[0]: header bits（version + size）
//	[1]: msg_type(4) | flags(4)  — 期望 0b1001 (Full-server) | 0b0100 (with event)
//	[2]: serialization | compression  — 不解析
//	[3]: reserved
//	[4-7]: int32 BE event code  — 期望 152
//	[8-11]: uint32 BE session_id length
//	[12 ..]: session_id bytes
//	[..]: uint32 BE payload length + payload JSON
//
// 解析失败一律返回 0, false — 调用方据此安静跳过，不影响透传。
func tryParseSessionFinishedTextWords(data []byte) (int, bool) {
	if len(data) < 12 {
		return 0, false
	}
	msgType := (data[1] >> 4) & 0x0F
	flags := data[1] & 0x0F
	if msgType != v3MsgTypeFullServer || flags&v3FlagWithEvent == 0 {
		return 0, false
	}
	event := int32(binary.BigEndian.Uint32(data[4:8]))
	if event != v3EventSessionFinished {
		return 0, false
	}
	pos := 8
	if pos+4 > len(data) {
		return 0, false
	}
	sidLen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	if pos+sidLen+4 > len(data) {
		return 0, false
	}
	pos += sidLen
	plen := int(binary.BigEndian.Uint32(data[pos : pos+4]))
	pos += 4
	if pos+plen > len(data) {
		return 0, false
	}
	var env v3SessionResultEnvelope
	if err := common.Unmarshal(data[pos:pos+plen], &env); err != nil {
		return 0, false
	}
	if env.Usage == nil {
		// SessionFinished 但没 usage 字段（X-Control-Require-Usage-Tokens-Return
		// 没启用时会发生）。识别成功，但贡献 0 字符。
		return 0, true
	}
	return env.Usage.TextWords, true
}

// buildRealtimeTTSUsage 把累计 text_words 包装成 *dto.Usage，供调用方计费。
// 火山 TTS 按"输入字符数"计费，所以把 text_words 当作 PromptTokens / TextTokens。
func buildRealtimeTTSUsage(totalTextWords int) *dto.Usage {
	if totalTextWords < 0 {
		totalTextWords = 0
	}
	return &dto.Usage{
		PromptTokens:     totalTextWords,
		CompletionTokens: 0,
		TotalTokens:      totalTextWords,
		PromptTokensDetails: dto.InputTokenDetails{
			TextTokens: totalTextWords,
		},
	}
}

// writeCloseWithReason 尽力把 close frame 与人类可读的 reason 一起发给客户端。
// 用于建连失败、协议错误等场景。
func writeCloseWithReason(ws *websocket.Conn, code int, reason string) {
	if ws == nil {
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	msg := websocket.FormatCloseMessage(code, reason)
	_ = ws.WriteControl(websocket.CloseMessage, msg, deadline)
}
