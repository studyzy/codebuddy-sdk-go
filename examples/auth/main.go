// examples/auth/main.go
// 认证示例：演示 Authenticate/Wait/Logout 流程

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	codebuddy "github.com/studyzy/codebuddy-sdk-go"
)

func main() {
	ctx := context.Background()

	fmt.Println("正在检查认证状态...")
	flow, err := codebuddy.Authenticate(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "启动认证流程失败: %v\n", err)
		os.Exit(1)
	}

	if flow.AuthURL != "" {
		fmt.Printf("请在浏览器中访问以下 URL 完成登录:\n%s\n\n", flow.AuthURL)
		fmt.Println("等待登录完成（超时 5 分钟）...")
	} else {
		fmt.Println("已登录，获取用户信息...")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	result, err := flow.Wait(timeoutCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "登录失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("登录成功！\n")
	fmt.Printf("用户名: %s\n", result.UserInfo.UserName)
	fmt.Printf("用户ID: %s\n", result.UserInfo.UserID)
	if result.UserInfo.Enterprise != nil {
		fmt.Printf("企业: %s\n", *result.UserInfo.Enterprise)
	}
}
