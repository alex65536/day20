package scheduler

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/alex65536/day20/internal/roomapi"
	"github.com/alex65536/day20/internal/roomkeeper"
	"github.com/alex65536/day20/internal/stat"
	"github.com/alex65536/day20/internal/util/clone"
	"github.com/alex65536/day20/internal/util/randutil"
	"github.com/alex65536/go-chess/chess"
	"github.com/alex65536/go-chess/clock"
)

const ContestNameMaxLen = 128

type ContestKind int

const (
	ContestUnknownKind ContestKind = iota
	ContestMatch
)

func (k ContestKind) PrettyString() string {
	switch k {
	case ContestMatch:
		return "Match"
	default:
		return "?"
	}
}

type ContestStatusKind int

const (
	ContestUnknownStatus ContestStatusKind = iota
	ContestRunning
	ContestSucceeded
	ContestAborted
	ContestFailed
)

func (k ContestStatusKind) String() string {
	switch k {
	case ContestRunning:
		return "running"
	case ContestSucceeded:
		return "success"
	case ContestAborted:
		return "abort"
	case ContestFailed:
		return "fail"
	default:
		return "?"
	}
}

func (k ContestStatusKind) PrettyString() string {
	switch k {
	case ContestRunning:
		return "Running"
	case ContestSucceeded:
		return "Success"
	case ContestAborted:
		return "Aborted"
	case ContestFailed:
		return "Failed"
	default:
		return "?"
	}
}

func (k ContestStatusKind) IsFinished() bool {
	return k == ContestSucceeded || k == ContestAborted || k == ContestFailed
}

type ContestStatus struct {
	Kind   ContestStatusKind `gorm:"index"`
	Reason string
}

func NewStatusRunning() ContestStatus   { return ContestStatus{Kind: ContestRunning} }
func NewStatusSucceeded() ContestStatus { return ContestStatus{Kind: ContestSucceeded} }

func NewStatusAborted(reason string) ContestStatus {
	return ContestStatus{
		Kind:   ContestAborted,
		Reason: reason,
	}
}

func NewStatusFailed(reason string) ContestStatus {
	return ContestStatus{
		Kind:   ContestFailed,
		Reason: reason,
	}
}

type ContestSettings struct {
	Name           string
	FixedTime      *time.Duration
	TimeControl    *clock.Control `gorm:"serializer:chess"`
	OpeningBook    OpeningBook    `gorm:"embedded;embeddedPrefix:opening_"`
	ScoreThreshold int32
	TimeMargin     *time.Duration
	Kind           ContestKind
	Players        []roomapi.JobEngine `gorm:"serializer:json"`
	Match          *MatchSettings      `gorm:"-"`
}

func (s *ContestSettings) Validate() error {
	if s.Name == "" {
		return fmt.Errorf("no contest name")
	}
	if utf8.RuneCountInString(s.Name) > ContestNameMaxLen {
		return fmt.Errorf("contest name exceeds %v runes", ContestNameMaxLen)
	}
	if s.FixedTime != nil {
		if *s.FixedTime <= 0 {
			return fmt.Errorf("non-positive fixed time")
		}
	}
	if s.TimeControl != nil {
		if err := s.TimeControl.Validate(); err != nil {
			return fmt.Errorf("time control: %w", err)
		}
	}
	_, err := s.OpeningBook.Book(randutil.DefaultSource())
	if err != nil {
		return fmt.Errorf("opening book: %w", err)
	}
	if s.TimeMargin != nil {
		if *s.TimeMargin < 0 {
			return fmt.Errorf("non-positive time margin")
		}
	}
	switch s.Kind {
	case ContestMatch:
		if len(s.Players) != 2 {
			return fmt.Errorf("bad player count")
		}
		if s.Match == nil {
			return fmt.Errorf("no match data")
		}
		if s.Match.Games <= 0 {
			return fmt.Errorf("bad number of games")
		}
	default:
		return fmt.Errorf("bad contest type")
	}
	return nil
}

func (s ContestSettings) Clone() ContestSettings {
	s.FixedTime = clone.TrivialPtr(s.FixedTime)
	s.TimeControl = clone.Ptr(s.TimeControl)
	s.TimeMargin = clone.TrivialPtr(s.TimeMargin)
	s.Players = clone.DeepSlice(s.Players)
	s.Match = clone.Ptr(s.Match)
	return s
}

type MatchSettings struct {
	Games int64
}

func (s MatchSettings) Clone() MatchSettings {
	return s
}

type ContestInfo struct {
	ID string `gorm:"primaryKey"`
	ContestSettings
	PosInQueue uint64
}

func (i *ContestInfo) NewData() ContestData {
	switch i.Kind {
	case ContestMatch:
		return ContestData{
			Status:     NewStatusRunning(),
			LastIndex:  0,
			FailedJobs: 0,
			Match: &MatchData{
				FirstWin:  0,
				Draw:      0,
				SecondWin: 0,
				Inverted:  0,
			},
		}
	default:
		panic("must not happen")
	}
}

func (i ContestInfo) Clone() ContestInfo {
	i.ContestSettings = i.ContestSettings.Clone()
	return i
}

type ContestData struct {
	Status     ContestStatus `gorm:"embedded;embeddedPrefix:status_"`
	LastIndex  int64
	FailedJobs int64
	Match      *MatchData `gorm:"-"`
}

func (d ContestData) Clone() ContestData {
	d.Match = clone.Ptr(d.Match)
	return d
}

type MatchData struct {
	FirstWin  int64 `gorm:"column:w1"`
	Draw      int64 `gorm:"column:draw"`
	SecondWin int64 `gorm:"column:w2"`
	Inverted  int64
}

func (d MatchData) Status() stat.Status {
	return stat.Status{
		Win:  int(d.FirstWin),
		Draw: int(d.Draw),
		Lose: int(d.SecondWin),
	}
}

func (d MatchData) Clone() MatchData {
	return d
}

func (d MatchData) Played() int64 {
	return d.FirstWin + d.Draw + d.SecondWin
}

type ContestFullData struct {
	Info ContestInfo
	Data ContestData
}

type JobInfo struct {
	Job       roomapi.Job `gorm:"embedded"`
	ContestID string      `gorm:"index"`
	WhiteID   int
	BlackID   int
}

func (i JobInfo) Clone() JobInfo {
	i.Job = i.Job.Clone()
	return i
}

type RunningJob struct {
	JobInfo
}

func (j RunningJob) Clone() RunningJob {
	j.JobInfo = j.JobInfo.Clone()
	return j
}

type FinishedJob struct {
	JobInfo
	Status     roomkeeper.JobStatus `gorm:"embedded;embeddedPrefix:status_"`
	GameResult chess.Status         `gorm:"serializer:chess"`
	Index      int64                `gorm:"index"`
	PGN        *string
}

func (j FinishedJob) Clone() FinishedJob {
	j.JobInfo = j.JobInfo.Clone()
	j.PGN = clone.TrivialPtr(j.PGN)
	return j
}
