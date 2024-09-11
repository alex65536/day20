package roomkeeper

import (
	"context"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomapi"
)

type Job struct {
	JobID     string
	ContestID string
	Desc      roomapi.Job
}

func (j Job) Clone() Job {
	j.Desc = j.Desc.Clone()
	return j
}

type JobStatus int

const (
	JobRunning JobStatus = iota
	JobSucceeded
	JobAborted
)

func (s JobStatus) String() string {
	switch s {
	case JobRunning:
		return "running"
	case JobSucceeded:
		return "success"
	case JobAborted:
		return "abort"
	default:
		return "unknown"
	}
}

type RoomDesc struct {
	RoomID string
	Job    *Job
}

func (r RoomDesc) Clone() RoomDesc {
	if r.Job != nil {
		j := *r.Job
		r.Job = &j
	}
	return r
}

type DB interface {
	UpdateRoom(ctx context.Context, room RoomDesc) error
	DeleteRoom(ctx context.Context, roomID string) error
	AddGame(ctx context.Context, contestID string, game *battle.GameExt) error
}

type Scheduler interface {
	IsContestRunning(contestID string) bool
	NextJob(ctx context.Context) (*Job, error)
	OnJobFinished(jobID string, status JobStatus)
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
