package logger

import (
	"time"
)

type BaseTime struct {
	NowTime time.Time
}

func (b BaseTime) Now() time.Time {
	// init
	now := time.Now()
	nowUnix := now.Unix()
	baseTime := nowUnix - int64(now.Hour()) - int64(now.Second())
	b.NowTime = time.Unix(baseTime, 0)

	return b.NowTime
}
