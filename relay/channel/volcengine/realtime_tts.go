package volcengine

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/model"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
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
// 阶段一职责：
//   - 加载渠道的 VolcTTSConfig（resource_id / auth_mode / require_usage）
//   - 用 buildV3Headers 注入 X-Api-Key / X-Api-Resource-Id / X-Api-Connect-Id /
//     X-Control-Require-Usage-Tokens-Return（与现有 tts_v3_ws.go 路径同款逻辑）
//   - 与上游建立 WS 连接
//   - 启两个 goroutine 双向泵帧；任一方向出错或客户端断开都会拉倒另一边
//
// 阶段二将在这里增加：解析下行 Event_SessionFinished (152) 帧抠 usage.text_words
// 累加做计费 + 写 RecordConsumeLog。
func HandleRealtimeTTSPassthrough(c *gin.Context, clientWS *websocket.Conn, channel *model.Channel) error {
	if channel == nil {
		return errors.New("channel is nil")
	}

	otherSettings := channel.GetOtherSettings()
	volcCfg := otherSettings.ResolvedVolcTTS()

	apiKey := strings.TrimSpace(channel.Key)
	if apiKey == "" {
		return errors.New("channel api key is empty")
	}

	connectID := uuid.NewString()
	upstreamHeader, err := buildV3Headers(volcCfg, apiKey, connectID)
	if err != nil {
		return fmt.Errorf("build volcengine v3 headers: %w", err)
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), realtimeDialTimeout)
	defer cancelDial()

	upstreamWS, dialResp, dialErr := websocket.DefaultDialer.DialContext(dialCtx, realtimeTTSUpstreamURL, upstreamHeader)
	if dialErr != nil {
		hint := ""
		statusCode := http.StatusBadGateway
		if dialResp != nil {
			statusCode = dialResp.StatusCode
			if logID := dialResp.Header.Get("X-Tt-Logid"); logID != "" {
				hint = fmt.Sprintf(" logid=%s", logID)
			}
		}
		// 通过 close frame 把建连失败的原因传给客户端，方便排错
		writeCloseWithReason(clientWS, websocket.CloseInternalServerErr,
			fmt.Sprintf("upstream dial failed (status=%d): %v%s", statusCode, dialErr, hint))
		return fmt.Errorf("dial volcengine v3: %w%s", dialErr, hint)
	}
	defer upstreamWS.Close()

	// 把上游的 X-Tt-Logid 通过文本帧告知客户端便于排查（火山的 logid 是定位
	// 服务端问题的关键）。客户端可忽略此帧（不是火山协议帧，type=1 文本帧）。
	if dialResp != nil {
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

	// 上游 → 客户端：对 src 加 idle deadline，超时即视为上游卡住。
	go func() {
		errCh <- pumpRealtimeFrames(upstreamWS, clientWS, "upstream→client", realtimeUpstreamIdleTimeout)
	}()

	// 客户端 → 上游：不加 idle 限制（用户可能正在思考或停顿）。
	go func() {
		errCh <- pumpRealtimeFrames(clientWS, upstreamWS, "client→upstream", 0)
	}()

	// 任一方向出错或正常结束都退出；让 defer cancel 把另一边带下来。
	firstErr := <-errCh
	cancel()
	// 等第二个 goroutine 退出，避免泄露
	<-errCh
	return firstErr
}

// pumpRealtimeFrames 把 src 收到的每条 WS 消息原样转发到 dst。
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
