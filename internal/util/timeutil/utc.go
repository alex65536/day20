package timeutil

import (
	"database/sql/driver"
	"fmt"
	"time"
)

type UTCTime time.Time

func (t UTCTime) Value() (driver.Value, error) {
	return time.Time(t), nil
}

func (t UTCTime) UTC() time.Time {
	return time.Time(t).UTC()
}

func (t UTCTime) Local() time.Time {
	return time.Time(t).Local()
}

func (t UTCTime) Compare(u UTCTime) int {
	return time.Time(t).Compare(time.Time(u))
}

func (t *UTCTime) Scan(value any) error {
	if value == nil {
		return nil
	}
	cvt, err := driver.DefaultParameterConverter.ConvertValue(value)
	if err != nil {
		return fmt.Errorf("convert: %w", err)
	}
	cvtTime, ok := cvt.(time.Time)
	if !ok {
		return fmt.Errorf("expected type time.Time, got type %T", cvt)
	}
	*t = UTCTime(cvtTime)
	return nil
}

func NowUTC() UTCTime {
	return UTCTime(time.Now().UTC())
}

func (t UTCTime) Add(delta time.Duration) UTCTime {
	return UTCTime(time.Time(t).Add(delta))
}
