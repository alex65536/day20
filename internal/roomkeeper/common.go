package roomkeeper

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/go-chess/util/maybe"
)

var ErrGameNotReady = errors.New("game not ready")

type JobStatusKind int

const (
	JobUnknown JobStatusKind = iota
	JobRunning
	JobSucceeded
	JobAborted
)

func (k JobStatusKind) String() string {
	switch k {
	case JobUnknown:
		return "unknown"
	case JobRunning:
		return "running"
	case JobSucceeded:
		return "success"
	case JobAborted:
		return "abort"
	default:
		return "bad"
	}
}

func (k JobStatusKind) IsFinished() bool {
	return k == JobSucceeded || k == JobAborted
}

type JobStatus struct {
	Kind   JobStatusKind `gorm:"index"`
	Reason string
}

func (s JobStatus) String() string {
	return fmt.Sprintf("%v(%q)", s.Kind, s.Reason)
}

func NewStatusUnknown() JobStatus   { return JobStatus{Kind: JobUnknown} }
func NewStatusRunning() JobStatus   { return JobStatus{Kind: JobRunning} }
func NewStatusSucceeded() JobStatus { return JobStatus{Kind: JobSucceeded} }
func NewStatusAborted(reason string) JobStatus {
	return JobStatus{
		Kind:   JobAborted,
		Reason: reason,
	}
}

type RoomInfo struct {
	ID   string `gorm:"primaryKey"`
	Name string
}

type RoomFullData struct {
	Info RoomInfo
	Job  *roomapi.Job
}

type DB interface {
	ListActiveRooms(ctx context.Context) ([]RoomFullData, error)
	CreateRoom(ctx context.Context, info RoomInfo) error
	UpdateRoom(ctx context.Context, roomID string, jobID maybe.Maybe[string]) error
	StopRoom(ctx context.Context, roomID string) error
}

type Scheduler interface {
	IsJobAborted(jobID string) (string, bool)
	NextJob(ctx context.Context) (*roomapi.Job, error)
	OnJobFinished(jobID string, status JobStatus, game *battle.GameExt)
}

type Options struct {
	MaxJobFetchTimeout  time.Duration `toml:"max-job-fetch-timeout"`
	RoomLivenessTimeout time.Duration `toml:"room-liveness-timeout"`
	GCInterval          time.Duration `toml:"gc-interval"`
}

func (o *Options) FillDefaults() {
	if o.MaxJobFetchTimeout == 0 {
		o.MaxJobFetchTimeout = 3 * time.Minute
	}
	if o.RoomLivenessTimeout == 0 {
		o.RoomLivenessTimeout = 2 * time.Minute
	}
	if o.GCInterval == 0 {
		o.GCInterval = max(500*time.Millisecond, o.RoomLivenessTimeout/5)
	}
}
