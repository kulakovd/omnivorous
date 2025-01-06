package m3u8

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

func Parse(data string) (*Playlist, error) {
	var p Playlist

	for _, line := range strings.Split(data, "\n") {
		if len(line) == 0 {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			if p.IsMaster {
				// this is a stream url, add to the last stream
				lastStream := &p.Streams[len(p.Streams)-1]
				if lastStream.Url != "" {
					slog.Error("Stream URL already set", "line", line)
					return nil, fmt.Errorf("stream URL already set")
				}
				lastStream.Url = line
			} else {
				// this is a segment url, add to the last segment
				lastSegment := &p.Segments[len(p.Segments)-1]
				if lastSegment.Url != "" {
					slog.Error("Segment URL already set", "line", line)
					return nil, fmt.Errorf("segment URL already set")
				}
				lastSegment.Url = line
			}
		} else if strings.HasPrefix(line, "#EXTM3U") {
			p = Playlist{}
		} else if strings.HasPrefix(line, "#EXT-X-VERSION") {
			_, versionStr := splitLine(line)
			version, err := strconv.Atoi(versionStr)
			if err != nil {
				slog.Error("Error parsing version", err, "line", line)
				return nil, err
			}
			p.Version = version
		} else if strings.HasPrefix(line, "#EXT-X-STREAM-INF") {
			p.IsMaster = true
			var s Stream
			_, attrs := splitLine(line)
			for _, attr := range strings.Split(attrs, ",") {
				if strings.HasPrefix(attr, "BANDWIDTH=") {
					_, bandwidthStr := splitAttr(attr)
					bandwidth, err := strconv.Atoi(bandwidthStr)
					if err != nil {
						slog.Error("Error parsing bandwidth", err, "attr", attr)
						return nil, err
					}
					s.Bandwidth = bandwidth
				}
				if strings.HasPrefix(attr, "RESOLUTION=") {
					_, res := splitAttr(attr)
					var width, height int
					_, err := fmt.Fscanf(strings.NewReader(res), "%dx%d", &width, &height)
					if err != nil {
						return nil, err
					}
					s.Resolution = Resolution{Width: width, Height: height}
				}
			}
			if p.Streams == nil {
				p.Streams = make([]Stream, 0)
			}
			p.Streams = append(p.Streams, s)
		} else if strings.HasPrefix(line, "#EXTINF") {
			var s Segment
			_, attrsStr := splitLine(line)
			// first attr is duration
			attrs := strings.Split(attrsStr, ",")
			duration, err := strconv.ParseFloat(attrs[0], 64)
			if err != nil {
				slog.Error("Error parsing duration", err, "line", line)
				return nil, err
			}
			s.Duration = time.Duration(duration*1000) * time.Millisecond
			if p.Streams == nil {
				p.Streams = make([]Stream, 0)
			}
			p.Segments = append(p.Segments, s)
		} else {
			if p.Rest == nil {
				p.Rest = make(map[string]string)
			}
			if strings.Contains(line, ":") {
				key, value := splitLine(line)
				p.Rest[key] = value
			} else {
				p.Rest[line] = ""
			}
		}
	}

	return &p, nil
}

func splitAttr(attr string) (string, string) {
	parts := strings.Split(attr, "=")
	return parts[0], parts[1]
}

func splitLine(line string) (string, string) {
	parts := strings.Split(line, ":")
	return parts[0], parts[1]
}
