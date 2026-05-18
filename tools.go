// Package codebuddy 定义了 CodeBuddy SDK 的工具相关类型。
// 本文件包含工具名称常量、AskUserQuestion 输入类型等。
package codebuddy

// =============================================================================
// 工具名称常量
// =============================================================================

const (
	// ToolAskUserQuestion 是向用户提问的工具名称。
	ToolAskUserQuestion = "AskUserQuestion"
	// ToolRead 是读取文件的工具名称。
	ToolRead = "Read"
	// ToolWrite 是写入文件的工具名称。
	ToolWrite = "Write"
	// ToolEdit 是编辑文件的工具名称。
	ToolEdit = "Edit"
	// ToolMultiEdit 是多处编辑文件的工具名称。
	ToolMultiEdit = "MultiEdit"
	// ToolNotebookEdit 是编辑 Notebook 的工具名称。
	ToolNotebookEdit = "NotebookEdit"
	// ToolGlob 是按 glob 模式搜索文件路径的工具名称。
	ToolGlob = "Glob"
	// ToolGrep 是按正则在文件内容中搜索的工具名称。
	ToolGrep = "Grep"
	// ToolBash 是执行 Shell 命令的工具名称。
	ToolBash = "Bash"
	// ToolTask 是派生子 Agent 执行任务的工具名称。
	ToolTask = "Task"
	// ToolWebFetch 是抓取 URL 内容的工具名称。
	ToolWebFetch = "WebFetch"
	// ToolWebSearch 是执行联网搜索的工具名称。
	ToolWebSearch = "WebSearch"
)

// =============================================================================
// AskUserQuestion 类型
// =============================================================================

// AskUserQuestionOption 表示 askUserQuestion 工具的单个选项。
type AskUserQuestionOption struct {
	Label       string
	Description string
}

// AskUserQuestionQuestion 表示 askUserQuestion 工具中的单个问题。
type AskUserQuestionQuestion struct {
	Question    string
	Header      string
	Options     []AskUserQuestionOption
	MultiSelect bool
}

// AskUserQuestionInput 表示 askUserQuestion 工具的输入。
type AskUserQuestionInput struct {
	Questions []AskUserQuestionQuestion
	Answers   map[string]string
}
