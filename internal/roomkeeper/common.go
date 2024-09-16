package roomkeeper

import (
	"context"
	"errors"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util"
)

var ErrGameNotReady = errors.New("game not ready")

type Job struct {
	ContestID string
	Desc      roomapi.Job
}

func (j Job) Clone() Job {
	j.Desc = j.Desc.Clone()
	return j
}

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
	Kind   JobStatusKind
	Reason string
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
	ID   string
	Name string
}

type RoomData struct {
	Job *Job
}

func (d RoomData) Clone() RoomData {
	d.Job = util.ClonePtr(d.Job)
	return d
}

type RoomFullData struct {
	Info RoomInfo
	Data RoomData
}

type DB interface {
	ListActiveRooms(ctx context.Context) ([]RoomFullData, error)
	CreateRoom(ctx context.Context, info RoomInfo) error
	UpdateRoom(ctx context.Context, roomID string, data RoomData) error
	DeleteRoom(ctx context.Context, roomID string) error
}

type Scheduler interface {
	IsContestRunning(contestID string) bool
	NextJob(ctx context.Context) (*Job, error)
	OnJobFinished(jobID string, status JobStatus, game *battle.GameExt)
}

type Options struct {
	MaxJobFetchTimeout  time.Duration
	RoomLivenessTimeout time.Duration
	GCInterval          time.Duration
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
