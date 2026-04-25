package otel

import (
	"github.com/QuantumNous/new-api/constant"
)

// ChannelTypeToProvider maps New-API's internal channel type IDs to
// OpenTelemetry gen_ai.system semantic convention values.
// Reference: https://opentelemetry.io/docs/specs/semconv/gen-ai/
func ChannelTypeToProvider(channelType int) string {
	switch channelType {
	case constant.ChannelTypeOpenAI, constant.ChannelTypeOpenAIMax, constant.ChannelTypeOpenRouter, constant.ChannelTypeXinference:
		return "openai"
	case constant.ChannelTypeAzure:
		return "azure_openai"
	case constant.ChannelTypeAnthropic, constant.ChannelTypeMoonshot:
		return "anthropic"
	case constant.ChannelTypeGemini, constant.ChannelTypeVertexAi:
		return "gcp_vertex_ai"
	case constant.ChannelTypeAws:
		return "aws_bedrock"
	case constant.ChannelTypeOllama:
		return "ollama"
	case constant.ChannelTypeBaidu, constant.ChannelTypeBaiduV2:
		return "baidu"
	case constant.ChannelTypeAli:
		return "alibaba_cloud"
	case constant.ChannelTypeZhipu, constant.ChannelTypeZhipu_v4:
		return "zhipu"
	case constant.ChannelTypeXunfei:
		return "xunfei"
	case constant.ChannelTypeTencent:
		return "tencent"
	case constant.ChannelTypeDeepSeek:
		return "deepseek"
	case constant.ChannelTypeMistral:
		return "mistral"
	case constant.ChannelTypeCohere:
		return "cohere"
	case constant.ChannelTypePerplexity:
		return "perplexity"
	case constant.ChannelTypeXai:
		return "xai"
	case constant.ChannelTypeMiniMax:
		return "minimax"
	case constant.ChannelTypeVolcEngine:
		return "volcengine"
	case constant.ChannelTypeSiliconFlow:
		return "siliconflow"
	case constant.ChannelCloudflare:
		return "cloudflare"
	case constant.ChannelTypeCoze:
		return "coze"
	case constant.ChannelTypeDify:
		return "dify"
	case constant.ChannelTypeCodex:
		return "openai"
	case constant.ChannelTypeMokaAI:
		return "mokaai"
	default:
		return "unknown"
	}
}

// RelayFormatToOperation maps relay format to gen_ai.operation.name.
func RelayFormatToOperation(relayFormat string) string {
	switch relayFormat {
	case "openai", "claude", "gemini":
		return "chat"
	case "embedding":
		return "embedding"
	case "image":
		return "image"
	case "audio":
		return "audio"
	case "rerank":
		return "rerank"
	case "responses", "responses_compaction":
		return "chat"
	default:
		return "unknown"
	}
}
