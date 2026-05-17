package main

import (
	"context"
	"fmt"
	"os"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	// Register CanUseTool - allow read-only tools, deny writes
	opts := &codebuddy.Options{
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, opts codebuddy.CanUseToolOptions) (codebuddy.PermissionResult, error) {
			allowed := []string{"Read", "Glob", "Grep", "Bash"}
			for _, a := range allowed {
				if toolName == a {
					return &codebuddy.PermissionResultAllow{}, nil
				}
			}
			return &codebuddy.PermissionResultDeny{
				Message: fmt.Sprintf("工具 %s 已被权限回调拒绝", toolName),
			}, nil
		},
	}

	ctx := context.Background()
	msgCh, err := codebuddy.Query(ctx, "请帮我创建一个 /tmp/test.txt 文件，内容为 hello", opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Query error: %v\n", err)
		os.Exit(1)
	}

	for msg := range msgCh {
		switch m := msg.(type) {
		case *codebuddy.AssistantMessage:
			for _, block := range m.Content {
				if tb, ok := block.(*codebuddy.TextBlock); ok {
					fmt.Println(tb.Text)
				}
			}
		case *codebuddy.ResultMessage:
			if m.Result != nil {
				fmt.Printf("[Result] %s\n", *m.Result)
			}
		case *codebuddy.ErrorMessage:
			fmt.Fprintf(os.Stderr, "[Error] %s\n", m.Error)
		}
	}
}
