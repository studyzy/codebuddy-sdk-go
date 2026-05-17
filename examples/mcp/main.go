// examples/mcp/main.go
// MCP SDK 服务器示例：注册一个 get_weather 工具，向 CodeBuddy 询问北京天气

package main

import (
	"context"
	"fmt"
	"os"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	// 创建 SDK MCP 服务器并注册 get_weather 工具
	server, sdkConfig := codebuddy.CreateSDKMCPServer("weather-server", "1.0.0", []*codebuddy.ToolDefinition{
		{
			Name:        "get_weather",
			Description: "获取指定城市的当前天气信息",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "城市名称，例如：北京、上海、广州",
					},
				},
				"required": []string{"city"},
			},
			Handler: codebuddy.WrapSimpleHandler(func(ctx context.Context, args map[string]any) (string, error) {
				city, _ := args["city"].(string)
				if city == "" {
					city = "未知城市"
				}
				// 返回模拟天气数据
				return fmt.Sprintf(`{
  "city": "%s",
  "temperature": "18°C",
  "feels_like": "16°C",
  "condition": "晴转多云",
  "humidity": "45%%",
  "wind": "东南风 3级",
  "uv_index": "中等",
  "forecast": "未来三天以晴天为主，气温逐渐回升"
}`, city), nil
			}),
		},
	})
	_ = server // server 已通过 sdkConfig 引用，此处无需直接使用

	// 将 SDK MCP 服务器配置放入 MCPServers
	opts := &codebuddy.Options{
		MCPServers: codebuddy.MCPServerMap{
			"weather": sdkConfig,
		},
		PermissionMode: codebuddy.PermissionModeBypassPermissions.Ptr(),
	}

	ctx := context.Background()
	msgCh, err := codebuddy.Query(ctx, "北京今天天气怎么样？请使用 get_weather 工具查询。", opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("正在查询北京天气...")

	for msg := range msgCh {
		switch m := msg.(type) {
		case *codebuddy.AssistantMessage:
			for _, block := range m.Content {
				if tb, ok := block.(*codebuddy.TextBlock); ok {
					fmt.Print(tb.Text)
				}
			}
		case *codebuddy.ResultMessage:
			fmt.Printf("\n\n--- 完成 ---\n")
			fmt.Printf("用时: %dms，轮次: %d\n", m.DurationMs, m.NumTurns)
			if m.TotalCostUSD != nil {
				fmt.Printf("费用: $%.4f\n", *m.TotalCostUSD)
			}
		case *codebuddy.ErrorMessage:
			fmt.Fprintf(os.Stderr, "错误: %s\n", m.Error)
		}
	}
}
