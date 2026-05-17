// session_lock.go
// 进程内会话锁 - 防止同一 session ID 被同时 resume。
// 对应 NodeJS SDK 的 connect.js acquireSessionLock / releaseSessionLock。

package codebuddy

import "sync"

var sessionLocks sync.Map // key: sessionID (string) → struct{}{}

// acquireSessionLock 尝试为指定 sessionID 获取锁。
// 若该 sessionID 已被锁定，返回 false；否则锁定并返回 true。
func acquireSessionLock(sessionID string) bool {
	_, loaded := sessionLocks.LoadOrStore(sessionID, struct{}{})
	return !loaded
}

// releaseSessionLock 释放指定 sessionID 的锁。
func releaseSessionLock(sessionID string) {
	sessionLocks.Delete(sessionID)
}
