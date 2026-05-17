// examples/query/main.go
// Query 单次查询示例：向 CodeBuddy 发送一条指令并打印所有返回消息

package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	prompt := "用一句话介绍 Go 语言的并发模型"
	if len(os.Args) > 1 {
		prompt = strings.Join(os.Args[1:], " ")
	}

	ctx := context.Background()
	msgCh, err := codebuddy.Query(ctx, prompt, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "连接失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("发送查询: %s\n\n", prompt)

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
