// Package codebuddy 定义了 CodeBuddy SDK 的所有公共类型。
// message_parser.go
// 消息解析器 - 将 CLI 输出的原始 JSON 数据解析为强类型消息和内容块

package codebuddy

// ParseContentBlock 将原始 map 数据解析为 ContentBlock。
// 根据 "type" 字段区分类型：text / thinking / tool_use / tool_result /
// redacted_thinking / image。
// 不认识的类型返回 nil。
func ParseContentBlock(data map[string]any) ContentBlock {
	blockType, _ := data["type"].(string)
	switch blockType {
	case "text":
		return &TextBlock{
			Text: getString(data, "text"),
		}
	case "thinking":
		return &ThinkingBlock{
			Thinking:  getString(data, "thinking"),
			Signature: getString(data, "signature"),
		}
	case "tool_use":
		input, _ := data["input"].(map[string]any)
		if input == nil {
			input = make(map[string]any)
		}
		return &ToolUseBlock{
			ID:    getString(data, "id"),
			Name:  getString(data, "name"),
			Input: input,
		}
	case "tool_result":
		isErr, _ := data["is_error"].(bool)
		var isErrPtr *bool
		if v, ok := data["is_error"]; ok && v != nil {
			isErrPtr = &isErr
		}
		return &ToolResultBlock{
			ToolUseID: getString(data, "tool_use_id"),
			Content:   data["content"],
			IsError:   isErrPtr,
		}
	case "redacted_thinking":
		return &RedactedThinkingBlock{
			Data: getString(data, "data"),
		}
	case "image":
		source := ImageSource{}
		if srcData, ok := data["source"].(map[string]any); ok {
			source.Type = getString(srcData, "type")
			source.MediaType = getString(srcData, "media_type")
			source.Data = getString(srcData, "data")
			source.URL = getString(srcData, "url")
		}
		return &ImageContentBlock{
			Source: source,
		}
	}
	return nil
}

// ParseContentBlocks 批量解析内容块列表，跳过无法解析的项。
func ParseContentBlocks(items []any) []ContentBlock {
	blocks := make([]ContentBlock, 0, len(items))
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if b := ParseContentBlock(m); b != nil {
				blocks = append(blocks, b)
			}
		}
	}
	return blocks
}

// ParseMessage 将原始 map 数据解析为 Message。
// 根据顶层 "type" 字段区分：user / assistant / system / result / stream_event /
// error / topic / tool_progress / file-history-snapshot。
// system 类型会按 subtype 进一步分派为 CompactBoundaryMessage 或 StatusMessage。
// 不认识的类型返回 nil。
func ParseMessage(data map[string]any) Message {
	msgType, _ := data["type"].(string)
	switch msgType {
	case "user":
		msgData, _ := data["message"].(map[string]any)
		var content any
		if msgData != nil {
			content = msgData["content"]
		}
		// content 可能是 string 或 []any（ContentBlock 列表）
		if contentList, ok := content.([]any); ok {
			content = ParseContentBlocks(contentList)
		}
		uuid := getStringPtr(data, "uuid")
		parentID := getStringPtr(data, "parent_tool_use_id")
		return &UserMessage{
			Content:         content,
			UUID:            uuid,
			ParentToolUseID: parentID,
		}

	case "assistant":
		msgData, _ := data["message"].(map[string]any)
		var blocks []ContentBlock
		if msgData != nil {
			if contentList, ok := msgData["content"].([]any); ok {
				blocks = ParseContentBlocks(contentList)
			}
		}
		model := getString(data, "model")
		parentID := getStringPtr(data, "parent_tool_use_id")
		errStr := getStringPtr(data, "error")
		return &AssistantMessage{
			Content:         blocks,
			Model:           model,
			ParentToolUseID: parentID,
			Error:           errStr,
		}

	case "partial_assistant":
		msgData, _ := data["message"].(map[string]any)
		var blocks []ContentBlock
		if msgData != nil {
			if contentList, ok := msgData["content"].([]any); ok {
				blocks = ParseContentBlocks(contentList)
			}
		}
		model := getString(data, "model")
		parentID := getStringPtr(data, "parent_tool_use_id")
		errStr := getStringPtr(data, "error")
		return &PartialAssistantMessage{
			Content:         blocks,
			Model:           model,
			ParentToolUseID: parentID,
			Error:           errStr,
		}

	case "system":
		subtype := getString(data, "subtype")
		switch subtype {
		case "compact_boundary":
			compactMeta, _ := data["compact_metadata"].(map[string]any)
			return &CompactBoundaryMessage{
				UUID:            getString(data, "uuid"),
				SessionID:       getString(data, "session_id"),
				CompactMetadata: compactMeta,
			}
		case "status":
			return &StatusMessage{
				Status:    getStringPtr(data, "status"),
				UUID:      getString(data, "uuid"),
				SessionID: getString(data, "session_id"),
			}
		default:
			return &SystemMessage{
				Subtype: subtype,
				Data:    data,
			}
		}

	case "result":
		// stop_reason
		stopReason := getStringPtr(data, "stop_reason")
		// total_cost_usd
		var costUSD *float64
		if v, ok := data["total_cost_usd"].(float64); ok {
			costUSD = &v
		}
		// usage
		var usage map[string]any
		if u, ok := data["usage"].(map[string]any); ok {
			usage = u
		}
		// result
		result := getStringPtr(data, "result")
		// errors
		var errs []string
		if el, ok := data["errors"].([]any); ok {
			for _, e := range el {
				if s, ok := e.(string); ok {
					errs = append(errs, s)
				}
			}
		}
		durationMs := getInt(data, "duration_ms")
		durationAPIMs := getInt(data, "duration_api_ms")
		numTurns := getInt(data, "num_turns")
		return &ResultMessage{
			Subtype:           getString(data, "subtype"),
			DurationMs:        durationMs,
			DurationAPIMs:     durationAPIMs,
			IsError:           getBool(data, "is_error"),
			NumTurns:          numTurns,
			SessionID:         getString(data, "session_id"),
			StopReason:        stopReason,
			TotalCostUSD:      costUSD,
			Usage:             usage,
			Result:            result,
			StructuredOutput:  data["structured_output"],
			PermissionDenials: parsePermissionDenials(data),
			Errors:            errs,
		}

	case "stream_event":
		event, _ := data["event"].(map[string]any)
		if event == nil {
			event = make(map[string]any)
		}
		return &StreamEvent{
			UUID:            getString(data, "uuid"),
			SessionID:       getString(data, "session_id"),
			Event:           event,
			ParentToolUseID: getStringPtr(data, "parent_tool_use_id"),
		}

	case "error":
		var errs []string
		if rawErrors, ok := data["errors"].([]any); ok {
			for _, item := range rawErrors {
				if s, ok := item.(string); ok {
					errs = append(errs, s)
				}
			}
		}
		subtype := getStringPtr(data, "subtype")
		return &ErrorMessage{
			Error:     getString(data, "error"),
			SessionID: getStringPtr(data, "session_id"),
			Errors:    errs,
			Subtype:   subtype,
		}

	case "topic":
		return &TopicMessage{
			Topic:     getString(data, "topic"),
			SessionID: getString(data, "session_id"),
		}

	case "tool_progress":
		return &ToolProgressMessage{
			ToolUseID:          getString(data, "tool_use_id"),
			ToolName:           getString(data, "tool_name"),
			ParentToolUseID:    getStringPtr(data, "parent_tool_use_id"),
			ElapsedTimeSeconds: getFloat64(data, "elapsed_time_seconds"),
			UUID:               getString(data, "uuid"),
			SessionID:          getString(data, "session_id"),
		}

	case "file-history-snapshot":
		snapshot, _ := data["snapshot"].(map[string]any)
		return &FileHistorySnapshotMessage{
			Timestamp:        int64(getFloat64(data, "timestamp")),
			IsSnapshotUpdate: getBool(data, "isSnapshotUpdate"),
			Snapshot:         snapshot,
			ID:               getStringPtr(data, "id"),
			ParentID:         getStringPtr(data, "parentId"),
		}
	}
	return nil
}

// --- 私有辅助函数 ---

func getString(data map[string]any, key string) string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getStringPtr(data map[string]any, key string) *string {
	if v, ok := data[key]; ok {
		if s, ok := v.(string); ok {
			return &s
		}
	}
	return nil
}

func getBool(data map[string]any, key string) bool {
	if v, ok := data[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return false
}

func getInt(data map[string]any, key string) int {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		case int64:
			return int(n)
		}
	}
	return 0
}

func getFloat64(data map[string]any, key string) float64 {
	if v, ok := data[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return 0
}

// parsePermissionDenials 从 result 消息中解析 permission_denials 数组。
func parsePermissionDenials(data map[string]any) []PermissionDenial {
	raw, ok := data["permission_denials"].([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	denials := make([]PermissionDenial, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		toolInput, _ := m["tool_input"].(map[string]any)
		denials = append(denials, PermissionDenial{
			ToolName:  getString(m, "tool_name"),
			ToolUseID: getString(m, "tool_use_id"),
			ToolInput: toolInput,
		})
	}
	return denials
}
