package m3u8

type Playlist struct {
	Version  int
	IsMaster bool
	Streams  []Stream
	Segments []Segment
	Rest     map[string]string
}

func (p *Playlist) GetStreamByResolution(width, height int) *Stream {
	for _, s := range p.Streams {
		if s.Resolution.Width == width && s.Resolution.Height == height {
			return &s
		}
	}

	return nil
}

func (p *Playlist) GetBestResolutionStream() *Stream {
	var best Stream
	for _, s := range p.Streams {
		if s.Resolution.Width > best.Resolution.Width && s.Resolution.Height > best.Resolution.Height {
			best = s
		}
	}

	return &best
}
