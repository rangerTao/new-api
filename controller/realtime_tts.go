package controller

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/volcengine"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// RelayVolcRealtimeTTS 处理客户端通过 WebSocket 双向流式调用火山 TTS v3 的请求。
//
// 流程：
//  1. 由 TokenAuth + ModelRequestRateLimit + Distribute 中间件链完成鉴权与渠道选择
//     （已经把 channel_id 等写入 context）
//  2. 校验渠道类型是否为火山引擎；非火山直接拒绝（避免协议不匹配）
//  3. 升级 HTTP 连接为 WebSocket
//  4. 拉起 volcengine.HandleRealtimeTTSPassthrough 完成上游拨号 + 双向帧泵
//
// 阶段一：透传链路 + 鉴权 + 错误处理；不含计费（计费在阶段二补充）。
func RelayVolcRealtimeTTS(c *gin.Context) {
	requestId := c.GetString(common.RequestIdKey)

	channelType := c.GetInt("channel_type")
	if channelType != constant.ChannelTypeVolcEngine {
		// 这条路径仅支持火山引擎渠道；其他类型即使被路由到这里也直接拒绝。
		writeRealtimeBadRequest(c, fmt.Errorf("/v1/audio/realtime requires a Volcengine (type 45) channel, got channel_type=%d", channelType))
		return
	}

	channelID := c.GetInt("channel_id")
	if channelID == 0 {
		writeRealtimeBadRequest(c, errors.New("no channel selected"))
		return
	}

	// 拉取完整 channel 实体（包含 Key / Settings / OtherSettings 等敏感字段，
	// middleware.Distribute 设置的 context 仅含元信息）。
	channel, err := model.GetChannelById(channelID, true)
	if err != nil || channel == nil {
		writeRealtimeBadRequest(c, fmt.Errorf("get channel %d failed: %v", channelID, err))
		return
	}

	ws, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		// upgrader 失败时 c.Writer 已经被部分写入，此时不能再写 JSON 响应。
		// 仅记日志，客户端会看到 HTTP 4xx / 连接断开。
		logger.LogError(c, fmt.Sprintf("realtime tts ws upgrade failed: %s [reqId=%s]", err.Error(), requestId))
		return
	}
	defer ws.Close()

	// 双向透传到火山 v3 双向流式 TTS（阶段一：不计费）。
	if relayErr := volcengine.HandleRealtimeTTSPassthrough(c, ws, channel); relayErr != nil {
		logger.LogError(c, fmt.Sprintf("realtime tts passthrough error: %s [reqId=%s]", relayErr.Error(), requestId))
	}
}

// writeRealtimeBadRequest 在 WebSocket upgrade 之前以 JSON 返回错误。
func writeRealtimeBadRequest(c *gin.Context, err error) {
	apiErr := types.NewError(err, types.ErrorCodeInvalidRequest)
	c.JSON(http.StatusBadRequest, gin.H{"error": apiErr.ToOpenAIError()})
}
