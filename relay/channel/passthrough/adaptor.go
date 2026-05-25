// Package passthrough provides a stub Adaptor for ChannelTypePassthrough.
//
// Passthrough channels do NOT flow through the standard relay framework — the
// gateway handles them in controller.CustomPassthrough, which speaks raw HTTP
// to the configured upstream service. This stub exists only so that:
//
//   - relay.GetAdaptor(APITypePassthrough) returns a non-nil value (controller
//     init() iterates 0..APITypeDummy and crashes if any slot is nil),
//   - GetChannelName / GetModelList provide sensible labels for the model list
//     UI without leaking framework-specific behavior,
//   - any accidental call into a relay-style method returns a clear error
//     instead of segfaulting.
package passthrough

import (
	"errors"
	"io"
	"net/http"

	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

const ChannelName = "Passthrough"

// ModelList is intentionally empty: passthrough channels register their own
// model names per-channel via the admin UI; there is no globally meaningful
// list.
var ModelList = []string{}

type Adaptor struct{}

var ErrUnsupported = errors.New("passthrough channels are handled by controller.CustomPassthrough; the OpenAI relay framework is not invoked")

func (a *Adaptor) Init(info *relaycommon.RelayInfo) {}

func (a *Adaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return "", ErrUnsupported
}

func (a *Adaptor) SetupRequestHeader(c *gin.Context, req *http.Header, info *relaycommon.RelayInfo) error {
	return ErrUnsupported
}

func (a *Adaptor) ConvertOpenAIRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeneralOpenAIRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertRerankRequest(c *gin.Context, relayMode int, request dto.RerankRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertEmbeddingRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.EmbeddingRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertAudioRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.AudioRequest) (io.Reader, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertImageRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertOpenAIResponsesRequest(c *gin.Context, info *relaycommon.RelayInfo, request dto.OpenAIResponsesRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertClaudeRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ClaudeRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) ConvertGeminiRequest(c *gin.Context, info *relaycommon.RelayInfo, request *dto.GeminiChatRequest) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, requestBody io.Reader) (any, error) {
	return nil, ErrUnsupported
}

func (a *Adaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usage any, err *types.NewAPIError) {
	return nil, types.NewError(ErrUnsupported, types.ErrorCodeInvalidApiType)
}

func (a *Adaptor) GetModelList() []string {
	return ModelList
}

func (a *Adaptor) GetChannelName() string {
	return ChannelName
}
