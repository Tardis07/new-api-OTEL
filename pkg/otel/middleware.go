package otel

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"

	"github.com/gin-gonic/gin"
	sdkotel "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const ctxKeyOTELSpan = "otel_span"
const maxBodyCaptureSize = 4096

func RelayMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !enabled {
			c.Next()
			return
		}

		tracer := sdkotel.Tracer("new-api/relay")
		start := time.Now()

		spanName := fmt.Sprintf("%s %s", c.Request.Method, c.FullPath())
		if spanName == " " {
			spanName = fmt.Sprintf("%s %s", c.Request.Method, c.Request.URL.Path)
		}

		ctx, span := tracer.Start(c.Request.Context(), spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				attribute.String("http.request.method", c.Request.Method),
				attribute.String("url.full", c.Request.URL.String()),
				attribute.String("http.route", c.FullPath()),
				attribute.String("user_agent.original", c.Request.UserAgent()),
				attribute.String("newapi.request_id", c.GetString(common.RequestIdKey)),
			),
		)
		defer span.End()

		c.Set(ctxKeyOTELSpan, span)
		c.Request = c.Request.WithContext(ctx)

		RecordClientRequestHeaders(c)
		c.Next()
		RecordClientRequestBody(c)
		RecordGatewayResponseHeaders(c)

		statusCode := c.Writer.Status()
		elapsed := time.Since(start)
		channelType := c.GetInt(string(constant.ContextKeyChannelType))
		channelId := c.GetInt(string(constant.ContextKeyChannelId))
		modelName := c.GetString(string(constant.ContextKeyOriginalModel))
		provider := ChannelTypeToProvider(channelType)
		isStream := c.GetBool(string(constant.ContextKeyIsStream))

		span.SetAttributes(
			attribute.Int("http.response.status_code", statusCode),
			attribute.Float64("http.server.request.duration_ms", float64(elapsed.Milliseconds())),
			attribute.String("gen_ai.request.model", modelName),
			attribute.String("gen_ai.provider.name", provider),
			attribute.Bool("gen_ai.request.stream", isStream),
			attribute.Int("newapi.channel.id", channelId),
			attribute.Int("newapi.channel.type", channelType),
		)

		if statusCode >= 400 {
			span.SetStatus(codes.Error, http.StatusText(statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}

		metricAttrs := metric.WithAttributes(
			attribute.String("http.request.method", c.Request.Method),
			attribute.Int("http.response.status_code", statusCode),
			attribute.String("gen_ai.request.model", modelName),
			attribute.String("gen_ai.provider.name", provider),
			attribute.Bool("gen_ai.request.stream", isStream),
		)
		if RequestCounter != nil {
			RequestCounter.Add(ctx, 1, metricAttrs)
		}
		if RequestDuration != nil {
			RequestDuration.Record(ctx, float64(elapsed.Milliseconds()), metricAttrs)
		}
	}
}

func RecordTokenUsage(c *gin.Context, inTokens, outTokens int) {
	if !enabled {
		return
	}
	ctx := c.Request.Context()
	modelName := c.GetString(string(constant.ContextKeyOriginalModel))
	channelType := c.GetInt(string(constant.ContextKeyChannelType))
	provider := ChannelTypeToProvider(channelType)
	isStream := c.GetBool(string(constant.ContextKeyIsStream))

	attrs := metric.WithAttributes(
		attribute.String("gen_ai.request.model", modelName),
		attribute.String("gen_ai.provider.name", provider),
		attribute.Bool("gen_ai.request.stream", isStream),
	)
	if InputTokens != nil {
		InputTokens.Record(ctx, int64(inTokens), attrs)
	}
	if OutputTokens != nil {
		OutputTokens.Record(ctx, int64(outTokens), attrs)
	}
	if TotalTokens != nil {
		TotalTokens.Record(ctx, int64(inTokens+outTokens), attrs)
	}

	addSpanAttributes(c,
		attribute.Int("gen_ai.usage.input_tokens", inTokens),
		attribute.Int("gen_ai.usage.output_tokens", outTokens),
		attribute.Int("gen_ai.usage.total_tokens", inTokens+outTokens),
	)
}

func RecordDetailedTokens(c *gin.Context, cached, reasoning, audio, image int) {
	if !enabled {
		return
	}
	addSpanAttributes(c,
		attribute.Int("gen_ai.usage.cache_read.input_tokens", cached),
		attribute.Int("gen_ai.usage.reasoning_tokens", reasoning),
		attribute.Int("gen_ai.usage.audio_tokens", audio),
		attribute.Int("gen_ai.usage.image_tokens", image),
	)
}

func RecordResponseData(c *gin.Context, responseId, model, systemFingerprint, finishReason string) {
	if !enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.response.id", responseId),
		attribute.String("gen_ai.response.model", model),
		attribute.String("gen_ai.response.finish_reasons", finishReason),
	}
	if systemFingerprint != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.system_fingerprint", systemFingerprint))
	}
	addSpanAttributes(c, attrs...)
}

func RecordStreamEnd(c *gin.Context, chunkCount int, endReason string, ttftMs float64) {
	if !enabled {
		return
	}
	addSpanAttributes(c,
		attribute.Int("newapi.stream.chunk_count", chunkCount),
		attribute.String("newapi.stream.end_reason", endReason),
		attribute.Float64("gen_ai.response.time_to_first_chunk_ms", ttftMs),
	)
	AddEvent(c, "newapi.stream.end",
		attribute.Int("chunk_count", chunkCount),
		attribute.String("end_reason", endReason),
		attribute.Float64("ttft_ms", ttftMs),
	)
	if TimeToFirstToken != nil && ttftMs > 0 {
		TimeToFirstToken.Record(c.Request.Context(), ttftMs, metric.WithAttributes(
			attribute.String("gen_ai.request.model", c.GetString(string(constant.ContextKeyOriginalModel))),
			attribute.String("gen_ai.provider.name", ChannelTypeToProvider(c.GetInt(string(constant.ContextKeyChannelType)))),
		))
	}
}

func RecordRelayContext(c *gin.Context, relayFormat, relayMode, upstreamModel, originalModel string) {
	if !enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("gen_ai.operation.name", relayFormat),
		attribute.String("newapi.relay.mode", relayMode),
		attribute.String("gen_ai.request.model", originalModel),
	}
	if upstreamModel != "" && upstreamModel != originalModel {
		attrs = append(attrs, attribute.String("newapi.relay.upstream_model", upstreamModel))
	}
	addSpanAttributes(c, attrs...)
}

func RecordRetryInfo(c *gin.Context, retryIndex int, channelsTried []string, lastErr string) {
	if !enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Int("newapi.retry.index", retryIndex),
		attribute.Int("newapi.retry.total_channels_tried", len(channelsTried)),
	}
	if len(channelsTried) > 0 {
		attrs = append(attrs, attribute.String("newapi.retry.channels", strings.Join(channelsTried, " → ")))
	}
	if lastErr != "" {
		attrs = append(attrs, attribute.String("newapi.retry.last_error", lastErr))
	}
	addSpanAttributes(c, attrs...)
}

func RecordError(c *gin.Context, errCode, errType string, statusCode int, isChannelErr, isSkipRetry bool, errMsg string) {
	if !enabled {
		return
	}
	addSpanAttributes(c,
		attribute.String("error.type", errType),
		attribute.String("newapi.error.code", errCode),
		attribute.Int("http.response.status_code", statusCode),
		attribute.Bool("newapi.error.is_channel_error", isChannelErr),
		attribute.Bool("newapi.error.skip_retry", isSkipRetry),
		attribute.String("error.message", errMsg),
	)
	AddEvent(c, "newapi.error",
		attribute.String("error.code", errCode),
		attribute.String("error.type", errType),
		attribute.Int("error.status_code", statusCode),
		attribute.String("error.message", errMsg),
	)
}

func RecordBillingInfo(c *gin.Context, preConsumed int, modelPrice, groupRatio, modelRatio float64, usePrice bool) {
	if !enabled {
		return
	}
	addSpanAttributes(c,
		attribute.Int("newapi.billing.pre_consumed_quota", preConsumed),
		attribute.Float64("newapi.billing.model_price", modelPrice),
		attribute.Float64("newapi.billing.group_ratio", groupRatio),
		attribute.Float64("newapi.billing.model_ratio", modelRatio),
		attribute.Bool("newapi.billing.use_price", usePrice),
	)
}

func RecordUserContext(c *gin.Context, userId int, userGroup string, tokenId int, tokenUnlimited bool) {
	if !enabled {
		return
	}
	addSpanAttributes(c,
		attribute.Int("newapi.user.id", userId),
		attribute.String("newapi.user.group", userGroup),
		attribute.Int("newapi.token.id", tokenId),
		attribute.Bool("newapi.token.unlimited", tokenUnlimited),
	)
}

func EmitStreamChunkEvent(c *gin.Context, chunkIndex int) {
	if !enabled {
		return
	}
	AddEvent(c, fmt.Sprintf("newapi.stream.chunk #%d", chunkIndex),
		attribute.Int("chunk_index", chunkIndex),
	)
}

func EmitStreamChunkEventWithData(c *gin.Context, chunkIndex int, rawData string) {
	if !enabled {
		return
	}

	attrs := []attribute.KeyValue{
		attribute.Int("chunk_index", chunkIndex),
	}

	if parsed := parseOpenAIChunk(rawData); parsed != nil {
		attrs = append(attrs, parsed...)
	} else if parsed := parseClaudeChunk(rawData); parsed != nil {
		attrs = append(attrs, parsed...)
	} else if parsed := parseGeminiChunk(rawData); parsed != nil {
		attrs = append(attrs, parsed...)
	}

	attrs = append(attrs, attribute.String("newapi.stream.chunk_raw", truncateStr(rawData, 1000)))

	AddEvent(c, fmt.Sprintf("newapi.stream.chunk #%d", chunkIndex), attrs...)
}

func parseOpenAIChunk(rawData string) []attribute.KeyValue {
	var chunk struct {
		Choices []struct {
			Delta struct {
				Content    *string `json:"content"`
				Reasoning  *string `json:"reasoning_content"`
				Reasoning2 *string `json:"reasoning"`
				Role       string  `json:"role"`
				ToolCalls  []struct {
					Index    *int   `json:"index"`
					ID       string `json:"id"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		} `json:"choices"`
		Usage *struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}

	if err := common.Unmarshal([]byte(rawData), &chunk); err != nil {
		return nil
	}
	if len(chunk.Choices) == 0 && chunk.Usage == nil && chunk.Model == "" {
		return nil
	}

	var attrs []attribute.KeyValue

	if len(chunk.Choices) > 0 {
		delta := chunk.Choices[0].Delta

		if delta.Content != nil && *delta.Content != "" {
			attrs = append(attrs, attribute.String("gen_ai.content.text", truncateStr(*delta.Content, 500)))
		}
		if delta.Reasoning != nil && *delta.Reasoning != "" {
			attrs = append(attrs, attribute.String("gen_ai.content.reasoning", truncateStr(*delta.Reasoning, 500)))
		} else if delta.Reasoning2 != nil && *delta.Reasoning2 != "" {
			attrs = append(attrs, attribute.String("gen_ai.content.reasoning", truncateStr(*delta.Reasoning2, 500)))
		}
		if delta.Role != "" {
			attrs = append(attrs, attribute.String("gen_ai.content.role", delta.Role))
		}
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
			if tc.ID != "" {
				attrs = append(attrs, attribute.String(fmt.Sprintf("gen_ai.tool_call.%d.id", idx), tc.ID))
			}
			if tc.Function.Name != "" {
				attrs = append(attrs, attribute.String(fmt.Sprintf("gen_ai.tool_call.%d.name", idx), tc.Function.Name))
			}
			if tc.Function.Arguments != "" {
				attrs = append(attrs, attribute.String(fmt.Sprintf("gen_ai.tool_call.%d.arguments", idx), tc.Function.Arguments))
			}
		}
		if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason != "" {
			attrs = append(attrs, attribute.String("gen_ai.response.finish_reason", *chunk.Choices[0].FinishReason))
		}
	}

	if chunk.Usage != nil {
		attrs = append(attrs,
			attribute.Int("gen_ai.usage.input_tokens", chunk.Usage.PromptTokens),
			attribute.Int("gen_ai.usage.output_tokens", chunk.Usage.CompletionTokens),
		)
	}
	if chunk.Model != "" {
		attrs = append(attrs, attribute.String("gen_ai.response.model", chunk.Model))
	}

	return attrs
}

func parseClaudeChunk(rawData string) []attribute.KeyValue {
	var event struct {
		Type    string `json:"type"`
		Message *struct {
			Id    string `json:"id"`
			Model string `json:"model"`
			Role  string `json:"role"`
			Usage *struct {
				InputTokens  int `json:"input_tokens"`
				OutputTokens int `json:"output_tokens"`
			} `json:"usage"`
		} `json:"message"`
		ContentBlock *struct {
			Type string `json:"type"`
			Id   string `json:"id"`
			Name string `json:"name"`
			Text *string `json:"text"`
		} `json:"content_block"`
		Delta *struct {
			Type        string  `json:"type"`
			Text        *string `json:"text"`
			Thinking    *string `json:"thinking"`
			PartialJson *string `json:"partial_json"`
			StopReason  *string `json:"stop_reason"`
		} `json:"delta"`
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}

	if err := common.Unmarshal([]byte(rawData), &event); err != nil {
		return nil
	}
	if event.Type == "" {
		return nil
	}

	var attrs []attribute.KeyValue
	attrs = append(attrs, attribute.String("gen_ai.event.type", event.Type))

	switch event.Type {
	case "message_start":
		if event.Message != nil {
			attrs = append(attrs,
				attribute.String("gen_ai.response.id", event.Message.Id),
				attribute.String("gen_ai.response.model", event.Message.Model),
				attribute.String("gen_ai.content.role", event.Message.Role),
			)
			if event.Message.Usage != nil {
				attrs = append(attrs, attribute.Int("gen_ai.usage.input_tokens", event.Message.Usage.InputTokens))
			}
		}
	case "content_block_start":
		if event.ContentBlock != nil {
			attrs = append(attrs, attribute.String("gen_ai.content.block_type", event.ContentBlock.Type))
			if event.ContentBlock.Type == "tool_use" {
				if event.ContentBlock.Id != "" {
					attrs = append(attrs, attribute.String("gen_ai.tool_call.0.id", event.ContentBlock.Id))
				}
				if event.ContentBlock.Name != "" {
					attrs = append(attrs, attribute.String("gen_ai.tool_call.0.name", event.ContentBlock.Name))
				}
			}
		}
	case "content_block_delta":
		if event.Delta != nil {
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != nil && *event.Delta.Text != "" {
					attrs = append(attrs, attribute.String("gen_ai.content.text", truncateStr(*event.Delta.Text, 500)))
				}
			case "thinking_delta":
				if event.Delta.Thinking != nil && *event.Delta.Thinking != "" {
					attrs = append(attrs, attribute.String("gen_ai.content.reasoning", truncateStr(*event.Delta.Thinking, 500)))
				}
			case "input_json_delta":
				if event.Delta.PartialJson != nil && *event.Delta.PartialJson != "" {
					attrs = append(attrs, attribute.String("gen_ai.tool_call.0.arguments", *event.Delta.PartialJson))
				}
			}
		}
	case "message_delta":
		if event.Delta != nil && event.Delta.StopReason != nil && *event.Delta.StopReason != "" {
			attrs = append(attrs, attribute.String("gen_ai.response.finish_reason", *event.Delta.StopReason))
		}
		if event.Usage != nil {
			attrs = append(attrs,
				attribute.Int("gen_ai.usage.input_tokens", event.Usage.InputTokens),
				attribute.Int("gen_ai.usage.output_tokens", event.Usage.OutputTokens),
			)
		}
	}

	return attrs
}

func parseGeminiChunk(rawData string) []attribute.KeyValue {
	var chunk struct {
		Candidates []struct {
			Content *struct {
				Parts []struct {
					Text         string `json:"text"`
					FunctionCall *struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"args"`
					} `json:"functionCall"`
				} `json:"parts"`
				Role string `json:"role"`
			} `json:"content"`
			FinishReason *string `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata *struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
		} `json:"usageMetadata"`
	}

	if err := common.Unmarshal([]byte(rawData), &chunk); err != nil {
		return nil
	}
	if len(chunk.Candidates) == 0 && chunk.UsageMetadata == nil {
		return nil
	}

	var attrs []attribute.KeyValue

	if len(chunk.Candidates) > 0 {
		cand := chunk.Candidates[0]
		if cand.Content != nil {
			if cand.Content.Role != "" {
				attrs = append(attrs, attribute.String("gen_ai.content.role", cand.Content.Role))
			}
			for _, part := range cand.Content.Parts {
				if part.Text != "" {
					attrs = append(attrs, attribute.String("gen_ai.content.text", truncateStr(part.Text, 500)))
				}
				if part.FunctionCall != nil {
					attrs = append(attrs, attribute.String("gen_ai.tool_call.0.name", part.FunctionCall.Name))
					if part.FunctionCall.Arguments != nil {
						attrs = append(attrs, attribute.String("gen_ai.tool_call.0.arguments", string(part.FunctionCall.Arguments)))
					}
				}
			}
		}
		if cand.FinishReason != nil && *cand.FinishReason != "" {
			attrs = append(attrs, attribute.String("gen_ai.response.finish_reason", *cand.FinishReason))
		}
	}

	if chunk.UsageMetadata != nil {
		attrs = append(attrs,
			attribute.Int("gen_ai.usage.input_tokens", chunk.UsageMetadata.PromptTokenCount),
			attribute.Int("gen_ai.usage.output_tokens", chunk.UsageMetadata.CandidatesTokenCount),
		)
	}

	return attrs
}

func RecordClientRequestHeaders(c *gin.Context) {
	if !enabled {
		return
	}
	skip := map[string]bool{
		"authorization": true, "cookie": true, "proxy-authorization": true,
	}
	parts := make([]string, 0)
	for k := range c.Request.Header {
		if skip[strings.ToLower(k)] {
			continue
		}
		v := c.Request.Header.Get(k)
		if v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) > 0 {
		addSpanAttributes(c, attribute.String("http.request.headers", strings.Join(parts, "; ")))
	}
}

func RecordClientRequestBody(c *gin.Context) {
	if !enabled {
		return
	}
	storage, err := common.GetBodyStorage(c)
	if err != nil || storage == nil {
		return
	}
	data, err := storage.Bytes()
	if err != nil || len(data) == 0 {
		return
	}
	bodyStr := string(data)
	if len(bodyStr) > maxBodyCaptureSize {
		bodyStr = bodyStr[:maxBodyCaptureSize] + "...[truncated]"
	}
	addSpanAttributes(c, attribute.String("http.request.body", bodyStr))
}

func RecordGatewayResponseHeaders(c *gin.Context) {
	if !enabled {
		return
	}
	w := c.Writer.Header()
	if len(w) == 0 {
		return
	}
	parts := make([]string, 0)
	for k, vals := range w {
		if len(vals) > 0 {
			parts = append(parts, k+"="+vals[0])
		}
	}
	if len(parts) > 0 {
		addSpanAttributes(c, attribute.String("http.response.headers", strings.Join(parts, "; ")))
	}
}

func RecordUpstreamRequest(c *gin.Context, method, url string, headers http.Header) {
	if !enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.String("newapi.upstream.method", method),
		attribute.String("newapi.upstream.url", url),
	}
	skip := map[string]bool{
		"authorization": true, "x-api-key": true, "x-goog-api-key": true,
		"cookie": true, "proxy-authorization": true,
	}
	parts := make([]string, 0)
	for k := range headers {
		if skip[strings.ToLower(k)] {
			continue
		}
		if v := headers.Get(k); v != "" {
			parts = append(parts, k+"="+v)
		}
	}
	if len(parts) > 0 {
		attrs = append(attrs, attribute.String("newapi.upstream.request.headers", strings.Join(parts, "; ")))
	}
	addSpanAttributes(c, attrs...)
}

func RecordUpstreamRequestBody(c *gin.Context, body []byte) {
	if !enabled || len(body) == 0 {
		return
	}
	bodyStr := string(body)
	if len(bodyStr) > maxBodyCaptureSize {
		bodyStr = bodyStr[:maxBodyCaptureSize] + "...[truncated]"
	}
	addSpanAttributes(c, attribute.String("newapi.upstream.request.body", bodyStr))
}

func RecordUpstreamResponse(c *gin.Context, statusCode int, headers http.Header) {
	if !enabled {
		return
	}
	attrs := []attribute.KeyValue{
		attribute.Int("newapi.upstream.status_code", statusCode),
	}
	if headers != nil {
		parts := make([]string, 0)
		for k, vals := range headers {
			if len(vals) > 0 {
				parts = append(parts, k+"="+vals[0])
			}
		}
		if len(parts) > 0 {
			attrs = append(attrs, attribute.String("newapi.upstream.response.headers", strings.Join(parts, "; ")))
		}
	}
	addSpanAttributes(c, attrs...)
}

func RecordUpstreamResponseBody(c *gin.Context, body []byte) {
	if !enabled || len(body) == 0 {
		return
	}
	bodyStr := string(body)
	if len(bodyStr) > maxBodyCaptureSize {
		bodyStr = bodyStr[:maxBodyCaptureSize] + "...[truncated]"
	}
	addSpanAttributes(c, attribute.String("newapi.upstream.response.body", bodyStr))
}

func addSpanAttributes(c *gin.Context, attrs ...attribute.KeyValue) {
	if spanVal, exists := c.Get(ctxKeyOTELSpan); exists {
		if span, ok := spanVal.(trace.Span); ok {
			span.SetAttributes(attrs...)
		}
	}
}

func AddEvent(c *gin.Context, name string, attrs ...attribute.KeyValue) {
	if !enabled {
		return
	}
	if spanVal, exists := c.Get(ctxKeyOTELSpan); exists {
		if span, ok := spanVal.(trace.Span); ok {
			span.AddEvent(name, trace.WithAttributes(attrs...))
		}
	}
}

func GetSpan(c *gin.Context) trace.Span {
	if spanVal, exists := c.Get(ctxKeyOTELSpan); exists {
		if span, ok := spanVal.(trace.Span); ok {
			return span
		}
	}
	return trace.SpanFromContext(context.Background())
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

// RecordProcessedStreamOutput records the fully-assembled stream output on the span.
// This includes concatenated text, tool call details, and reasoning content.
// Should be called once after stream processing completes (not per-chunk).
func RecordProcessedStreamOutput(c *gin.Context, fullText string, toolCalls []ProcessedToolCall) {
	if !enabled {
		return
	}

	attrs := []attribute.KeyValue{}

	if fullText != "" {
		attrs = append(attrs, attribute.String("gen_ai.output.text", truncateStr(fullText, 4096)))
	}

	for i, tc := range toolCalls {
		if tc.Name != "" {
			attrs = append(attrs, attribute.String(fmt.Sprintf("gen_ai.tool_call.%d.name", i), tc.Name))
		}
		if tc.Arguments != "" {
			attrs = append(attrs, attribute.String(fmt.Sprintf("gen_ai.tool_call.%d.arguments", i), truncateStr(tc.Arguments, 2048)))
		}
		if tc.ID != "" {
			attrs = append(attrs, attribute.String(fmt.Sprintf("gen_ai.tool_call.%d.id", i), tc.ID))
		}
	}

	if len(attrs) > 0 {
		addSpanAttributes(c, attrs...)
	}
}

// ProcessedToolCall represents a single tool call extracted from stream processing.
type ProcessedToolCall struct {
	ID        string
	Name      string
	Arguments string
}

