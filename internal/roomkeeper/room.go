package roomkeeper

import (
	"fmt"
	"sync"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/util/id"
)

type room struct {
	mu      sync.RWMutex
	desc    RoomDesc
	state   *delta.RoomState
	subs    map[string]chan struct{}
	stopped bool
}

func newRoom(desc RoomDesc) *room {
	r := &room{
		desc:    desc,
		state:   delta.NewRoomState(),
		subs:    make(map[string]chan struct{}),
		stopped: false,
	}
	r.onJobReset()
	return r
}

func (r *room) onJobReset() {
	job := r.desc.Job
	if job == nil {
		r.state.JobID = ""
		r.state.State = nil
	} else {
		r.state.JobID = job.Desc.ID
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
	id := id.ID()
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

func (r *room) Info() RoomInfo {
	return r.desc.Info
}

func (r *room) ID() string {
	return r.Info().ID
}

func (r *room) Desc() RoomDesc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.desc
}

func (r *room) Job() *Job {
	return r.desc.Job
}

func (r *room) SetJob(job *Job) {
	defer r.onUpdate()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.desc.Job = job
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

func (r *room) Update(req *roomapi.UpdateRequest) (JobStatus, *delta.JobState, error) {
	defer r.onUpdate()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.desc.Job == nil {
		return NewStatusAborted("no job running"), nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no job running",
		}
	}

	status := NewStatusRunning()
	defer func() {
		if status.Kind != JobRunning {
			r.desc.Job = nil
			r.onJobReset()
		}
	}()

	if req.Done {
		if req.Error == "" {
			status = NewStatusSucceeded()
		} else {
			status = NewStatusAborted(fmt.Sprintf("error: %v", req.Error))
		}
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
			return status, nil, fmt.Errorf("apply delta: %w", err)
		}
	}

	if status.Kind == JobSucceeded {
		return status, r.state.State.Clone(), nil
	}
	return status, nil, nil
}

func (r *room) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		return
	}
	r.stopped = true
	for _, sub := range r.subs {
		close(sub)
	}
	r.subs = nil
	r.desc.Job = nil
	r.onJobReset()
}
