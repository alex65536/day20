package userauth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/alex65536/day20/internal/util/clone"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/timeutil"
)

type ErrorInviteLinkVerify struct {
	e error
}

func (e *ErrorInviteLinkVerify) Unwrap() error { return e.e }
func (e *ErrorInviteLinkVerify) Error() string { return fmt.Sprintf("verify invite link: %v", e.e) }

type ManagerOptions struct {
	GCInterval       time.Duration    `toml:"gc-interval"`
	LinkPrefix       string           `toml:"link-prefix"`
	Password         *PasswordOptions `toml:"password"`
	InviteLinkExpiry time.Duration    `toml:"invite-link-expiry"`
}

func (o ManagerOptions) Clone() ManagerOptions {
	o.Password = clone.TrivialPtr(o.Password)
	return o
}

func (o *ManagerOptions) FillDefaults() {
	if o.GCInterval == 0 {
		o.GCInterval = 5 * time.Minute
	}
	if o.InviteLinkExpiry == 0 {
		o.InviteLinkExpiry = 1 * time.Hour
	}
}

type Manager struct {
	DB
	o      *ManagerOptions
	log    *slog.Logger
	ctx    context.Context
	cancel func()
	done   chan struct{}
}

func NewManager(log *slog.Logger, db DB, o ManagerOptions) (*Manager, error) {
	o = o.Clone()
	o.FillDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	hasOwner, err := db.HasOwnerUser(ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("check for owner user: %w", err)
	}
	m := &Manager{
		DB:     db,
		o:      &o,
		log:    log,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if !hasOwner {
		link, err := m.doGenerateInviteLink(m.ctx, "invite for owner", nil, OwnerPerms(), false)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("create first invite: %w", err)
		}
		log.Warn("owner has not been created yet, follow the invite link to create it",
			slog.String("url", m.InviteLinkURL(link)))
	}
	go m.loop()
	return m, nil
}

func (m *Manager) Close() {
	m.cancel()
	<-m.done
}

func (m *Manager) doGenerateInviteLink(ctx context.Context, name string, creator *User, perms Perms, verify bool) (InviteLink, error) {
	now := timeutil.NowUTC()
	var ownerUserID *string
	if creator != nil {
		ownerUserID = clone.TrivialPtr(&creator.ID)
	}
	link := InviteLink{
		OwnerUserID: ownerUserID,
		Perms:       perms,
		Name:        name,
		CreatedAt:   now,
		ExpiresAt:   now.Add(m.o.InviteLinkExpiry),
	}
	if verify {
		if ownerUserID == nil {
			return InviteLink{}, &ErrorInviteLinkVerify{
				e: fmt.Errorf("no owner user"),
			}
		}
		if err := link.Verify(creator); err != nil {
			return InviteLink{}, &ErrorInviteLinkVerify{e: err}
		}
	}
	if err := link.GenerateNew(); err != nil {
		return InviteLink{}, fmt.Errorf("generate: %w", err)
	}
	if err := m.CreateInviteLink(ctx, link); err != nil {
		return InviteLink{}, fmt.Errorf("save to db: %w", err)
	}
	return link, nil
}

func (m *Manager) GenerateInviteLink(ctx context.Context, name string, creator *User, perms Perms) (InviteLink, error) {
	return m.doGenerateInviteLink(ctx, name, creator, perms, true)
}

func (m *Manager) InviteLinkURL(l InviteLink) string {
	return m.o.LinkPrefix + l.Value
}

func (m *Manager) SetPassword(u *User, password []byte) error {
	return u.SetPassword(password, m.o.Password)
}

func (m *Manager) VerifyPassword(u *User, password []byte) bool {
	return u.VerifyPassword(password, m.o.Password)
}

func (m *Manager) loop() {
	defer close(m.done)
	ticker := time.NewTicker(m.o.GCInterval)
	defer ticker.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
			err := m.DB.PruneInviteLinks(m.ctx, timeutil.NowUTC())
			if err != nil && !errors.Is(err, context.Canceled) {
				m.log.Warn("could not prune invite links", slogx.Err(err))
			}
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
}
