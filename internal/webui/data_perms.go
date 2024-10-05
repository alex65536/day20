package webui

import (
	"github.com/alex65536/day20/internal/userauth"
)

type permsDataItem struct {
	Kind   userauth.PermKind
	Active bool
}

type permsData struct {
	IsOwner   bool
	IsBlocked bool
	Perms     []permsDataItem
}

func buildPermsData(p userauth.Perms) *permsData {
	perms := make([]permsDataItem, 0, userauth.PermMax)
	for perm := range userauth.PermMax {
		perms = append(perms, permsDataItem{
			Kind:   perm,
			Active: p.Get(perm),
		})
	}
	return &permsData{
		IsOwner:   p.IsOwner,
		IsBlocked: p.IsBlocked,
		Perms:     perms,
	}
}
