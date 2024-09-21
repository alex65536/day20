package userauth

import (
	"fmt"
)

func ValidatePassword(password string) error {
	if len(password) < 8 || len(password) > 64 {
		return fmt.Errorf("password must have from 8 to 64 characters")
	}
	return nil
}

func ValidateUsername(username string) error {
	if len(username) < 3 || len(username) > 64 {
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
