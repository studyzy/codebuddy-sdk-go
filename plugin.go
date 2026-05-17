// plugin.go
// 插件管理 API - 通过直接操作 ~/.codebuddy/settings.json 管理市场和插件。
//
// 对应 NodeJS SDK 的 plugin.js，提供以下功能：
//   - InstallMarketplace / RemoveMarketplace：管理插件市场
//   - InstallPlugin / EnablePlugin / DisablePlugin：管理具体插件
//
// 注意：这些操作直接读写本地配置文件，不需要 CLI 进程，也不需要认证。

package codebuddy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PluginResult 表示插件操作的结果。
type PluginResult struct {
	// Success 是否成功
	Success bool
	// Message 操作结果描述
	Message string
}

// InstallMarketplaceOptions 表示安装市场的选项。
type InstallMarketplaceOptions struct {
	// Name 市场名称
	Name string
	// Repo GitHub 仓库，格式 "owner/repo"
	Repo string
	// AutoUpdate 是否自动更新，默认 true
	AutoUpdate bool
}

// RemoveMarketplaceOptions 表示移除市场的选项。
type RemoveMarketplaceOptions struct {
	// Name 市场名称
	Name string
	// RemovePlugins 是否同时移除属于该市场的已启用插件，默认 true
	RemovePlugins bool
}

// InstallPluginOptions 表示安装插件的选项。
type InstallPluginOptions struct {
	// Name 插件名称
	Name string
	// Marketplace 所属市场名称
	Marketplace string
}

// ---- 内部辅助函数 ----

func codeBuddyDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codebuddy")
}

func settingsPath() string {
	return filepath.Join(codeBuddyDir(), "settings.json")
}

func knownMarketplacesPath() string {
	return filepath.Join(codeBuddyDir(), "plugins", "known_marketplaces.json")
}

func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return map[string]any{}, nil
	}
	return result, nil
}

func writeJSONFile(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return os.WriteFile(path, b, 0o644)
}

func pluginID(name, marketplace string) string {
	return name + "@" + marketplace
}

// ---- 公共 API ----

// InstallMarketplace 安装市场，添加到 extraKnownMarketplaces。
//
// 示例：
//
//	result, err := codebuddy.InstallMarketplace(codebuddy.InstallMarketplaceOptions{
//	    Name: "my-marketplace",
//	    Repo: "owner/repo",
//	})
func InstallMarketplace(opts InstallMarketplaceOptions) (PluginResult, error) {
	if opts.Name == "" || opts.Repo == "" {
		return PluginResult{Message: "marketplace name and repo are required"}, nil
	}
	if !strings.Contains(opts.Repo, "/") {
		return PluginResult{Message: `repo must be in format "owner/repo"`}, nil
	}

	settings, err := readJSONFile(settingsPath())
	if err != nil {
		return PluginResult{}, err
	}

	known, _ := settings["extraKnownMarketplaces"].(map[string]any)
	if known == nil {
		known = make(map[string]any)
	}
	if _, exists := known[opts.Name]; exists {
		return PluginResult{Message: fmt.Sprintf("marketplace %q already exists", opts.Name)}, nil
	}

	autoUpdate := true
	if !opts.AutoUpdate {
		autoUpdate = false
	}
	known[opts.Name] = map[string]any{
		"source": map[string]any{
			"source": "github",
			"repo":   opts.Repo,
		},
		"autoUpdate": autoUpdate,
	}
	settings["extraKnownMarketplaces"] = known

	if err := writeJSONFile(settingsPath(), settings); err != nil {
		return PluginResult{}, err
	}
	return PluginResult{Success: true, Message: fmt.Sprintf("marketplace %q installed successfully", opts.Name)}, nil
}

// RemoveMarketplace 从配置中移除市场。
//
// 示例：
//
//	result, err := codebuddy.RemoveMarketplace(codebuddy.RemoveMarketplaceOptions{
//	    Name: "my-marketplace",
//	})
func RemoveMarketplace(opts RemoveMarketplaceOptions) (PluginResult, error) {
	if opts.Name == "" {
		return PluginResult{Message: "marketplace name is required"}, nil
	}

	settings, err := readJSONFile(settingsPath())
	if err != nil {
		return PluginResult{}, err
	}
	knownMarketplaces, err := readJSONFile(knownMarketplacesPath())
	if err != nil {
		return PluginResult{}, err
	}

	inSettings := false
	if known, ok := settings["extraKnownMarketplaces"].(map[string]any); ok {
		_, inSettings = known[opts.Name]
	}
	_, inKnown := knownMarketplaces[opts.Name]

	if !inSettings && !inKnown {
		return PluginResult{Message: fmt.Sprintf("marketplace %q not found", opts.Name)}, nil
	}

	// 从 settings.json 移除
	if known, ok := settings["extraKnownMarketplaces"].(map[string]any); ok {
		delete(known, opts.Name)
		if len(known) == 0 {
			delete(settings, "extraKnownMarketplaces")
		} else {
			settings["extraKnownMarketplaces"] = known
		}
	}

	// 按需移除属于该市场的插件
	removePlugins := opts.RemovePlugins
	if removePlugins {
		if enabled, ok := settings["enabledPlugins"].(map[string]any); ok {
			suffix := "@" + opts.Name
			for pluginKey := range enabled {
				if strings.HasSuffix(pluginKey, suffix) {
					delete(enabled, pluginKey)
				}
			}
			if len(enabled) == 0 {
				delete(settings, "enabledPlugins")
			} else {
				settings["enabledPlugins"] = enabled
			}
		}
	}

	if err := writeJSONFile(settingsPath(), settings); err != nil {
		return PluginResult{}, err
	}

	// 从 known_marketplaces.json 移除
	if inKnown {
		delete(knownMarketplaces, opts.Name)
		if err := writeJSONFile(knownMarketplacesPath(), knownMarketplaces); err != nil {
			return PluginResult{}, err
		}
	}

	return PluginResult{Success: true, Message: fmt.Sprintf("marketplace %q removed successfully", opts.Name)}, nil
}

// InstallPlugin 安装并启用插件（向 enabledPlugins 添加 true 记录）。
//
// 示例：
//
//	result, err := codebuddy.InstallPlugin(codebuddy.InstallPluginOptions{
//	    Name:        "typescript-lsp",
//	    Marketplace: "claude-plugins-official",
//	})
func InstallPlugin(opts InstallPluginOptions) (PluginResult, error) {
	if opts.Name == "" || opts.Marketplace == "" {
		return PluginResult{Message: "plugin name and marketplace are required"}, nil
	}

	settings, err := readJSONFile(settingsPath())
	if err != nil {
		return PluginResult{}, err
	}

	enabled, _ := settings["enabledPlugins"].(map[string]any)
	if enabled == nil {
		enabled = make(map[string]any)
	}
	id := pluginID(opts.Name, opts.Marketplace)
	enabled[id] = true
	settings["enabledPlugins"] = enabled

	if err := writeJSONFile(settingsPath(), settings); err != nil {
		return PluginResult{}, err
	}
	return PluginResult{Success: true, Message: fmt.Sprintf("plugin %q installed and enabled successfully", id)}, nil
}

// EnablePlugin 启用已安装的插件（与 InstallPlugin 行为相同）。
//
// 示例：
//
//	codebuddy.EnablePlugin("typescript-lsp", "claude-plugins-official")
func EnablePlugin(name, marketplace string) (PluginResult, error) {
	return InstallPlugin(InstallPluginOptions{Name: name, Marketplace: marketplace})
}

// DisablePlugin 禁用插件（向 enabledPlugins 写入 false 记录）。
//
// 示例：
//
//	codebuddy.DisablePlugin("typescript-lsp", "claude-plugins-official")
func DisablePlugin(name, marketplace string) (PluginResult, error) {
	if name == "" || marketplace == "" {
		return PluginResult{Message: "plugin name and marketplace are required"}, nil
	}

	settings, err := readJSONFile(settingsPath())
	if err != nil {
		return PluginResult{}, err
	}

	enabled, _ := settings["enabledPlugins"].(map[string]any)
	if enabled == nil {
		enabled = make(map[string]any)
	}
	id := pluginID(name, marketplace)
	enabled[id] = false
	settings["enabledPlugins"] = enabled

	if err := writeJSONFile(settingsPath(), settings); err != nil {
		return PluginResult{}, err
	}
	return PluginResult{Success: true, Message: fmt.Sprintf("plugin %q disabled successfully", id)}, nil
}
