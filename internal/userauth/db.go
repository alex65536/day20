package userauth

import (
	"context"
	"errors"

	"github.com/alex65536/day20/internal/util/timeutil"
)

var (
	ErrInviteLinkUsed    = errors.New("invite link already used")
	ErrUserAlreadyExists = errors.New("user with such username already exists")
	ErrUserNotFound      = errors.New("user not found")
)

type GetUserOptions struct {
	WithInviteLinks bool
	WithRoomTokens  bool
}

type UpdateUserOptions struct {
	InvalidatePerms bool
}

type DB interface {
	CreateUser(ctx context.Context, user User, link InviteLink) error
	GetUser(ctx context.Context, userID string, o ...GetUserOptions) (User, error)
	GetUserByUsername(ctx context.Context, username string, o ...GetUserOptions) (User, error)
	ListUsers(ctx context.Context) ([]User, error)
	UpdateUser(ctx context.Context, user User, o ...UpdateUserOptions) error
	HasOwnerUser(ctx context.Context) (bool, error)
	CreateInviteLink(ctx context.Context, link InviteLink) error
	GetInviteLink(ctx context.Context, linkHash string, now timeutil.UTCTime) (InviteLink, error)
	PruneInviteLinks(ctx context.Context, now timeutil.UTCTime) error
	DeleteInviteLink(ctx context.Context, linkHash string) error
	CreateRoomToken(ctx context.Context, token RoomToken) error
	DeleteRoomToken(ctx context.Context, tokenHash string) error
}
