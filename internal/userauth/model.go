package userauth

import (
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"

	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/timeutil"
	"golang.org/x/crypto/argon2"
)

type PasswordOptions struct {
	Time    uint32 `toml:"time"`
	Memory  uint32 `toml:"memory"`
	Threads uint8  `toml:"threads"`
	KeyLen  uint32 `toml:"key-len"`
	SaltLen uint32 `toml:"salt-len"`
}

var defaultPasswordOptions = &PasswordOptions{
	Time:    3,
	Memory:  16384,
	Threads: 1,
	KeyLen:  32,
	SaltLen: 32,
}

type PermKind int

const (
	PermInvite PermKind = iota
	PermDiscuss
	PermRunContests
	PermHostRooms
	PermAdmin
	PermMax
)

func (k PermKind) String() string {
	switch k {
	case PermInvite:
		return "invite"
	case PermDiscuss:
		return "discuss"
	case PermRunContests:
		return "run-contests"
	case PermHostRooms:
		return "host-rooms"
	case PermAdmin:
		return "admin"
	default:
		panic("bad perm")
	}
}

func (k PermKind) PrettyString() string {
	switch k {
	case PermInvite:
		return "Invite"
	case PermDiscuss:
		return "Discuss"
	case PermRunContests:
		return "Run contests"
	case PermHostRooms:
		return "Host rooms"
	case PermAdmin:
		return "Admin"
	default:
		panic("bad perm")
	}
}

type Perms struct {
	IsOwner   bool
	IsBlocked bool

	CanInvite      bool
	CanDiscuss     bool
	CanRunContests bool
	CanHostRooms   bool
	CanAdmin       bool
}

func (p *Perms) GetMut(k PermKind) *bool {
	switch k {
	case PermInvite:
		return &p.CanInvite
	case PermDiscuss:
		return &p.CanDiscuss
	case PermRunContests:
		return &p.CanRunContests
	case PermHostRooms:
		return &p.CanHostRooms
	case PermAdmin:
		return &p.CanAdmin
	default:
		panic("bad perm to get")
	}
}

func (p Perms) Get(k PermKind) bool {
	if p.IsBlocked {
		return false
	}
	if p.IsOwner {
		return true
	}
	return *p.GetMut(k)
}

func OwnerPerms() Perms {
	return Perms{
		IsOwner:        true,
		CanInvite:      true,
		CanDiscuss:     true,
		CanRunContests: true,
		CanHostRooms:   true,
		CanAdmin:       true,
	}
}

func BlockedPerms() Perms {
	return Perms{
		IsBlocked: true,
	}
}

func (p Perms) LessEq(q Perms) bool {
	if p.IsBlocked || q.IsBlocked {
		return p.IsBlocked
	}
	for k := range PermMax {
		if p.Get(k) && !q.Get(k) {
			return false
		}
	}
	return true
}

type User struct {
	ID           string  `gorm:"primaryKey"`
	Username     string  `gorm:"index"`
	InviterID    *string `gorm:"index"`
	PasswordHash []byte
	PasswordSalt []byte
	Epoch        int
	Perms        Perms        `gorm:"embedded"`
	RoomTokens   []RoomToken  `gorm:"foreignKey:UserID"`
	InviteLinks  []InviteLink `gorm:"foreignKey:OwnerUserID"`
}

func (u *User) doHash(password []byte, o *PasswordOptions) []byte {
	return argon2.IDKey(password, u.PasswordSalt, o.Time, o.Memory, o.Threads, o.KeyLen)
}

func (u *User) SetPassword(password []byte, o *PasswordOptions) error {
	if o == nil {
		o = defaultPasswordOptions
	}

	salt := make([]byte, o.SaltLen)
	_, err := io.ReadFull(crand.Reader, salt)
	if err != nil {
		return fmt.Errorf("generate salt: %w", err)
	}

	u.PasswordSalt = salt
	u.PasswordHash = u.doHash(password, o)
	u.Epoch++
	return nil
}

func (u *User) VerifyPassword(password []byte, o *PasswordOptions) bool {
	if o == nil {
		o = defaultPasswordOptions
	}
	hash := u.doHash(password, o)
	return subtle.ConstantTimeCompare(hash, u.PasswordHash) == 1
}

type InviteLink struct {
	Hash        string  `gorm:"primaryKey"`
	OwnerUserID *string `gorm:"index"`
	Name        string
	Value       string
	Perms       Perms `gorm:"embedded"`
	CreatedAt   timeutil.UTCTime
	ExpiresAt   timeutil.UTCTime `gorm:"index"`
}

func HashInviteValue(val string) string {
	hash := sha256.Sum256([]byte(val))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func (l *InviteLink) GenerateNew() error {
	val, err := idgen.SecureLinkValue()
	if err != nil {
		return fmt.Errorf("generate invite value: %w", err)
	}
	l.Value = val
	l.Hash = HashInviteValue(val)
	return nil
}

type RoomToken struct {
	Hash   string `gorm:"primaryKey"`
	Name   string
	UserID string `gorm:"index"`
}

func HashRoomToken(tok string) string {
	hash := sha256.Sum256([]byte(tok))
	return base64.StdEncoding.EncodeToString(hash[:])
}

func (t *RoomToken) GenerateNew() (string, error) {
	tok, err := idgen.SecureToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	t.Hash = HashRoomToken(tok)
	return tok, nil
}

func (u *User) CanChangePerms(initiator *User, newPerms Perms) error {
	// Reset all the other perms if we are going to block the user.
	if newPerms.IsBlocked {
		newPerms = BlockedPerms()
	}

	// Can God create a stone so heavy that He cannot lift it?
	// Can the owner change his own permissions?
	// Here, the answer is "no one can change owner's permissions, including the owner himself".
	// That simple ;)
	if u.Perms.IsOwner {
		return fmt.Errorf("cannot change the owner's permissions")
	}

	// Cannot change anyone's perms to owner.
	if newPerms.IsOwner {
		return fmt.Errorf("cannot make anyone owner")
	}

	// Owner can do anything with everyone (excluding himself, but handled above).
	if initiator.Perms.IsOwner {
		return nil
	}

	// Non-admin users cannot do anything.
	if !initiator.Perms.Get(PermAdmin) {
		return fmt.Errorf("insufficient privilege for this operation")
	}

	// Special case: admin changes his own perms.
	// This is allowed as soon as he remains admin and doesn't try to ban himself.
	if initiator.ID == u.ID {
		if !newPerms.Get(PermAdmin) {
			return fmt.Errorf("cannot downgrade yourself from admin")
		}
		return nil
	}

	// Admin users cannot do anything with other admins or make others admins.
	if u.Perms.Get(PermAdmin) || newPerms.Get(PermAdmin) {
		return fmt.Errorf("insufficient privilege for this operation")
	}

	// All checks passed.
	return nil
}

func (l InviteLink) Verify(creator *User) error {
	// Special cases: IsOwner and IsBlocked are not allowed in invite links.
	if l.Perms.IsOwner {
		return fmt.Errorf("cannot create invite link for owner")
	}
	if l.Perms.IsBlocked {
		return fmt.Errorf("cannot create invite link for blocked user")
	}

	// You can invite only if you have invite perms.
	if !creator.Perms.Get(PermInvite) {
		return fmt.Errorf("this user cannot create invite links")
	}

	// Admins cannot invite admins (only owner can).
	if l.Perms.Get(PermAdmin) && !creator.Perms.IsOwner {
		return fmt.Errorf("only owner can invite admins")
	}

	// Invite link must not be greater than the ones you have yourself.
	if !l.Perms.LessEq(creator.Perms) {
		return fmt.Errorf("cannot create invite link with greater perms than yourself")
	}

	return nil
}
