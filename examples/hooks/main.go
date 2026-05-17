package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	matcherName := "Bash"
	timeout := float64(30)

	// Register PreToolUse hook for Bash - print command but block dangerous ones
	opts := &codebuddy.Options{
		Hooks: map[codebuddy.HookEvent][]codebuddy.HookMatcher{
			codebuddy.HookPreToolUse: {
				{
					Matcher: &matcherName,
					Hooks: []codebuddy.HookCallback{
						func(ctx context.Context, input map[string]any, toolUseID *string) (codebuddy.HookJSONOutput, error) {
							cmd, _ := input["command"].(string)
							fmt.Printf("[Hook] 即将执行 Bash: %s\n", cmd)

							// Block dangerous commands
							dangerous := []string{"rm -rf", "sudo", "dd if="}
							for _, d := range dangerous {
								if strings.Contains(cmd, d) {
									cont := false
									reason := fmt.Sprintf("危险命令已被 Hook 阻止: %s", d)
									return codebuddy.HookJSONOutput{Continue: &cont, StopReason: &reason}, nil
								}
							}

							cont := true
							return codebuddy.HookJSONOutput{Continue: &cont}, nil
						},
					},
					Timeout: &timeout,
				},
			},
		},
	}

	ctx := context.Background()
	msgCh, err := codebuddy.Query(ctx, "请列出当前目录下的文件", opts)
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
