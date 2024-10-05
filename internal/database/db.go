package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/scheduler"
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
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type Options struct {
	Path          string        `toml:"path"`
	Debug         bool          `toml:"debug"`
	SlowThreshold time.Duration `toml:"slow-threshold"`
	BusyTimeout   time.Duration `toml:"busy-timeout"`
	NoUseWAL      bool          `toml:"no-use-wal"`
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

	contestDataCols []string
	matchDataCols   []string
}

var (
	_ roomkeeper.DB             = (*DB)(nil)
	_ userauth.DB               = (*DB)(nil)
	_ webui.SessionStoreFactory = (*DB)(nil)
	_ scheduler.DB              = (*DB)(nil)
)

func (d *DB) Close() {
	db, err := d.db.DB()
	if err != nil {
		d.log.Error("could not get underlying db", slogx.Err(err))
		return
	}
	err = db.Close()
	if err != nil {
		d.log.Error("could not close db", slogx.Err(err))
	}
}

func buildPath(o Options) string {
	var params []string
	if !o.NoUseWAL {
		params = append(params, "_journal_mode=WAL")
		params = append(params, "_synchronous=NORMAL")
	}
	params = append(params, fmt.Sprintf("_busy_timeout=%v", o.BusyTimeout.Milliseconds()))
	params = append(params, "_foreign_keys=1")
	params = append(params, "_txlock=immediate")
	paramStr := strings.Join(params, "&")
	if paramStr == "" {
		return o.Path
	}
	return o.Path + "?" + paramStr
}

func (d *DB) doParseColumns(model any, store *sync.Map) ([]string, error) {
	s, err := schema.Parse(model, store, d.db.NamingStrategy)
	if err != nil {
		return nil, fmt.Errorf("parse schema: %w", err)
	}
	return sliceutil.FilterMap(s.Fields, func(f *schema.Field) (string, bool) {
		return f.DBName, f.DBName != ""
	}), nil
}

func (d *DB) parseColumns() error {
	store := &sync.Map{}
	var err error
	d.contestDataCols, err = d.doParseColumns(&scheduler.ContestData{}, store)
	if err != nil {
		return fmt.Errorf("parse ContestData: %w", err)
	}
	d.matchDataCols, err = d.doParseColumns(&scheduler.MatchData{}, store)
	if err != nil {
		return fmt.Errorf("parse MatchData: %w", err)
	}
	return nil
}

func New(log *slog.Logger, o Options) (*DB, error) {
	o.FillDefaults()

	if o.Path == "" {
		return nil, fmt.Errorf("no path to db")
	}

	log.Info("opening db")
	db, err := gorm.Open(sqlite.Open(buildPath(o)), &gorm.Config{
		Logger: Logger(log, o),
	})
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	d := &DB{db: db, log: log}

	if err := d.parseColumns(); err != nil {
		d.Close()
		return nil, fmt.Errorf("parse columns: %w", err)
	}

	log.Info("migrating db")
	if err := db.AutoMigrate(models...); err != nil {
		d.Close()
		return nil, fmt.Errorf("migrate db: %w", err)
	}

	log.Info("db opened")
	return d, nil
}

func (d *DB) ListActiveRooms(ctx context.Context) ([]roomkeeper.RoomFullData, error) {
	var res []Room
	err := d.db.WithContext(ctx).Model(&Room{}).Joins("Job").Find(&res).Error
	if err != nil {
		return nil, fmt.Errorf("list active rooms: %w", err)
	}
	data := sliceutil.Map(res, func(r Room) roomkeeper.RoomFullData {
		var job *roomapi.Job
		if r.JobID != nil {
			job = &r.Job.Job
		}
		return roomkeeper.RoomFullData{
			Info: r.Info,
			Job:  job,
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
		err := d.db.WithContext(ctx).Select("*").Updates(&user).Error
		if err != nil {
			return fmt.Errorf("update user: %w", err)
		}
		return nil
	}

	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Select("*").Updates(&user).Error; err != nil {
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

func (d *DB) HasOwnerUser(ctx context.Context) (bool, error) {
	var users []userauth.User
	err := d.db.WithContext(ctx).Limit(1).Find(&users).Error
	if err != nil {
		return false, fmt.Errorf("check for owner user: %w", err)
	}
	return len(users) == 1, nil
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

func (d *DB) DeleteInviteLink(ctx context.Context, linkHash string, userID string) error {
	err := d.db.WithContext(ctx).Where("owner_user_id = ?", userID).Delete(&userauth.InviteLink{Hash: linkHash}).Error
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

func (d *DB) GetRoomToken(ctx context.Context, hash string) (userauth.RoomToken, error) {
	var tokens []userauth.RoomToken
	err := d.db.WithContext(ctx).Limit(1).Where("hash = ?", hash).Limit(1).Find(&tokens).Error
	if err != nil {
		return userauth.RoomToken{}, fmt.Errorf("get room token: %w", err)
	}
	if len(tokens) == 0 {
		return userauth.RoomToken{}, userauth.ErrRoomTokenNotFound
	}
	return tokens[0], nil
}

func (d *DB) DeleteRoomToken(ctx context.Context, tokenHash string, userID string) error {
	err := d.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&userauth.RoomToken{Hash: tokenHash}).Error
	if err != nil {
		return fmt.Errorf("delete room token: %w", err)
	}
	return nil
}

func (d *DB) NewSessionStore(ctx context.Context, opts webui.SessionOptions) sessions.Store {
	s := gormstore.New(d.db, opts.Key)
	opts.AssignSessionOptions(s.SessionOpts)
	go s.PeriodicCleanup(opts.CleanupInterval, ctx.Done())
	return s
}

func (d *DB) buildContestFullData(c Contest) scheduler.ContestFullData {
	if c.Match != nil {
		c.Info.Match = &c.Match.Settings
		c.Data.Match = &c.Match.Data
	}
	return scheduler.ContestFullData{
		Info: c.Info,
		Data: c.Data,
	}
}

func (d *DB) ListContests(ctx context.Context) ([]scheduler.ContestFullData, error) {
	var contests []Contest
	err := d.db.WithContext(ctx).Preload("Match").Find(&contests).Error
	if err != nil {
		return nil, fmt.Errorf("list running contests: %w", err)
	}
	return sliceutil.Map(contests, d.buildContestFullData), nil
}

func (d *DB) ListRunningContestsFull(ctx context.Context) ([]scheduler.ContestFullData, error) {
	var contests []Contest
	err := d.db.WithContext(ctx).Preload("Match").
		Where("status_kind = ?", scheduler.ContestRunning).
		Find(&contests).Error
	if err != nil {
		return nil, fmt.Errorf("list running contests: %w", err)
	}
	return sliceutil.Map(contests, d.buildContestFullData), nil
}

func (d *DB) ListRunningJobs(ctx context.Context) ([]scheduler.RunningJob, error) {
	var jobs []scheduler.RunningJob
	err := d.db.WithContext(ctx).Model(&scheduler.RunningJob{}).Find(&jobs).Error
	if err != nil {
		return nil, fmt.Errorf("list running jobs: %w", err)
	}
	return jobs, nil
}

func (d *DB) CreateContest(ctx context.Context, info scheduler.ContestInfo, data scheduler.ContestData) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var match *Match
		if info.Match != nil {
			match = &Match{
				ContestID: info.ID,
				Settings:  *info.Match,
				Data:      *data.Match,
			}
			err := tx.Create(match).Error
			if err != nil {
				return fmt.Errorf("create match: %w", err)
			}
		}
		err := tx.Create(&Contest{
			Info:  info,
			Data:  data,
			Match: match,
		}).Error
		if err != nil {
			return fmt.Errorf("create contest: %w", err)
		}
		return nil
	})
}

func (d *DB) UpdateContest(ctx context.Context, contestID string, data scheduler.ContestData) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if data.Match != nil {
			err := tx.Select(d.matchDataCols).Where("contest_id = ?", contestID).
				Updates(&Match{Data: *data.Match}).Error
			if err != nil {
				return fmt.Errorf("update match: %w", err)
			}
		}
		err := tx.Select(d.contestDataCols).Where("id = ?", contestID).
			Updates(&Contest{Data: data}).Error
		if err != nil {
			return fmt.Errorf("update contest: %w", err)
		}
		return nil
	})
}

func (d *DB) GetContest(ctx context.Context, contestID string) (scheduler.ContestInfo, scheduler.ContestData, error) {
	var contests []Contest
	err := d.db.WithContext(ctx).Preload("Match").Where("id = ?", contestID).Limit(1).Find(&contests).Error
	if err != nil {
		return scheduler.ContestInfo{}, scheduler.ContestData{}, fmt.Errorf("get contest: %w", err)
	}
	if len(contests) == 0 {
		return scheduler.ContestInfo{}, scheduler.ContestData{}, scheduler.ErrNoSuchContest
	}
	fullData := d.buildContestFullData(contests[0])
	return fullData.Info, fullData.Data, nil
}

func (d *DB) CreateRunningJob(ctx context.Context, job *scheduler.RunningJob) error {
	err := d.db.WithContext(ctx).Create(job).Error
	if err != nil {
		return fmt.Errorf("create running job: %w", err)
	}
	return nil
}

func (d *DB) FinishRunningJob(ctx context.Context, job *scheduler.FinishedJob) error {
	return d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		delTx := tx.Where("id = ?", job.Job.ID).Delete(&scheduler.RunningJob{})
		if delTx.RowsAffected == 0 {
			d.log.Warn("trying to finish the job that was never running",
				slog.String("job_id", job.Job.ID),
			)
		}
		if err := delTx.Error; err != nil {
			return fmt.Errorf("delete running job: %w", err)
		}
		if err := tx.Create(job).Error; err != nil {
			return fmt.Errorf("create finished job: %w", err)
		}
		return nil
	})
}

func (d *DB) ListContestSucceededJobs(ctx context.Context, contestID string) ([]scheduler.FinishedJob, error) {
	var jobs []scheduler.FinishedJob
	err := d.db.WithContext(ctx).Where("contest_id = ? AND status_kind = ?", contestID, roomkeeper.JobSucceeded).
		Order([]clause.OrderByColumn{
			{Column: clause.Column{Name: "index"}},
			{Column: clause.Column{Name: "job_id"}},
		}).Find(&jobs).Error
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	return jobs, nil
}
