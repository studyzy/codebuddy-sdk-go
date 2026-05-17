// protocol.go
// 控制协议助手 - 构建 SDK 与 CLI 之间的控制请求/响应信封

package codebuddy

import (
	"context"
	"encoding/json"
	"fmt"
)

// BuildControlResponse 构建成功的控制响应信封。
// 格式：{"type":"control_response","response":{"subtype":"success","request_id":"...","response":{...}}}
func BuildControlResponse(requestID string, response map[string]any) map[string]any {
	return map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   response,
		},
	}
}

// BuildControlErrorResponse 构建错误的控制响应信封。
// 格式：{"type":"control_response","response":{"subtype":"error","request_id":"...","error":"..."}}
func BuildControlErrorResponse(requestID string, errMsg string) map[string]any {
	return map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "error",
			"request_id": requestID,
			"error":      errMsg,
		},
	}
}

// writeControlResponse 将控制响应序列化并写入 transport。
// 这是 json.Marshal + transport.Write 的统一封装，避免各处的静默错误忽略。
func writeControlResponse(ctx context.Context, t Transport, payload map[string]any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		// json.Marshal 对纯 Go 结构体基本不会失败；记录后继续
		return fmt.Errorf("marshal control response: %w", err)
	}
	return t.Write(ctx, string(b))
}

// HookCallbackRegistry Hooks 回调注册表：callback ID → HookCallback 函数
type HookCallbackRegistry map[string]HookCallback

// BuildHooksConfig 将 Options.Hooks 转换为 CLI 期望的配置格式，同时构建回调注册表。
//
// 返回值：
//   - config：发送给 CLI 的 hooks 配置（可为 nil）
//   - registry：callback ID → HookCallback 的映射表
//
// callback ID 格式：hook_{event}_{matcherIndex}_{hookIndex}
// CLI 期望格式：{"PreToolUse": [{"matcher": "...", "hookCallbackIds": ["..."], "timeout": 30}]}
func BuildHooksConfig(hooks map[HookEvent][]HookMatcher) (config map[string]any, registry HookCallbackRegistry) {
	registry = make(HookCallbackRegistry)
	if hooks == nil {
		return nil, registry
	}

	config = make(map[string]any)
	for event, matchers := range hooks {
		eventStr := string(event)
		matcherConfigs := make([]any, 0, len(matchers))
		for i, m := range matchers {
			callbackIDs := make([]string, 0, len(m.Hooks))
			for j, hookFn := range m.Hooks {
				cbID := fmt.Sprintf("hook_%s_%d_%d", eventStr, i, j)
				callbackIDs = append(callbackIDs, cbID)
				registry[cbID] = hookFn
			}
			mc := map[string]any{
				"matcher":         m.Matcher,
				"hookCallbackIds": callbackIDs,
				"timeout":         m.Timeout,
			}
			matcherConfigs = append(matcherConfigs, mc)
		}
		config[eventStr] = matcherConfigs
	}
	if len(config) == 0 {
		return nil, registry
	}
	return config, registry
}
