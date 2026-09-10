package ota

import "errors"

// 哨兵错误。
var (
	// ErrFirmwareNotFound 表示固件未找到。
	ErrFirmwareNotFound = errors.New("ota: firmware not found")

	// ErrFirmwareExists 表示固件版本已存在。
	ErrFirmwareExists = errors.New("ota: firmware version already exists")

	// ErrActivationExpired 表示激活码已过期。
	ErrActivationExpired = errors.New("ota: activation code expired")

	// ErrChecksumMismatch 表示固件校验和不匹配。
	ErrChecksumMismatch = errors.New("ota: checksum mismatch")
)