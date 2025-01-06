package m3u8

import "time"

type Segment struct {
	Url      string
	Duration time.Duration
}
