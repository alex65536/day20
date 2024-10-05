package webui

import (
	"github.com/alex65536/day20/internal/userauth"
)

type userPartData struct {
	Username string
	Perms    *permsData
}

func buildUserPartData(user userauth.User) *userPartData {
	return &userPartData{
		Username: user.Username,
		Perms:    buildPermsData(user.Perms),
	}
}
