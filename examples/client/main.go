// examples/client/main.go
// Client / Session 多轮对话示例
//
// 演示两种使用方式：
//   1. 推荐：client.NewSession() → Session API（对象模型清晰）
//   2. 兼容：client.Connect() → 旧版 Client 直连 API

package main

import (
	"context"
	"fmt"
	"os"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	ctx := context.Background()

	// ============================================================
	// 方式一（推荐）：Session API
	// ============================================================
	fmt.Println("=== 方式一：Session API ===")

	client := codebuddy.NewClient(nil)

	// NewSession 立即确定 SessionID，不发起任何 I/O
	session := client.NewSession(nil)
	defer session.Close()

	fmt.Printf("Session ID: %s\n\n", session.ID())

	// --- 第一轮 ---
	fmt.Println("第一轮: What is 1+1?")
	if err := session.Send(ctx, "What is 1+1?"); err != nil {
		fmt.Fprintf(os.Stderr, "Send 失败: %v\n", err)
		os.Exit(1)
	}
	result1, err := printAndWait(ctx, session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "接收失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n[第一轮完成] 用时: %dms，轮次: %d\n\n", result1.DurationMs, result1.NumTurns)

	// --- 第二轮（保持上下文）---
	fmt.Println("第二轮: Now multiply that by 5")
	if err := session.Send(ctx, "Now multiply that by 5"); err != nil {
		fmt.Fprintf(os.Stderr, "Send 失败: %v\n", err)
		os.Exit(1)
	}
	result2, err := printAndWait(ctx, session)
	if err != nil {
		fmt.Fprintf(os.Stderr, "接收失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n[第二轮完成] 用时: %dms，轮次: %d\n\n", result2.DurationMs, result2.NumTurns)
}

// printAndWait 从 Session.Stream() 迭代消息并打印，直到 ResultMessage。
func printAndWait(ctx context.Context, session *codebuddy.Session) (*codebuddy.ResultMessage, error) {
	for {
		select {
		case msg, ok := <-session.Stream():
			if !ok {
				return nil, fmt.Errorf("session closed")
			}
			switch m := msg.(type) {
			case *codebuddy.AssistantMessage:
				for _, block := range m.Content {
					if tb, ok := block.(*codebuddy.TextBlock); ok {
						fmt.Print(tb.Text)
					}
				}
			case *codebuddy.ResultMessage:
				fmt.Println()
				return m, nil
			case *codebuddy.ErrorMessage:
				return nil, fmt.Errorf("CLI 错误: %s", m.Error)
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}
