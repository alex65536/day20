package scheduler

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/clone"
	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/sliceutil"
	"github.com/alex65536/day20/internal/util/slogx"
)

type Options struct {
	MaxRunningContests int           `toml:"max-running-contests"`
	DBSaveTimeout      time.Duration `toml:"db-save-timeout"`
	MaxFailedJobs      int64         `toml:"max-failed-jobs"`
}

func (o Options) Clone() Options {
	return o
}

func (o *Options) FillDefaults() {
	if o.MaxRunningContests == 0 {
		o.MaxRunningContests = 100
	}
	if o.DBSaveTimeout == 0 {
		o.DBSaveTimeout = 10 * time.Second
	}
	if o.MaxFailedJobs == 0 {
		o.MaxFailedJobs = 10
	}
}

type contestExt struct {
	s     *Scheduler
	sched *contestScheduler
	dbMu  chan struct{}
}

func newContestExt(s *Scheduler, sched *contestScheduler) *contestExt {
	e := &contestExt{
		s:     s,
		sched: sched,
		dbMu:  make(chan struct{}, 1),
	}
	e.dbMu <- struct{}{}
	return e
}

func (c *contestExt) Save() {
	ctx, cancel := context.WithTimeout(context.Background(), c.s.o.DBSaveTimeout)
	defer cancel()
	select {
	case <-c.dbMu:
	case <-ctx.Done():
		c.s.log.Error("could not save contest state", slogx.Err(ctx.Err()))
		return
	}
	defer func() { c.dbMu <- struct{}{} }()
	err := c.s.db.UpdateContest(ctx, c.sched.Info().ID, c.sched.Data())
	if err != nil {
		c.s.log.Error("could not save contest state", slogx.Err(err))
		return
	}
}

type contestHeapItem struct {
	ContestID  string
	PosInQueue uint64
}

type contestHeap []contestHeapItem

func (h contestHeap) Len() int           { return len(h) }
func (h contestHeap) Less(i, j int) bool { return h[i].PosInQueue < h[j].PosInQueue }
func (h contestHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *contestHeap) Push(x any) {
	*h = append(*h, x.(contestHeapItem))
}

func (h *contestHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

type Scheduler struct {
	o   *Options
	db  DB
	log *slog.Logger

	mu           sync.RWMutex
	jobs         map[string]*RunningJob
	contests     map[string]*contestExt
	heap         contestHeap
	lastQueuePos uint64
	notify       chan struct{}
}

func (s *Scheduler) onHeapUpdatedUnlocked() {
	if len(s.heap) != 0 {
		select {
		case s.notify <- struct{}{}:
		default:
		}
	}
}

func (s *Scheduler) acquireContest(ctx context.Context) (*contestExt, error) {
	for {
		contest, ok := func() (*contestExt, bool) {
			s.mu.Lock()
			defer s.mu.Unlock()
			for {
				if len(s.heap) == 0 {
					return nil, false
				}
				contestID := s.heap[0].ContestID
				contest, ok := s.contests[contestID]
				if !ok || contest.sched.IsFinished() {
					heap.Pop(&s.heap)
					delete(s.contests, contestID)
					continue
				}
				s.onHeapUpdatedUnlocked()
				return contest, true
			}
		}()
		if ok {
			return contest, nil
		}
		select {
		case <-s.notify:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *Scheduler) delContestIfFinished(contest *contestExt) {
	if contest.sched.IsFinished() {
		s.mu.Lock()
		delete(s.contests, contest.sched.Info().ID)
		s.mu.Unlock()
	}
}

func (s *Scheduler) IsJobAborted(jobID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return "job lost by scheduler", true
	}
	contest, ok := s.contests[job.ContestID]
	if !ok {
		return "contest finished", true
	}
	return contest.sched.IsJobAborted(jobID)
}

func (s *Scheduler) NextJob(ctx context.Context) (*roomapi.Job, error) {
	for {
		contest, err := s.acquireContest(ctx)
		if err != nil {
			return nil, err
		}
		job, err := contest.sched.NextJob(ctx)
		if err != nil {
			if errors.Is(err, errContestFinished) {
				continue
			}
			return nil, fmt.Errorf("get job in contest: %w", err)
		}
		if err := s.db.CreateRunningJob(context.Background(), job); err != nil {
			s.log.Error("could not create job in db", slogx.Err(err))
		}
		s.mu.Lock()
		s.jobs[job.Job.ID] = job
		s.mu.Unlock()
		contest.Save()
		return clone.Ptr(&job.Job), nil
	}
}

func (s *Scheduler) OnJobFinished(jobID string, status roomkeeper.JobStatus, game *battle.GameExt) {
	if !status.Kind.IsFinished() {
		panic("must not happen")
	}

	job, contest, jobOk, contestOk := func() (*RunningJob, *contestExt, bool, bool) {
		s.mu.Lock()
		defer s.mu.Unlock()
		job, ok := s.jobs[jobID]
		if !ok {
			return nil, nil, false, false
		}
		delete(s.jobs, jobID)
		contest, ok := s.contests[job.ContestID]
		if !ok {
			return job, nil, true, false
		}
		return job, contest, true, true
	}()
	if !jobOk {
		s.log.Error("got job unknown to scheduler", slog.String("job_id", jobID))
		return
	}

	finishedJob, err := func() (*FinishedJob, error) {
		if !contestOk {
			s.log.Info("got job after contest finished", slog.String("job_id", jobID), slog.String("status", status.String()))
			return nil, fmt.Errorf("got job after contest finished")
		}
		job, err := contest.sched.FinalizeJob(jobID, status, game)
		contest.Save()
		s.delContestIfFinished(contest)
		return job, err
	}()
	if err != nil {
		finishedJob = &FinishedJob{
			JobInfo: job.JobInfo.Clone(),
			Status:  status,
			PGN:     nil,
		}
		if finishedJob.Status.Kind != roomkeeper.JobAborted {
			finishedJob.Status = roomkeeper.NewStatusAborted(err.Error())
		}
		addPGNToJobOrAbort(s.log, finishedJob, game)
	}

	s.db.FinishRunningJob(context.Background(), finishedJob)
}

func (s *Scheduler) CreateContest(ctx context.Context, settings ContestSettings) (ContestInfo, error) {
	if err := settings.Validate(); err != nil {
		return ContestInfo{}, fmt.Errorf("invalid contest settings: %w", err)
	}

	contest, err := func() (*contestExt, error) {
		s.mu.Lock()
		s.lastQueuePos++
		queuePos := s.lastQueuePos
		s.mu.Unlock()
		info := ContestInfo{
			ContestSettings: settings.Clone(),
			ID:              idgen.ID(),
			PosInQueue:      queuePos,
		}
		data := info.NewData()
		sched, err := newContestScheduler(s.log, s.o, &info, data, nil)
		if err != nil {
			return nil, fmt.Errorf("create contest scheduler: %w", err)
		}
		if err := s.db.CreateContest(ctx, info, data); err != nil {
			s.log.Warn("could not create contest in db", slogx.Err(err))
			sched.Abort("contest not created in db")
			return nil, fmt.Errorf("create contest in db: %w", err)
		}
		contest := newContestExt(s, sched)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.contests[info.ID] = contest
		heap.Push(&s.heap, contestHeapItem{
			ContestID:  info.ID,
			PosInQueue: info.PosInQueue,
		})
		s.onHeapUpdatedUnlocked()
		return contest, nil
	}()
	if err != nil {
		return ContestInfo{}, err
	}

	return contest.sched.Info().Clone(), nil
}

func (s *Scheduler) AbortContest(contestID string, reason string) {
	s.mu.RLock()
	contest, ok := s.contests[contestID]
	s.mu.RUnlock()
	if !ok {
		return
	}
	contest.sched.Abort(reason)
	contest.Save()
	s.delContestIfFinished(contest)
}

func (s *Scheduler) GetContest(ctx context.Context, contestID string) (ContestInfo, ContestData, error) {
	s.mu.RLock()
	contest, ok := s.contests[contestID]
	s.mu.RUnlock()
	if !ok {
		return s.db.GetContest(ctx, contestID)
	}
	return contest.sched.info.Clone(), contest.sched.Data(), nil
}

func (s *Scheduler) ListAllContests(ctx context.Context) ([]ContestFullData, error) {
	return s.db.ListContests(ctx)
}

func (s *Scheduler) ListContestSucceededJobs(ctx context.Context, contestID string) ([]FinishedJob, error) {
	jobs, err := s.db.ListContestSucceededJobs(ctx, contestID)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	if len(jobs) == 0 {
		_, _, err := s.GetContest(ctx, contestID)
		if err != nil {
			return nil, fmt.Errorf("get contest: %w", err)
		}
	}
	return jobs, nil
}

func (s *Scheduler) ListRunningContests() []ContestFullData {
	contests := func() []*contestScheduler {
		s.mu.RLock()
		defer s.mu.RUnlock()
		contests := make([]*contestScheduler, 0, len(s.contests))
		for _, c := range s.contests {
			contests = append(contests, c.sched)
		}
		return contests
	}()
	res := sliceutil.FilterMap(contests, func(sched *contestScheduler) (ContestFullData, bool) {
		data := sched.Data()
		if data.Status.Kind.IsFinished() {
			return ContestFullData{}, false
		}
		return ContestFullData{
			Info: sched.Info().Clone(),
			Data: data,
		}, true
	})
	return res
}

func New(ctx context.Context, log *slog.Logger, db DB, o Options) (*Scheduler, error) {
	o = o.Clone()
	o.FillDefaults()

	rooms, err := db.ListActiveRooms(ctx)
	if err != nil {
		log.Warn("could not list active rooms", slogx.Err(err))
		return nil, fmt.Errorf("list active rooms: %w", err)
	}

	dbContests, err := db.ListRunningContestsFull(ctx)
	if err != nil {
		log.Warn("could not list running contests", slogx.Err(err))
		return nil, fmt.Errorf("list running contests: %w", err)
	}

	roomJobs := make(map[string]struct{})
	for _, r := range rooms {
		if r.Job != nil {
			roomJobs[r.Job.ID] = struct{}{}
		}
	}

	jobs := make(map[string]*RunningJob)
	contests := make(map[string]*contestScheduler)
	for _, dbContest := range dbContests {
		contestJobs := make([]*RunningJob, 0, len(dbContest.Jobs))
		for _, job := range dbContest.Jobs {
			if _, ok := roomJobs[job.Job.ID]; !ok {
				log.Warn("found running jobs not belonging to any room, aborting",
					slog.String("job_id", job.Job.ID),
				)
				finishedJob := &FinishedJob{
					JobInfo: job.JobInfo.Clone(),
					Status:  roomkeeper.NewStatusAborted("job lost by rooms"),
					PGN:     nil,
				}
				if err := db.FinishRunningJob(ctx, finishedJob); err != nil {
					log.Warn("could not finish running job", slogx.Err(err))
					return nil, fmt.Errorf("finish running job: %w", err)
				}
				continue
			}
			contestJobs = append(contestJobs, &job)
			jobs[job.Job.ID] = &job
		}

		info := clone.Ptr(&dbContest.Info)
		data := dbContest.Data.Clone()
		sched, err := newContestScheduler(log, &o, info, data, contestJobs)
		if err != nil {
			log.Warn("could not create contest scheduler, aborting",
				slog.String("contest_id", info.ID), slogx.Err(err))
			data.Status = NewStatusAborted("could not schedule contest")
			if err := db.UpdateContest(ctx, info.ID, data); err != nil {
				log.Warn("could not abort contest", slog.String("contest_id", info.ID), slogx.Err(err))
				return nil, fmt.Errorf("abort contest: %w", err)
			}
			continue
		}
		contests[info.ID] = sched
	}

	var cHeap contestHeap
	heap.Init(&cHeap)
	var lastQueuePos uint64
	for _, c := range contests {
		info := c.Info()
		heap.Push(&cHeap, contestHeapItem{
			ContestID:  info.ID,
			PosInQueue: info.PosInQueue,
		})
		lastQueuePos = max(lastQueuePos, info.PosInQueue)
	}

	s := &Scheduler{
		o:            &o,
		db:           db,
		log:          log,
		jobs:         jobs,
		contests:     make(map[string]*contestExt, len(contests)),
		heap:         cHeap,
		lastQueuePos: lastQueuePos,
		notify:       make(chan struct{}, 1),
	}
	for k, sched := range contests {
		s.contests[k] = newContestExt(s, sched)
	}
	s.onHeapUpdatedUnlocked()
	return s, nil
}
