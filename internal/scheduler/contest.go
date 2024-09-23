package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/alex65536/day20/internal/battle"
	"github.com/alex65536/day20/internal/opening"
	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/util/clone"
	"github.com/alex65536/day20/internal/util/idgen"
	"github.com/alex65536/day20/internal/util/randutil"
	"github.com/alex65536/go-chess/chess"
)

var errContestStopped = errors.New("contest stopped, no new jobs")

type contestScheduler struct {
	log  *slog.Logger
	info *ContestInfo
	book opening.Book

	mu     sync.RWMutex
	data   ContestData
	jobs   map[string]*RunningJob
	sched  Schedule
	notify chan struct{}
	closed bool
}

func newContestScheduler(
	log *slog.Logger,
	info *ContestInfo,
	data ContestData,
	jobs []*RunningJob,
) (*contestScheduler, error) {
	data = data.Clone()
	if data.Status.Kind != ContestRunning {
		panic("must not happen")
	}

	log = log.With(slog.String("contest_id", info.ID))

	sched, err := info.BuildSchedule(&data)
	if err != nil {
		return nil, fmt.Errorf("bad schedule: %w", err)
	}

	book, err := info.OpeningBook.Book(randutil.DefaultSource())
	if err != nil {
		return nil, fmt.Errorf("bad opening book: %w", err)
	}

	jobMap := make(map[string]*RunningJob, len(jobs))
	for _, j := range jobs {
		if !sched.Dec(j.ScheduleKey()) {
			log.Warn("found extraneous job", slog.String("job_id", j.Job.ID))
			continue
		}
		jobMap[j.Job.ID] = j
	}

	cs := &contestScheduler{
		log:  log,
		info: info,
		book: book,

		data:   data,
		jobs:   jobMap,
		sched:  sched,
		notify: make(chan struct{}, 1),
		closed: false,
	}
	cs.onUpdatedUnlocked()
	return cs, nil
}

func (s *contestScheduler) isStoppedUnlocked() bool {
	return s.data.Status.Kind != ContestRunning
}

func (s *contestScheduler) onUpdatedUnlocked() {
	if s.isStoppedUnlocked() {
		if !s.closed {
			close(s.notify)
			s.closed = true
		}
		return
	}
	if !s.sched.Empty() {
		select {
		case s.notify <- struct{}{}:
		default:
		}
	}
}

func (s *contestScheduler) getJob() (*RunningJob, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isStoppedUnlocked() {
		return nil, false, errContestStopped
	}
	k, ok := s.sched.Peek()
	if !ok {
		return nil, false, nil
	}
	_ = s.sched.Dec(k)
	opening := s.book.Opening()
	startMoves := make([]chess.UCIMove, opening.Len())
	for i := range opening.Len() {
		startMoves[i] = opening.MoveAt(i).UCIMove()
	}
	startBoard := opening.StartPos()
	var pStartBoard *chess.RawBoard
	if startBoard != chess.InitialRawBoard() {
		pStartBoard = &startBoard
	}
	timeControl := clone.Ptr(s.info.TimeControl)
	if timeControl != nil && s.info.Kind == ContestMatch && k.WhiteID == 1 {
		timeControl.White, timeControl.Black = timeControl.Black, timeControl.White
	}
	job := &RunningJob{
		JobInfo: JobInfo{
			Job: roomapi.Job{
				ID:             idgen.ID(),
				FixedTime:      clone.TrivialPtr(s.info.FixedTime),
				TimeControl:    timeControl,
				StartBoard:     pStartBoard,
				StartMoves:     startMoves,
				ScoreThreshold: s.info.ScoreThreshold,
				TimeMargin:     clone.TrivialPtr(s.info.TimeMargin),
				White:          s.info.Players[k.WhiteID].Clone(),
				Black:          s.info.Players[k.BlackID].Clone(),
			},
			ContestID: s.info.ID,
			WhiteID:   k.WhiteID,
			BlackID:   k.BlackID,
		},
	}
	s.jobs[job.Job.ID] = job
	s.onUpdatedUnlocked()
	return job, true, nil
}

func (s *contestScheduler) IsStopped() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isStoppedUnlocked()
}

func (s *contestScheduler) Info() *ContestInfo {
	return s.info
}

func (s *contestScheduler) Status() ContestStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Status
}

func (s *contestScheduler) Data() ContestData {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Clone()
}

func (s *contestScheduler) Abort(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.isStoppedUnlocked() {
		return
	}
	s.jobs = make(map[string]*RunningJob)
	s.data.Status = NewContestAborted(reason)
	s.onUpdatedUnlocked()
}

func (s *contestScheduler) IsJobAborted(jobID string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.isStoppedUnlocked() {
		return "contest stopped", true
	}
	_, ok := s.jobs[jobID]
	if ok {
		return "", false
	}
	return "job lost by scheduler", true
}

func (s *contestScheduler) NextJob(ctx context.Context) (*RunningJob, error) {
	for {
		job, ok, err := s.getJob()
		if err != nil {
			return nil, err
		}
		if ok {
			return job, nil
		}
		select {
		case _, ok := <-s.notify:
			if !ok {
				return nil, errContestStopped
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (s *contestScheduler) FinalizeJob(
	jobID string,
	srcStatus roomkeeper.JobStatus,
	game *battle.GameExt,
) (*FinishedJob, error) {
	if !srcStatus.Kind.IsFinished() {
		panic("must not happen")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.isStoppedUnlocked() {
		s.log.Info("got job after contest stopped", slog.String("job_id", jobID), slog.String("status", srcStatus.String()))
		return nil, fmt.Errorf("got job after contest stopped")
	}
	runningJob, ok := s.jobs[jobID]
	if !ok {
		s.log.Info("got stray job", slog.String("job_id", jobID), slog.String("status", srcStatus.String()))
		return nil, fmt.Errorf("job lost by contest scheduler")
	}
	delete(s.jobs, jobID)

	defer s.onUpdatedUnlocked()

	job := &FinishedJob{
		JobInfo:    runningJob.JobInfo.Clone(),
		Status:     srcStatus,
		Index:      0,
		GameResult: chess.StatusRunning,
		PGN:        nil,
	}

	if job.Status.Kind == roomkeeper.JobSucceeded {
		job.GameResult = game.Game.Outcome().Status()
		if job.GameResult != chess.StatusWhiteWins &&
			job.GameResult != chess.StatusBlackWins &&
			job.GameResult != chess.StatusDraw {
			s.log.Warn("bad game outcome in job", slog.String("job_id", jobID))
			job.Status = roomkeeper.NewStatusAborted("bad game result")
			job.GameResult = chess.StatusRunning
		}
	}

	if job.Status.Kind == roomkeeper.JobSucceeded {
		game = clone.TrivialPtr(game) // Yes, TrivialPtr() is intended, since we want a shallow copy.
		if game != nil {
			game.Round = int(s.data.LastIndex + 1)
		}
	}

	addPGNToJobOrAbort(s.log, job, game)

	switch job.Status.Kind {
	case roomkeeper.JobAborted:
		s.sched.Inc(job.ScheduleKey())
	case roomkeeper.JobSucceeded:
		s.data.LastIndex++
		job.Index = s.data.LastIndex
		switch s.info.Kind {
		case ContestMatch:
			inv := job.WhiteID == 1
			if inv {
				s.data.Match.Inverted++
			}
			switch job.GameResult {
			case chess.StatusWhiteWins:
				if inv {
					s.data.Match.SecondWin++
				} else {
					s.data.Match.FirstWin++
				}
			case chess.StatusBlackWins:
				if inv {
					s.data.Match.FirstWin++
				} else {
					s.data.Match.SecondWin++
				}
			case chess.StatusDraw:
				s.data.Match.Draw++
			default:
				panic("must not happen")
			}
		default:
			panic("bad contest kind")
		}
		if len(s.jobs) == 0 && s.sched.Empty() {
			s.data.Status = NewContestSucceeded()
		}
	default:
		panic("bad job kind")
	}

	return job, nil
}
