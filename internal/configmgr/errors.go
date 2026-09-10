package configmgr

import "errors"

// 哨兵错误。
var (
	// ErrConfigNotFound 配置项不存在。
	ErrConfigNotFound = errors.New("configmgr: config not found")

	// ErrConfigSystem 系统配置不可删除。
	ErrConfigSystem = errors.New("configmgr: system config cannot be deleted")

	// ErrConfigInvalid 配置值类型无效。
	ErrConfigInvalid = errors.New("configmgr: invalid config value type")
)