package util

import (
	"regexp"
	"strings"
)

const (
	MaxUsernameLen = 32
	MaxMessageLen  = 4000
	MaxChannelLen  = 64
	MaxTopicLen    = 256
	MaxDisplayName = 64
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func ValidateUsername(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > MaxUsernameLen {
		return "username must be 2-32 characters"
	}
	if !usernameRegex.MatchString(name) {
		return "username can only contain letters, numbers, _ and -"
	}
	return ""
}

func ValidatePassword(pw string) string {
	if len(pw) < 8 {
		return "password must be at least 8 characters"
	}
	if len(pw) > 128 {
		return "password must be at most 128 characters"
	}
	return ""
}

func ValidateChannelName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > MaxChannelLen {
		return "channel name must be 1-64 characters"
	}
	return ""
}

func ValidateCategoryName(name string) string {
	name = strings.TrimSpace(name)
	if len(name) < 1 || len(name) > MaxChannelLen {
		return "category name must be 1-64 characters"
	}
	return ""
}

func ValidateMessageContent(content string) string {
	if len(strings.TrimSpace(content)) == 0 {
		return "message cannot be empty"
	}
	if len(content) > MaxMessageLen {
		return "message must be at most 4000 characters"
	}
	return ""
}
