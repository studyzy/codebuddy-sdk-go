// Package codebuddy 定义了 CodeBuddy SDK 的所有公共类型。
// message_parser.go
// 消息解析器 - 将 CLI 输出的原始 JSON 数据解析为强类型消息和内容块

package codebuddy

// ParseContentBlock 将原始 map 数据解析为 ContentBlock。
// 根据 "type" 字段区分类型：text / thinking / tool_use / tool_result。
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
// 根据顶层 "type" 字段区分：user / assistant / system / result / stream_event / error。
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

	case "system":
		return &SystemMessage{
			Subtype: getString(data, "subtype"),
			Data:    data,
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
			Subtype:          getString(data, "subtype"),
			DurationMs:       durationMs,
			DurationAPIMs:    durationAPIMs,
			IsError:          getBool(data, "is_error"),
			NumTurns:         numTurns,
			SessionID:        getString(data, "session_id"),
			StopReason:       stopReason,
			TotalCostUSD:     costUSD,
			Usage:            usage,
			Result:           result,
			StructuredOutput: data["structured_output"],
			Errors:           errs,
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
