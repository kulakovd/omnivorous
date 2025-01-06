package m3u8

type Resolution struct {
	Width  int
	Height int
}

type Stream struct {
	Url        string
	Bandwidth  int
	Resolution Resolution
}
