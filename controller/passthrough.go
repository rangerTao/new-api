package controller

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	channelconstant "github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// CustomPassthrough is the handler for ANY /v1/custom/:model/*path.
//
// It bridges arbitrary upstream HTTP services (non-OpenAI protocols) into the
// new-api gateway, providing:
//   - token auth (via the standard middleware chain)
//   - per-channel routing by model name (via Distribute middleware)
//   - per-channel allowed-path-prefix safety check (deny-all by default)
//   - optional channel-key auth header injection (bearer / custom header / basic)
//   - per-call billing using model_price (charged regardless of upstream result —
//     "calling = pay" semantics)
//   - request/response streaming with chunked + SSE support
//
// The handler does NOT use the OpenAI relay framework, since upstream payloads
// can be any shape (binary, multipart, custom JSON).
func CustomPassthrough(c *gin.Context) {
	// ----- 1. RelayInfo bootstrap -----
	info := relaycommon.GenRelayInfoOpenAIAudio(c, nil)
	info.InitChannelMeta(c)

	if info.ChannelMeta == nil || info.ChannelType != channelconstant.ChannelTypePassthrough {
		respondJSONError(c, http.StatusBadRequest, types.ErrorCodeInvalidApiType,
			fmt.Errorf("channel %d is not a Passthrough channel", info.ChannelType))
		return
	}

	cfg := info.ChannelOtherSettings.ResolvedPassthrough()
	subPath := normalizePath(c.Param("path"))
	model := c.Param("model")
	method := c.Request.Method

	// ----- 2. Safety: path + method allowlist -----
	if !cfg.IsPathAllowed(subPath) {
		respondJSONError(c, http.StatusForbidden, types.ErrorCodeAccessDenied,
			fmt.Errorf("path %q is not in allowed_path_prefixes for channel %d", subPath, info.ChannelId))
		return
	}
	if !cfg.IsMethodAllowed(method) {
		respondJSONError(c, http.StatusMethodNotAllowed, types.ErrorCodeAccessDenied,
			fmt.Errorf("method %s is not allowed by passthrough channel %d", method, info.ChannelId))
		return
	}

	// ----- 3. Billing: per-call model price -----
	priceData, err := helper.ModelPriceHelperPerCall(c, info)
	if err != nil {
		respondJSONError(c, http.StatusBadRequest, types.ErrorCodeBadRequestBody,
			fmt.Errorf("model_price lookup failed for %q: %w", model, err))
		return
	}
	info.PriceData = priceData

	if !priceData.FreeModel {
		info.ForcePreConsume = true
		if apiErr := service.PreConsumeBilling(c, priceData.Quota, info); apiErr != nil {
			respondAPIError(c, apiErr)
			return
		}
	}

	// "Calling = pay": settle the full pre-consumed amount on EVERY exit path,
	// success or failure. Defer guarantees a single settlement even on panic.
	settled := false
	settleOnce := func(actualQuota int) {
		if settled {
			return
		}
		settled = true
		if priceData.FreeModel {
			return
		}
		if err := service.SettleBilling(c, info, actualQuota); err != nil {
			logger.LogError(c, fmt.Sprintf("passthrough billing settle failed: %v", err))
		}
	}
	defer func() {
		// Guarantees billing on panic; normal paths call settleOnce explicitly.
		settleOnce(priceData.Quota)
	}()

	// ----- 4. Build upstream request -----
	upstreamBase := strings.TrimRight(info.ChannelBaseUrl, "/")
	if upstreamBase == "" {
		respondJSONError(c, http.StatusBadGateway, types.ErrorCodeBadResponse,
			fmt.Errorf("passthrough channel %d has empty base_url", info.ChannelId))
		return
	}
	upstreamURL := upstreamBase + subPath
	if rawQuery := c.Request.URL.RawQuery; rawQuery != "" {
		upstreamURL += "?" + rawQuery
	}

	ctx := c.Request.Context()
	req, err := http.NewRequestWithContext(ctx, method, upstreamURL, c.Request.Body)
	if err != nil {
		respondJSONError(c, http.StatusInternalServerError, types.ErrorCodeBadRequestBody,
			fmt.Errorf("build upstream request failed: %w", err))
		return
	}

	copyClientHeaders(c.Request.Header, req.Header)
	if err := injectPassthroughAuth(req.Header, cfg.Auth, info.ApiKey); err != nil {
		respondJSONError(c, http.StatusInternalServerError, types.ErrorCodeChannelInvalidKey, err)
		return
	}

	// ----- 5. Issue upstream request -----
	client := http.DefaultClient
	if cfg.TimeoutMs > 0 {
		client = &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond}
	}

	upstreamStart := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		respondJSONError(c, http.StatusBadGateway, types.ErrorCodeBadResponse,
			fmt.Errorf("passthrough upstream call failed: %w", err))
		return
	}
	defer resp.Body.Close()

	upstreamLatencyMs := time.Since(upstreamStart).Milliseconds()
	c.Set("passthrough_upstream_status", resp.StatusCode)
	c.Set("passthrough_upstream_latency_ms", upstreamLatencyMs)
	c.Set("passthrough_route", model+":"+subPath)

	// ----- 6. Forward response (streaming-aware) -----
	forwardUpstreamResponse(c, resp)

	// Log per-call billing audit (kept minimal to avoid bloating the logs).
	logger.LogInfo(c, fmt.Sprintf(
		"passthrough billed: model=%s channel=%d quota=%d upstream=%d latency_ms=%d path=%s",
		model, info.ChannelId, priceData.Quota, resp.StatusCode, upstreamLatencyMs, subPath,
	))
	settleOnce(priceData.Quota)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// hopByHopHeaders are connection-level headers that MUST NOT be forwarded.
// Reference: RFC 7230 §6.1.
var hopByHopHeaders = map[string]struct{}{
	"Connection":          {},
	"Proxy-Connection":    {},
	"Keep-Alive":          {},
	"Proxy-Authenticate":  {},
	"Proxy-Authorization": {},
	"Te":                  {},
	"Trailer":             {},
	"Transfer-Encoding":   {},
	"Upgrade":             {},
}

// downstreamOnlyHeaders are request headers that belong to the new-api boundary
// and must not leak to the upstream service.
var downstreamOnlyHeaders = map[string]struct{}{
	"Authorization":       {}, // replaced by injectPassthroughAuth
	"Cookie":              {},
	"X-Forwarded-For":     {},
	"X-Forwarded-Host":    {},
	"X-Forwarded-Proto":   {},
	"X-Real-Ip":           {},
}

func copyClientHeaders(src, dst http.Header) {
	for key, values := range src {
		canonical := http.CanonicalHeaderKey(key)
		if _, hop := hopByHopHeaders[canonical]; hop {
			continue
		}
		if _, internal := downstreamOnlyHeaders[canonical]; internal {
			continue
		}
		for _, v := range values {
			dst.Add(canonical, v)
		}
	}
}

func injectPassthroughAuth(header http.Header, auth *dto.PassthroughAuth, channelKey string) error {
	if auth == nil {
		return nil
	}
	switch auth.Type {
	case dto.PassthroughAuthNone, "none":
		return nil
	case dto.PassthroughAuthBearer:
		if channelKey == "" {
			return fmt.Errorf("passthrough channel key is empty for bearer auth")
		}
		header.Set("Authorization", "Bearer "+channelKey)
	case dto.PassthroughAuthHeader:
		if auth.HeaderName == "" {
			return fmt.Errorf("passthrough auth.header_name is empty for header auth")
		}
		if channelKey == "" {
			return fmt.Errorf("passthrough channel key is empty for header auth")
		}
		header.Set(auth.HeaderName, channelKey)
	case dto.PassthroughAuthBasic:
		if channelKey == "" {
			return fmt.Errorf("passthrough channel key is empty for basic auth")
		}
		header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(channelKey)))
	default:
		return fmt.Errorf("unknown passthrough auth.type %q", auth.Type)
	}
	return nil
}

// forwardUpstreamResponse copies status, headers, and body from resp into c.
// It auto-flushes when the upstream uses chunked encoding or SSE so streaming
// responses propagate to the client without buffering.
func forwardUpstreamResponse(c *gin.Context, resp *http.Response) {
	dst := c.Writer.Header()
	for key, values := range resp.Header {
		canonical := http.CanonicalHeaderKey(key)
		if _, hop := hopByHopHeaders[canonical]; hop {
			continue
		}
		dst[canonical] = values
	}
	c.Status(resp.StatusCode)

	if isStreamingResponse(resp) {
		flusher, _ := c.Writer.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := c.Writer.Write(buf[:n]); werr != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if readErr != nil {
				return
			}
		}
	}
	// Non-streaming path — single copy.
	_, _ = io.Copy(c.Writer, resp.Body)
}

func isStreamingResponse(resp *http.Response) bool {
	for _, te := range resp.TransferEncoding {
		if strings.EqualFold(te, "chunked") {
			return true
		}
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.HasPrefix(ct, "text/event-stream") {
		return true
	}
	if resp.ContentLength < 0 {
		// Server didn't declare Content-Length — treat as streaming.
		return true
	}
	return false
}

// normalizePath ensures the captured *path always starts with "/" so that
// allowed-prefix checks behave predictably even when gin yields "" for an
// empty catch-all match.
func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

func respondJSONError(c *gin.Context, status int, code types.ErrorCode, err error) {
	logger.LogError(c, fmt.Sprintf("passthrough error: %v", err))
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"code":    string(code),
			"message": err.Error(),
		},
	})
}

func respondAPIError(c *gin.Context, apiErr *types.NewAPIError) {
	logger.LogError(c, fmt.Sprintf("passthrough api error: %v", apiErr))
	status := http.StatusInternalServerError
	if apiErr.StatusCode > 0 {
		status = apiErr.StatusCode
	}
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"code":    string(apiErr.GetErrorCode()),
			"message": apiErr.Error(),
		},
	})
}

// Compile-time: bytes import kept for future request-body capture (audit logs).
var _ = bytes.NewReader
