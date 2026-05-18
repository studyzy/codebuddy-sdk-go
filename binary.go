// binary.go
// CLI 二进制文件定位模块 - 按优先级查找 CodeBuddy CLI 可执行文件

package codebuddy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
)

// cliVersionCache 缓存 CLI 版本号，避免重复读取文件
var (
	cliVersionCache string
	cliVersionOnce  sync.Once
)

// 公共环境变量常量
//
// 这些环境变量名与官方 CodeBuddy SDK（TypeScript/Python）保持一致，
// 供调用方在构造 Options.Env 或读取系统环境时使用，避免硬编码字符串。
const (
	// EnvAPIKey 是用户 API Key 的环境变量名（CODEBUDDY_API_KEY）。
	// 设置后，CLI 会优先使用该 Key 进行鉴权。
	EnvAPIKey = "CODEBUDDY_API_KEY"
	// EnvAuthToken 是 OAuth/会话令牌的环境变量名（CODEBUDDY_AUTH_TOKEN）。
	// 通常用于企业 OAuth 2.0 Client Credentials 场景。
	EnvAuthToken = "CODEBUDDY_AUTH_TOKEN"
	// EnvInternetEnvironment 是认证环境标识的环境变量名（CODEBUDDY_INTERNET_ENVIRONMENT）。
	// 取值如 "external" / "internal" / "ioa" / "cloudhosted"。
	EnvInternetEnvironment = "CODEBUDDY_INTERNET_ENVIRONMENT"
	// EnvCodePath 是 CLI 可执行文件路径的环境变量名（CODEBUDDY_CODE_PATH）。
	// SDK 会按此路径查找 codebuddy-headless 二进制。
	EnvCodePath = "CODEBUDDY_CODE_PATH"
)

// platformBinaryNames 各平台的 CLI 二进制文件名
var platformBinaryNames = map[string]string{
	"darwin":  "codebuddy-headless",
	"linux":   "codebuddy-headless",
	"windows": "codebuddy-headless.exe",
}

// GetCLIPath 按以下优先级查找 CodeBuddy CLI 可执行文件：
//  1. 用户在 Options.CLIPath 中指定的路径（由调用方传入，此函数不处理）
//  2. 环境变量 CODEBUDDY_CODE_PATH
//  3. 包内置的 bin/ 目录（与当前可执行文件同级）
//  4. 系统 PATH 中的 codebuddy-headless
//
// 若均未找到，返回 CLINotFoundError。
func GetCLIPath() (string, error) {
	osName := runtime.GOOS
	arch := runtime.GOARCH

	// 1. 环境变量优先
	if envPath := os.Getenv(EnvCodePath); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
		// 文件不存在，打印警告但继续尝试其他方式
		fmt.Fprintf(os.Stderr, "警告: %s 指向 '%s' 但文件不存在，尝试其他路径\n", EnvCodePath, envPath)
	}

	binaryName, ok := platformBinaryNames[osName]
	if !ok {
		binaryName = "codebuddy-headless"
	}

	// 2. 包内置的 bin/ 目录（与当前可执行文件同级）
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		binPath := filepath.Join(exeDir, "bin", binaryName)
		if _, err := os.Stat(binPath); err == nil {
			return binPath, nil
		}
	}

	// 3. 系统 PATH
	if path, err := exec.LookPath(binaryName); err == nil {
		return path, nil
	}
	// 也尝试不带 -headless 后缀的名字
	plainName := "codebuddy"
	if osName == "windows" {
		plainName = "codebuddy.exe"
	}
	if path, err := exec.LookPath(plainName); err == nil {
		return path, nil
	}

	return "", &CLINotFoundError{
		Message:  fmt.Sprintf("CodeBuddy CLI 可执行文件未找到（平台: %s/%s）。\n请安装 CodeBuddy CLI 或通过 %s 环境变量指定路径。", osName, arch, EnvCodePath),
		Platform: osName,
		Arch:     arch,
	}
}

// TryCLIPath 尝试获取 CLI 路径，失败时返回空字符串而非错误。
func TryCLIPath() string {
	path, err := GetCLIPath()
	if err != nil {
		return ""
	}
	return path
}

// GetCLIVersion 获取 CodeBuddy CLI 的版本号。
// 按以下顺序尝试：
//  1. metadata.json（goreleaser 二进制构建产物）
//  2. package.json（Node.js 开发环境）
//
// 结果会被缓存，重复调用只读取一次文件。
func GetCLIVersion(cliPath string) string {
	cliVersionOnce.Do(func() {
		cliVersionCache = resolveCliVersion(cliPath)
	})
	return cliVersionCache
}

// resolveCliVersion 实际执行版本读取逻辑。
func resolveCliVersion(cliPath string) string {
	if cliPath == "" {
		return "unknown"
	}

	cliDir := filepath.Dir(cliPath)
	parentDir := filepath.Dir(cliDir)

	// 1. 尝试 metadata.json（goreleaser 二进制）
	metadataPath := filepath.Join(parentDir, "metadata.json")
	if data, err := os.ReadFile(metadataPath); err == nil {
		var meta map[string]any
		if err := json.Unmarshal(data, &meta); err == nil {
			if tag, ok := meta["tag"].(string); ok {
				re := regexp.MustCompile(`@(\d+\.\d+\.\d+)`)
				if m := re.FindStringSubmatch(tag); len(m) > 1 {
					return m[1]
				}
			}
		}
	}

	// 2. 尝试 package.json
	for _, pkgDir := range []string{parentDir, filepath.Dir(parentDir)} {
		pkgPath := filepath.Join(pkgDir, "package.json")
		if data, err := os.ReadFile(pkgPath); err == nil {
			var pkg map[string]any
			if err := json.Unmarshal(data, &pkg); err == nil {
				// 先尝试 publishConfig.customPackage.version
				if pc, ok := pkg["publishConfig"].(map[string]any); ok {
					if cp, ok := pc["customPackage"].(map[string]any); ok {
						if v, ok := cp["version"].(string); ok && v != "" && v != "0.0.0" {
							return v
						}
					}
				}
				if v, ok := pkg["version"].(string); ok && v != "" && v != "0.0.0" {
					return v
				}
			}
		}
	}

	return "unknown"
}
