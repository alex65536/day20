package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/userauth"
	_ "github.com/alex65536/day20/internal/util/gormutil"
	"github.com/alex65536/day20/internal/util/sliceutil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/day20/internal/util/timeutil"
	"github.com/alex65536/day20/internal/webui"
	"github.com/alex65536/go-chess/util/maybe"
	"github.com/gorilla/sessions"
	"github.com/wader/gormstore/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Options struct {
	Path          string        `toml:"path"`
	Debug         bool          `toml:"debug"`
	SlowThreshold time.Duration `toml:"slow-threshold"`
	BusyTimeout   time.Duration `toml:"busy-timeout"`
	UseWAL        bool          `toml:"use-wal"`
}

func (o *Options) FillDefaults() {
	if o.SlowThreshold == 0 {
		o.SlowThreshold = 200 * time.Millisecond
	}
	if o.BusyTimeout == 0 {
		o.BusyTimeout = 1 * time.Minute
	}
}

type DB struct {
	db  *gorm.DB
	log *slog.Logger
}

var (
	_ roomkeeper.DB             = (*DB)(nil)
	_ userauth.DB               = (*DB)(nil)
	_ webui.SessionStoreFactory = (*DB)(nil)
)

func (d *DB) Close() {
	db, err := d.db.DB()
	if err != nil {
		d.log.Error("could not get underlying db", slogx.Err(err))
	}
	err = db.Close()
	if err != nil {
		d.log.Error("could not close db", slogx.Err(err))
	}
}

func buildPath(o Options) string {
	var params []string
	if o.UseWAL {
		params = append(params, "_journal_mode=WAL")
		params = append(params, "_synchronous=NORMAL")
	}
	params = append(params, fmt.Sprintf("_busy_timeout=%v", o.BusyTimeout.Milliseconds()))
	params = append(params, "_foreign_keys=1")
	paramStr := strings.Join(params, "&")
	if paramStr == "" {
		return o.Path
	}
	return o.Path + "?" + paramStr
}

func New(log *slog.Logger, o Options) (*DB, error) {
	o.FillDefaults()

	log.Info("opening db")
	db, err := gorm.Open(sqlite.Open(buildPath(o)), &gorm.Config{
		Logger: Logger(log, o),
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{db: db, log: log}

	log.Info("migrating db")
	if err := db.AutoMigrate(models...); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	log.Info("db opened")
	return d, nil
}

func (d *DB) CreateRunningJob(ctx context.Context, job roomapi.Job) error {
	err := d.db.WithContext(ctx).Create(&RunningJob{
		Job: job,
	}).Error
	if err != nil {
		return fmt.Errorf("create running job: %w", err)
	}
	return nil
}

func (d *DB) FinishRunningJob(ctx context.Context, jobID string, data FinishedJobData) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var job RunningJob
		err := tx.Where("id = ?", jobID).First(&job).Error
		if err != nil {
			return fmt.Errorf("find running job: %w", err)
		}
		err = tx.Delete(job).Error
		if err != nil {
			return fmt.Errorf("delete old job: %w", err)
		}
		err = tx.Create(&FinishedJob{
			Job:  job.Job,
			Data: data,
		}).Error
		if err != nil {
			return fmt.Errorf("create finished job: %w", err)
		}
		return nil
	})
}

func (d *DB) ListActiveRooms(ctx context.Context) ([]roomkeeper.RoomFullData, error) {
	var res []Room
	err := d.db.WithContext(ctx).Model(&Room{}).Joins("Job").Find(&res).Error
	if err != nil {
		return nil, fmt.Errorf("list active rooms: %w", err)
	}
	data := sliceutil.Map(res, func(r Room) roomkeeper.RoomFullData {
		return roomkeeper.RoomFullData{
			Info: r.Info,
			Job:  &r.Job.Job,
		}
	})
	return data, nil
}

func (d *DB) CreateRoom(ctx context.Context, info roomkeeper.RoomInfo) error {
	err := d.db.WithContext(ctx).Create(&Room{
		Info:  info,
		JobID: nil,
	}).Error
	if err != nil {
		return fmt.Errorf("create room: %w", err)
	}
	return nil
}

func (d *DB) UpdateRoom(ctx context.Context, roomID string, jobID maybe.Maybe[string]) error {
	var jobIDPtr *string
	if j, ok := jobID.TryGet(); ok {
		jobIDPtr = &j
	}
	err := d.db.WithContext(ctx).Model(&Room{}).Where("id = ?", roomID).Update("job_id", jobIDPtr).Error
	if err != nil {
		return fmt.Errorf("update room: %w", err)
	}
	return nil
}

func (d *DB) StopRoom(ctx context.Context, roomID string) error {
	err := d.db.WithContext(ctx).Delete(&Room{
		Info: roomkeeper.RoomInfo{ID: roomID},
	}).Error
	if err != nil {
		return fmt.Errorf("delete room: %w", err)
	}
	return nil
}

func (d *DB) CreateUser(ctx context.Context, user userauth.User, link userauth.InviteLink) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var result []userauth.User
		err := tx.Where("username = ?", user.Username).Limit(1).Find(&result).Error
		if err != nil {
			return fmt.Errorf("search for user: %w", err)
		}
		if len(result) != 0 {
			return userauth.ErrUserAlreadyExists
		}
		err = tx.Create(&user).Error
		if err != nil {
			return fmt.Errorf("create user: %w", err)
		}
		delTx := tx.Delete(link)
		err = delTx.Error
		if err != nil {
			return fmt.Errorf("delete link: %w", err)
		}
		if delTx.RowsAffected == 0 {
			return userauth.ErrInviteLinkUsed
		}
		return nil
	})
}

func (d *DB) applyUserOptions(tx *gorm.DB, os ...userauth.GetUserOptions) *gorm.DB {
	if len(os) > 1 {
		panic("too many options")
	}
	if len(os) == 1 {
		o := os[0]
		if o.WithInviteLinks {
			tx = tx.Preload("InviteLinks")
		}
		if o.WithRoomTokens {
			tx = tx.Preload("RoomTokens")
		}
	}
	return tx
}

func (d *DB) GetUser(ctx context.Context, userID string, o ...userauth.GetUserOptions) (userauth.User, error) {
	var users []userauth.User
	tx := d.applyUserOptions(d.db.WithContext(ctx), o...)
	err := tx.Where("id = ?", userID).Limit(1).Find(&users).Error
	if err != nil {
		return userauth.User{}, fmt.Errorf("get user: %w", err)
	}
	if len(users) == 0 {
		return userauth.User{}, userauth.ErrUserNotFound
	}
	return users[0], nil
}

func (d *DB) GetUserByUsername(ctx context.Context, username string, o ...userauth.GetUserOptions) (userauth.User, error) {
	var users []userauth.User
	tx := d.applyUserOptions(d.db.WithContext(ctx), o...)
	err := tx.Where("username = ?", username).Limit(1).Find(&users).Error
	if err != nil {
		return userauth.User{}, fmt.Errorf("get user: %w", err)
	}
	if len(users) == 0 {
		return userauth.User{}, userauth.ErrUserNotFound
	}
	return users[0], nil
}

func (d *DB) UpdateUser(ctx context.Context, user userauth.User, srcO ...userauth.UpdateUserOptions) error {
	var o userauth.UpdateUserOptions
	if len(srcO) > 1 {
		panic("too many options")
	}
	if len(srcO) == 1 {
		o = srcO[0]
	}

	if o == (userauth.UpdateUserOptions{}) {
		err := d.db.WithContext(ctx).Save(&user).Error
		if err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		return nil
	}

	return d.db.WithContext(ctx).Transaction(func (tx *gorm.DB) error {
		if err := tx.Save(&user).Error; err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		if !user.Perms.Get(userauth.PermInvite) {
			err := tx.Where("owner_user_id = ?", user.ID).Delete(&userauth.InviteLink{}).Error
			if err != nil {
				return fmt.Errorf("delete invite links: %w", err)
			}
		}
		if !user.Perms.Get(userauth.PermHostRooms) {
			err := tx.Where("user_id = ?", user.ID).Delete(&userauth.RoomToken{}).Error
			if err != nil {
				return fmt.Errorf("delete room tokens: %w", err)
			}
		}
		return nil
	})
}

func (d *DB) CountUsers(ctx context.Context) (int64, error) {
	var cnt int64
	err := d.db.WithContext(ctx).Model(&userauth.User{}).Count(&cnt).Error
	if err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return cnt, nil
}

func (d *DB) ListUsers(ctx context.Context) ([]userauth.User, error) {
	var users []userauth.User
	err := d.db.WithContext(ctx).Find(&users).Error
	if err != nil {
		return nil, fmt.Errorf("get users: %w", err)
	}
	return users, nil
}

func (d *DB) CreateInviteLink(ctx context.Context, link userauth.InviteLink) error {
	err := d.db.WithContext(ctx).Create(&link).Error
	if err != nil {
		return fmt.Errorf("create invite link: %w", err)
	}
	return nil
}

func (d *DB) GetInviteLink(ctx context.Context, linkHash string, now timeutil.UTCTime) (userauth.InviteLink, error) {
	var link userauth.InviteLink
	err := d.db.WithContext(ctx).Model(&link).Where("hash = ? AND expires_at >= ?", linkHash, now).First(&link).Error
	if err != nil {
		return userauth.InviteLink{}, fmt.Errorf("get invite link: %w", err)
	}
	return link, nil
}

func (d *DB) PruneInviteLinks(ctx context.Context, now timeutil.UTCTime) error {
	err := d.db.WithContext(ctx).Delete(&userauth.InviteLink{}, "expires_at < ?", now).Error
	if err != nil {
		return fmt.Errorf("prune invite links: %w", err)
	}
	return nil
}

func (d *DB) DeleteInviteLink(ctx context.Context, linkHash string) error {
	err := d.db.WithContext(ctx).Delete(&userauth.InviteLink{Hash: linkHash}).Error
	if err != nil {
		return fmt.Errorf("delete invite link: %w", err)
	}
	return nil
}

func (d *DB) CreateRoomToken(ctx context.Context, token userauth.RoomToken) error {
	err := d.db.WithContext(ctx).Create(&token).Error
	if err != nil {
		return fmt.Errorf("create room token: %w", err)
	}
	return nil
}

func (d *DB) DeleteRoomToken(ctx context.Context, tokenHash string) error {
	err := d.db.WithContext(ctx).Delete(&userauth.RoomToken{Hash: tokenHash}).Error
	if err != nil {
		return fmt.Errorf("delete room token: %w", err)
	}
	return nil
}

func (d *DB) NewSessionStore(ctx context.Context, opts webui.SessionOptions) sessions.Store {
	s := gormstore.New(d.db, opts.Key)
	go s.PeriodicCleanup(opts.CleanupInterval, ctx.Done())
	return s
}
