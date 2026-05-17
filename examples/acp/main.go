//go:build ignore

// acp/main.go
// ACP HTTP 客户端使用示例。
//
// 此文件展示两种使用 ACPClient 的方式：
//  1. RunTask - 一次性任务，自动管理连接生命周期
//  2. 多轮对话 - 复用连接发送多个提示
//
// 运行方式（需要实际运行中的 ACP 服务）：
//
//	go run examples/acp/main.go

package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	// 替换为实际的 ACP 端点和 Bearer Token
	const acpURL = "http://localhost:8848/acp"
	const token = "your-bearer-token"

	ctx := context.Background()

	// 示例 1: RunTask - 最简用法，自动连接、发送、断开
	fmt.Println("=== 示例 1: RunTask ===")
	result, err := codebuddy.NewACPClient(acpURL, token).RunTask(ctx, "用一句话介绍你自己")
	if err != nil {
		log.Printf("RunTask 失败: %v", err)
	} else {
		fmt.Printf("代理回复: %s\n", result)
	}

	// 示例 2: 多轮对话 - 复用连接保持上下文
	fmt.Println("\n=== 示例 2: 多轮对话 ===")
	client := codebuddy.NewACPClient(acpURL, token)
	if err := client.Connect(ctx); err != nil {
		log.Fatalf("连接失败: %v", err)
	}
	defer client.Disconnect()

	// 第一轮：带实时流式回调
	_, err = client.Prompt(ctx, "请记住：我的名字是 Alice", func(update map[string]any) {
		if update["sessionUpdate"] == "agent_message_chunk" {
			if content, ok := update["content"].(map[string]any); ok {
				if content["type"] == "text" {
					fmt.Print(content["text"])
				}
			}
		}
	})
	fmt.Println()
	if err != nil {
		log.Printf("第一轮 Prompt 失败: %v", err)
	}

	// 第二轮（同一会话，CodeBuddy 能记住第一轮的内容）
	updates2, err := client.Prompt(ctx, "我的名字是什么？", nil)
	if err != nil {
		log.Printf("第二轮 Prompt 失败: %v", err)
		return
	}
	// 手动提取文本回复
	var sb strings.Builder
	for _, u := range updates2 {
		if u["sessionUpdate"] == "agent_message_chunk" {
			if content, ok := u["content"].(map[string]any); ok {
				if content["type"] == "text" {
					if text, ok := content["text"].(string); ok {
						sb.WriteString(text)
					}
				}
			}
		}
	}
	fmt.Printf("第二轮回复: %s\n", sb.String())
}
