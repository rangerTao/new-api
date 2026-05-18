package dto

import "strings"

type ChannelSettings struct {
	ForceFormat            bool   `json:"force_format,omitempty"`
	ThinkingToContent      bool   `json:"thinking_to_content,omitempty"`
	Proxy                  string `json:"proxy"`
	PassThroughBodyEnabled bool   `json:"pass_through_body_enabled,omitempty"`
	SystemPrompt           string `json:"system_prompt,omitempty"`
	SystemPromptOverride   bool   `json:"system_prompt_override,omitempty"`
}

type VertexKeyType string

const (
	VertexKeyTypeJSON   VertexKeyType = "json"
	VertexKeyTypeAPIKey VertexKeyType = "api_key"
)

type AwsKeyType string

const (
	AwsKeyTypeAKSK   AwsKeyType = "ak_sk" // 默认
	AwsKeyTypeApiKey AwsKeyType = "api_key"
)

type ChannelOtherSettings struct {
	AzureResponsesVersion                 string        `json:"azure_responses_version,omitempty"`
	VertexKeyType                         VertexKeyType `json:"vertex_key_type,omitempty"` // "json" or "api_key"
	OpenRouterEnterprise                  *bool         `json:"openrouter_enterprise,omitempty"`
	ClaudeBetaQuery                       bool          `json:"claude_beta_query,omitempty"`         // Claude 渠道是否强制追加 ?beta=true
	AllowServiceTier                      bool          `json:"allow_service_tier,omitempty"`        // 是否允许 service_tier 透传（默认过滤以避免额外计费）
	AllowInferenceGeo                     bool          `json:"allow_inference_geo,omitempty"`       // 是否允许 inference_geo 透传（仅 Claude，默认过滤以满足数据驻留合规
	AllowSpeed                            bool          `json:"allow_speed,omitempty"`               // 是否允许 speed 透传（仅 Claude，默认过滤以避免意外切换推理速度模式）
	AllowSafetyIdentifier                 bool          `json:"allow_safety_identifier,omitempty"`   // 是否允许 safety_identifier 透传（默认过滤以保护用户隐私）
	DisableStore                          bool          `json:"disable_store,omitempty"`             // 是否禁用 store 透传（默认允许透传，禁用后可能导致 Codex 无法使用）
	AllowIncludeObfuscation               bool          `json:"allow_include_obfuscation,omitempty"` // 是否允许 stream_options.include_obfuscation 透传（默认过滤以避免关闭流混淆保护）
	AwsKeyType                            AwsKeyType    `json:"aws_key_type,omitempty"`
	UpstreamModelUpdateCheckEnabled       bool          `json:"upstream_model_update_check_enabled,omitempty"`        // 是否检测上游模型更新
	UpstreamModelUpdateAutoSyncEnabled    bool          `json:"upstream_model_update_auto_sync_enabled,omitempty"`    // 是否自动同步上游模型更新
	UpstreamModelUpdateLastCheckTime      int64         `json:"upstream_model_update_last_check_time,omitempty"`      // 上次检测时间
	UpstreamModelUpdateLastDetectedModels []string      `json:"upstream_model_update_last_detected_models,omitempty"` // 上次检测到的可加入模型
	UpstreamModelUpdateLastRemovedModels  []string      `json:"upstream_model_update_last_removed_models,omitempty"`  // 上次检测到的可删除模型
	UpstreamModelUpdateIgnoredModels      []string      `json:"upstream_model_update_ignored_models,omitempty"`       // 手动忽略的模型

	// Passthrough configures behaviour for ChannelTypePassthrough (generic
	// HTTP forwarder). nil/empty when not a passthrough channel.
	Passthrough *PassthroughConfig `json:"passthrough,omitempty"`
}

// PassthroughConfig — per-channel settings for the generic HTTP passthrough
// channel type (ChannelTypePassthrough). The upstream URL itself is taken from
// Channel.BaseURL; this struct tunes forwarding semantics + auth + safety.
type PassthroughConfig struct {
	// AllowedPathPrefixes lists the URL path prefixes that requests may target
	// on the upstream. Anything not matching one of these prefixes will be
	// rejected with 403. Empty / nil means "deny all" — the operator MUST
	// explicitly opt in to specific prefixes.
	AllowedPathPrefixes []string `json:"allowed_path_prefixes,omitempty"`

	// Auth selects how Channel.Key is propagated to the upstream. nil ==
	// no auth (suitable for internal / IP-allowlisted services).
	Auth *PassthroughAuth `json:"auth,omitempty"`

	// AllowedMethods optionally restricts which HTTP methods may be forwarded.
	// Empty means all methods allowed.
	AllowedMethods []string `json:"allowed_methods,omitempty"`

	// TimeoutMs caps the upstream request timeout. 0 means "no timeout"
	// (still bounded by client request context).
	TimeoutMs int `json:"timeout_ms,omitempty"`
}

// PassthroughAuth describes how to attach the channel key to upstream requests.
type PassthroughAuth struct {
	// Type: "" or "none" → no auth header injected
	//       "bearer"     → Authorization: Bearer <Channel.Key>
	//       "header"     → <HeaderName>: <Channel.Key>
	//       "basic"      → Authorization: Basic base64(<Channel.Key>) — Channel.Key must already be "user:pass"
	Type string `json:"type,omitempty"`
	// HeaderName is required when Type=="header".
	HeaderName string `json:"header_name,omitempty"`
}

// PassthroughAuthType* are the canonical Type values.
const (
	PassthroughAuthNone   = ""
	PassthroughAuthBearer = "bearer"
	PassthroughAuthHeader = "header"
	PassthroughAuthBasic  = "basic"
)

// ResolvedPassthrough returns a non-nil PassthroughConfig (defaulted to a
// deny-all config when unset) so call sites can read fields safely.
func (s *ChannelOtherSettings) ResolvedPassthrough() PassthroughConfig {
	if s == nil || s.Passthrough == nil {
		return PassthroughConfig{}
	}
	return *s.Passthrough
}

// IsPathAllowed reports whether subPath (a "/..."-prefixed string) matches at
// least one configured prefix. An empty prefix list always denies — fail-safe
// default to prevent accidental open-proxy exposure.
func (p PassthroughConfig) IsPathAllowed(subPath string) bool {
	if subPath == "" {
		return false
	}
	for _, prefix := range p.AllowedPathPrefixes {
		if prefix == "" {
			continue
		}
		if strings.HasPrefix(subPath, prefix) {
			return true
		}
	}
	return false
}

// IsMethodAllowed reports whether HTTP method is permitted. Empty AllowedMethods
// means "all methods OK".
func (p PassthroughConfig) IsMethodAllowed(method string) bool {
	if len(p.AllowedMethods) == 0 {
		return true
	}
	method = strings.ToUpper(method)
	for _, m := range p.AllowedMethods {
		if strings.ToUpper(m) == method {
			return true
		}
	}
	return false
}

func (s *ChannelOtherSettings) IsOpenRouterEnterprise() bool {
	if s == nil || s.OpenRouterEnterprise == nil {
		return false
	}
	return *s.OpenRouterEnterprise
}
