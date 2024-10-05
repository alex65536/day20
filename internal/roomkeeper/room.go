package roomkeeper

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/go-chess/util/maybe"
)

type room struct {
	info    RoomInfo
	mu      sync.RWMutex
	job     *roomapi.Job
	state   *delta.RoomState
	subs    map[string]chan struct{}
	stopped bool
}

func newRoom(data RoomFullData) *room {
	r := &room{
		info:    data.Info,
		job:     data.Job,
		state:   delta.NewRoomState(),
		subs:    make(map[string]chan struct{}),
		stopped: false,
	}
	r.onJobReset()
	return r
}

func (r *room) onJobReset() {
	job := r.job
	if job == nil {
		r.state.JobID = ""
		r.state.State = nil
	} else {
		r.state.JobID = job.ID
		r.state.State = delta.NewJobState()
	}
}

func (r *room) Subscribe() (<-chan struct{}, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		ch := make(chan struct{}, 1)
		close(ch)
		return ch, func() {}
	}
	id := idgen.ID()
	if _, ok := r.subs[id]; ok {
		panic("id collision")
	}
	ch := make(chan struct{}, 1)
	r.subs[id] = ch
	return ch, func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if !r.stopped {
			delete(r.subs, id)
		}
	}
}

func (r *room) onUpdate() {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, sub := range r.subs {
		select {
		case sub <- struct{}{}:
		default:
		}
	}
}

func (r *room) GameExt() (*battle.GameExt, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.state.State == nil {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no such job",
		}
	}
	if r.state.State.Info == nil {
		return nil, ErrGameNotReady
	}
	g, err := r.state.State.GameExt()
	if err != nil {
		return nil, fmt.Errorf("build game: %w", err)
	}
	return g, nil
}

func (r *room) Info() RoomInfo { return r.info }
func (r *room) ID() string     { return r.info.ID }

func (r *room) JobID() maybe.Maybe[string] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.job == nil {
		return maybe.None[string]()
	}
	return maybe.Some(r.job.ID)
}

func (r *room) SetJob(job *roomapi.Job) {
	defer r.onUpdate()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.job = job
	r.onJobReset()
}

func (r *room) StateDelta(old delta.RoomCursor) (*delta.RoomState, delta.RoomCursor, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, err := r.state.Delta(old)
	if err != nil {
		return nil, delta.RoomCursor{}, fmt.Errorf("compute delta: %w", err)
	}
	return d, r.state.Cursor(), nil
}

func (r *room) Update(log *slog.Logger, req *roomapi.UpdateRequest) (JobStatus, *delta.JobState, error) {
	defer r.onUpdate()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.job == nil {
		return NewStatusUnknown(), nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no job running",
		}
	}
	if r.job.ID != req.JobID {
		return NewStatusUnknown(), nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "job id mismatch",
		}
	}

	status := NewStatusRunning()
	defer func() {
		if status.Kind.IsFinished() {
			r.job = nil
			r.onJobReset()
		}
	}()

	switch req.Status {
	case roomapi.UpdateContinue:
	case roomapi.UpdateDone:
		status = NewStatusSucceeded()
	case roomapi.UpdateAbort:
		log.Info("received abort update", slog.String("err", req.Error))
		status = NewStatusAborted(fmt.Sprintf("error: %v", req.Error))
	case roomapi.UpdateFail:
		log.Info("received fail update", slog.String("err", req.Error))
		status = NewStatusFailed(fmt.Sprintf("error: %v", req.Error))
	default:
		log.Warn("received bad update",
			slog.String("err", req.Error),
			slog.String("status", string(req.Status)),
		)
		status = NewStatusAborted("job finished with unrecognized status")
	}

	if req.Delta != nil {
		if r.state.State.Cursor() != req.From {
			if req.From == (delta.JobCursor{}) {
				r.state.State = delta.NewJobState()
			} else {
				status = NewStatusRunning()
				return status, nil, &roomapi.Error{
					Code:    roomapi.ErrNeedsResync,
					Message: "state cursor mismatch",
				}
			}
		}
		if err := r.state.State.ApplyDelta(req.Delta); err != nil {
			status = NewStatusAborted("malformed state delta")
			return status, r.state.State.Clone(), fmt.Errorf("apply delta: %w", err)
		}
	}

	if !status.Kind.IsFinished() {
		return status, nil, nil
	}
	return status, r.state.State.Clone(), nil
}

func (r *room) Stop(log *slog.Logger) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.job != nil {
		panic("stopping room with unfinished job")
	}
	if r.stopped {
		return
	}
	r.stopped = true
	for _, sub := range r.subs {
		close(sub)
	}
	r.subs = nil
	r.job = nil
	r.onJobReset()
}
