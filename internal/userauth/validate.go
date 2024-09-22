package userauth

import (
	"fmt"
	"unicode/utf8"
)

func ValidatePassword(password string) error {
	pwLen := utf8.RuneCountInString(password)
	if pwLen < 8 || pwLen > 64 {
		return fmt.Errorf("password must have from 8 to 64 characters")
	}
	return nil
}

func ValidateUsername(username string) error {
	uLen := utf8.RuneCountInString(username)
	if uLen < 3 || uLen > 64 {
		return fmt.Errorf("username must have from 3 to 64 characters")
	}
	for _, c := range username {
		if !(('a' <= c && c <= 'z') ||
			('A' <= c && c <= 'Z') ||
			('0' <= c && c <= '9') ||
			c == '_' || c == '-') {
			return fmt.Errorf("allowed characters in username: A-Z, a-z, 0-9, -, _")
		}
	}
	return nil
}
