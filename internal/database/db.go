package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	_ "github.com/alex65536/day20/internal/util/gormutil"
	"github.com/alex65536/day20/internal/util/sliceutil"
	"github.com/alex65536/day20/internal/util/slogx"
	"github.com/alex65536/go-chess/util/maybe"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type Options struct {
	Path             string        `toml:"path"`
	Debug            bool          `toml:"debug"`
	SlowThreshold    time.Duration `toml:"slow-threshold"`
	BusyTimeout      time.Duration `toml:"busy-timeout"`
	UseWAL           bool          `toml:"use-wal"`
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

var _ roomkeeper.DB = (*DB)(nil)

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
		err := tx.Where("id = ?", jobID).Find(&job).Error
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
	err := d.db.WithContext(ctx).Model(&Room{}).Joins("Job").Scan(&res).Error
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
