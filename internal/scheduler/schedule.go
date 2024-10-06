package scheduler

import (
	"fmt"
	"maps"

	"github.com/alex65536/day20/internal/util/randutil"
)

type ScheduleKey struct {
	WhiteID int
	BlackID int
}

type Schedule struct {
	mp map[ScheduleKey]int64
	rs randutil.Set[ScheduleKey]
}

func NewSchedule() Schedule {
	return Schedule{mp: make(map[ScheduleKey]int64)}
}

func (s Schedule) Clone() Schedule {
	s.mp = maps.Clone(s.mp)
	s.rs = s.rs.Clone()
	return s
}

func (s Schedule) Empty() bool {
	return len(s.mp) == 0
}

func (s *Schedule) Inc(k ScheduleKey)      { _ = s.Add(k, 1) }
func (s *Schedule) Dec(k ScheduleKey) bool { return s.Add(k, -1) }

func (s *Schedule) Add(k ScheduleKey, delta int64) bool {
	switch {
	case delta > 0:
		s.mp[k] += delta
		_ = s.rs.Add(k)
		return true
	case delta == 0:
		return true
	default:
		v := s.mp[k] + delta
		switch {
		case v < 0:
			delete(s.mp, k)
			_ = s.rs.Del(k)
			return false
		case v == 0:
			delete(s.mp, k)
			_ = s.rs.Del(k)
			return true
		default:
			s.mp[k] = v
			_ = s.rs.Add(k)
			return true
		}
	}
}

func (s *Schedule) Peek() (ScheduleKey, bool) {
	if len(s.mp) == 0 {
		return ScheduleKey{}, false
	}
	k := s.rs.Get()
	if _, ok := s.mp[k]; !ok {
		panic("must not happen")
	}
	return k, true
}

func (j JobInfo) ScheduleKey() ScheduleKey {
	return ScheduleKey{
		WhiteID: j.WhiteID,
		BlackID: j.BlackID,
	}
}

func (i *ContestInfo) BuildSchedule(d *ContestData) (Schedule, error) {
	s := NewSchedule()
	switch i.Kind {
	case ContestMatch:
		total := i.Match.Games
		if total < 0 {
			return Schedule{}, fmt.Errorf("total number of games is negative")
		}
		_ = s.Add(ScheduleKey{WhiteID: 0, BlackID: 1}, (total+1)/2)
		_ = s.Add(ScheduleKey{WhiteID: 1, BlackID: 0}, total/2)
		played := d.Match.Played()
		playedInv := d.Match.Inverted
		playedNonInv := played - playedInv
		if playedInv < 0 || playedNonInv < 0 {
			return Schedule{}, fmt.Errorf("negative number of games played")
		}
		if !s.Add(ScheduleKey{WhiteID: 0, BlackID: 1}, -playedNonInv) {
			return Schedule{}, fmt.Errorf("too many games played")
		}
		if !s.Add(ScheduleKey{WhiteID: 1, BlackID: 0}, -playedInv) {
			return Schedule{}, fmt.Errorf("too many games played")
		}
	default:
		panic("bad contest kind")
	}
	return s, nil
}
