package roomkeeper

import (
	"fmt"
	"sync"

	"github.com/alex65536/day20/internal/delta"
	"github.com/alex65536/day20/internal/roomapi"
)

type room struct {
	mu      sync.RWMutex
	desc    RoomDesc
	state   *delta.State
	subs    map[string]chan struct{}
	stopped bool
}

func newRoom(desc RoomDesc) *room {
	r := &room{
		desc:    desc,
		subs:    make(map[string]chan struct{}),
		state:   nil,
		stopped: false,
	}
	if desc.Job != nil {
		r.state = delta.NewState()
	}
	return r
}

func (r *room) Subscribe() (<-chan struct{}, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped {
		ch := make(chan struct{}, 1)
		close(ch)
		return ch, func() {}
	}
	id := genUnusedKey(r.subs)
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

func (r *room) ID() string {
	return r.desc.RoomID
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
	r.state = nil
	if job != nil {
		r.state = delta.NewState()
	}
}

func (r *room) State(old delta.Cursor) (*delta.State, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.desc.Job == nil {
		return nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no job running",
		}
	}
	delta, err := r.state.Delta(old)
	if err != nil {
		return nil, fmt.Errorf("compute delta: %w", err)
	}
	return delta, nil
}

func (r *room) Update(req *roomapi.UpdateRequest) (JobStatus, *delta.State, error) {
	defer r.onUpdate()
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.desc.Job == nil {
		return JobAborted, nil, &roomapi.Error{
			Code:    roomapi.ErrNoJobRunning,
			Message: "no job running",
		}
	}

	status := JobRunning
	defer func() {
		if status != JobRunning {
			r.desc.Job = nil
			r.state = nil
		}
	}()

	if req.Done {
		if req.Error == "" {
			status = JobSucceeded
		} else {
			status = JobAborted
		}
	}

	if req.Delta != nil {
		if r.state.Cursor() != req.From {
			if req.From == (delta.Cursor{}) {
				r.state = delta.NewState()
			} else {
				status = JobRunning
				return status, nil, &roomapi.Error{
					Code:    roomapi.ErrNeedsResync,
					Message: "state cursor mismatch",
				}
			}
		}
		if err := r.state.ApplyDelta(req.Delta); err != nil {
			status = JobAborted
			return status, nil, fmt.Errorf("apply delta: %w", err)
		}
	}

	if status == JobSucceeded {
		return status, r.state, nil
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
	r.state = nil
}
