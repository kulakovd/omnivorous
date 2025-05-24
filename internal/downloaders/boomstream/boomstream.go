package boomstream

import (
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/schollz/progressbar/v3"
	"io"
	"net/http"
	"net/url"
	"omnivorous/internal/ffmpeg"
	"omnivorous/internal/fscache"
	"omnivorous/internal/m3u8"
	"omnivorous/internal/urlutils"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const xorKey = "bla_bla_bla"
const configVersion = "1.2.97"
const maxSimultaneousDownloads = 10

var headers = map[string]string{
	"Accept":             "*/*",
	"Accept-Encoding":    "gzip",
	"Accept-Language":    "ru-RU,ru;q=0.8",
	"Connection":         "keep-alive",
	"Content-Type":       "application/json",
	"Origin":             "https://otus.ru",
	"Referer":            "https://otus.ru/",
	"Sec-Fetch-Dest":     "empty",
	"Sec-Fetch-Mode":     "cors",
	"Sec-Fetch-Site":     "cross-site",
	"Sec-GPC":            "1",
	"User-Agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	"sec-ch-ua":          "\"Brave\";v=\"131\", \"Chromium\";v=\"131\", \"Not_A Brand\";v=\"24\"",
	"sec-ch-ua-mobile":   "?0",
	"sec-ch-ua-platform": "\"macOS\"",
}

type config struct {
	MediaData struct {
		Links struct {
			HLS string `json:"hls,omitempty"`
		} `json:"links"`
		Token string `json:"token"`
	} `json:"mediaData"`
	Meta struct {
		Title string `json:"title"`
	} `json:"meta"`
}

type downloader struct {
	config    config
	chunklist *m3u8.Playlist
	key       []byte
	iv        []byte
	dir       string
}

func Download(ctx context.Context, url *url.URL) error {
	boomstreamId := url.Path

	d := downloader{}

	bar := progressbar.NewOptions64(
		-1,
		progressbar.OptionSetDescription("Getting video config"),
		progressbar.OptionSetWidth(10),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\r")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
	)

	err := d.getConfig(ctx, url)
	if err != nil {
		return fmt.Errorf("error getting config: %w", err)
	}

	bar.Describe("Retrieving video data")

	decodedToken, err := decodeString(d.config.MediaData.Token)
	if err != nil {
		return fmt.Errorf("error decoding token: %w", err)
	}

	decodedPlaylistUrl, err := decodeString(d.config.MediaData.Links.HLS)
	if err != nil {
		return fmt.Errorf("error decoding playlist URL: %w", err)
	}

	master, err := d.getPlaylist(ctx, decodedPlaylistUrl)
	if err != nil {
		return fmt.Errorf("error getting master playlist: %w", err)
	}

	selectedStream := master.GetBestResolutionStream()

	d.chunklist, err = d.getPlaylist(ctx, selectedStream.Url)
	if err != nil {
		return fmt.Errorf("error getting chunklist: %w", err)
	}

	err = d.getDecryptionKey(ctx, decodedToken)
	if err != nil {
		return fmt.Errorf("error getting decryption key: %w", err)
	}

	d.dir, err = fscache.GetCacheDir("boomstream", boomstreamId)
	if err != nil {
		return fmt.Errorf("error getting cache dir: %w", err)
	}

	bar.Finish()
	bar = progressbar.Default(int64(len(d.chunklist.Segments)), "Downloading video")

	filesList, err := d.downloadSegments(ctx, bar)
	if err != nil {
		return fmt.Errorf("error downloading video: %w", err)
	}

	ffmpegInputFile, err := generateFfmpegInputFile(filesList, d.dir)

	wd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("error getting working directory: %w", err)
	}

	bar.Finish()
	bar = progressbar.NewOptions64(
		-1,
		progressbar.OptionSetDescription("Saving video"),
		progressbar.OptionSetWidth(10),
		progressbar.OptionThrottle(65*time.Millisecond),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprint(os.Stderr, "\n")
		}),
		progressbar.OptionSpinnerType(14),
		progressbar.OptionFullWidth(),
		progressbar.OptionSetRenderBlankState(true),
	)

	err = ffmpeg.JoinFiles(ffmpegInputFile, filepath.Join(wd, d.config.Meta.Title))
	if err != nil {
		return fmt.Errorf("error joining files: %w", err)
	}

	// delete cache
	err = os.RemoveAll(d.dir)

	bar.Finish()
	return nil
}

func (d *downloader) downloadSegments(ctx context.Context, bar *progressbar.ProgressBar) ([]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	fileList := make([]string, len(d.chunklist.Segments))
	fileListMutex := sync.Mutex{}

	sem := make(chan struct{}, maxSimultaneousDownloads)
	var wg sync.WaitGroup

	errChan := make(chan error, 1)

	for i, segment := range d.chunklist.Segments {
		wg.Add(1)
		sem <- struct{}{}

		go func(i int, segment m3u8.Segment) {
			defer wg.Done()
			defer func() { <-sem }()

			filename, err := downloadSegment(ctx, segment, d.dir, d.iv, d.key)
			if err != nil {
				select {
				case errChan <- err:
					cancel()
				default:
				}
			}

			bar.Add(1)

			fileListMutex.Lock()
			fileList[i] = filename
			fileListMutex.Unlock()
		}(i, segment)
	}

	go func() {
		wg.Wait()
		close(errChan)
	}()

	if err := <-errChan; err != nil {
		return nil, err
	}

	return fileList, nil
}

func downloadSegment(ctx context.Context, segment m3u8.Segment, dir string, iv, key []byte) (string, error) {
	select {
	case <-ctx.Done():
		return "", nil
	default:
	}

	segmentUrl, err := url.Parse(segment.Url)
	if err != nil {
		return "", fmt.Errorf("error parsing segment URL: %w", err)
	}

	filename := filepath.Join(dir, path.Base(segmentUrl.Path))
	if _, err := os.Stat(filename); err == nil {
		return filename, nil // Already downloaded
	}

	resp, err := getReq(ctx, segment.Url)
	if err != nil {
		return "", fmt.Errorf("error downloading segment: %w", err)
	}
	defer resp.Body.Close()

	encSegment, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading segment: %w", err)
	}

	decrSegment, err := aes128cbcDecrypt(encSegment, key, iv)
	if err != nil {
		return "", fmt.Errorf("error decrypting segment: %w", err)
	}

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	_, err = file.Write(decrSegment)
	if err != nil {
		return "", fmt.Errorf("error writing to file: %w", err)
	}

	return filename, nil
}

func generateFfmpegInputFile(fileList []string, dir string) (string, error) {
	lines := make([]string, len(fileList))
	for i, file := range fileList {
		lines[i] = fmt.Sprintf("file '%s'", file)
	}

	content := strings.Join(lines, "\n")
	filename := filepath.Join(dir, "input.txt")

	file, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("error opening file: %w", err)
	}

	_, err = file.WriteString(content)
	if err != nil {
		return "", fmt.Errorf("error writing to file: %w", err)
	}

	return filename, nil
}

func (d *downloader) getConfig(ctx context.Context, url *url.URL) error {
	configUrl := urlutils.Clone(url)
	configUrl.Path = configUrl.Path + "/config"

	query := configUrl.Query()
	query.Add("version", configVersion)

	configUrl.RawQuery = query.Encode()

	resp, err := getReq(ctx, configUrl.String())
	if err != nil {
		return fmt.Errorf("error getting config: %w", err)
	}
	defer resp.Body.Close()

	var data config
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return fmt.Errorf("error decoding JSON: %w", err)
	}

	d.config = data

	return nil
}

func (d *downloader) getPlaylist(ctx context.Context, url string) (*m3u8.Playlist, error) {
	// get the master playlist
	resp, err := getReq(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("error getting master playlist: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading response body: %w", err)
	}

	playlist, err := m3u8.Parse(string(data))
	if err != nil {
		return nil, fmt.Errorf("error parsing playlist: %w", err)
	}

	return playlist, nil
}

func (d *downloader) getDecryptionKey(ctx context.Context, token string) error {
	xMediaReady, ok := d.chunklist.Rest["#EXT-X-MEDIA-READY"]
	if !ok {
		return fmt.Errorf("cannot find #EXT-X-MEDIA-READY in chunklist")
	}

	decrMediaReady, err := xorDecrypt(xMediaReady, xorKey)
	if err != nil {
		return fmt.Errorf("error decrypting #EXT-X-MEDIA-READY: %w", err)
	}

	keyUrl := "https://play.boomstream.com/api/process/" + xorEncrypt(decrMediaReady[0:20]+token, xorKey)
	keyResp, err := getReq(ctx, keyUrl)
	if err != nil {
		return fmt.Errorf("error getting key: %w", err)
	}
	defer keyResp.Body.Close()

	key, err := io.ReadAll(keyResp.Body)
	if err != nil {
		return fmt.Errorf("error reading key response: %w", err)
	}

	iv := []byte(decrMediaReady[20:36])

	d.key = key
	d.iv = iv

	return nil
}

func xorDecrypt(text, key string) (string, error) {
	var result []byte

	// Extend the key to match the length of the sourceText
	for len(key) < len(text)/2 {
		key += key
	}

	for i := 0; i < len(text); i += 2 {
		// Convert hex to integer
		hexVal, err := hex.DecodeString(text[i : i+2])
		if err != nil {
			return "", fmt.Errorf("error decoding hex: %w", err)
		}
		// XOR with key and append the result
		c := hexVal[0] ^ key[i/2]
		result = append(result, c)
	}

	return string(result), nil
}

func xorEncrypt(sourceText, key string) string {
	var result string

	// Extend the key to match the length of the sourceText
	for len(key) < len(sourceText) {
		key += key
	}

	for i := 0; i < len(sourceText); i++ {
		// XOR the character with the key and format as hex
		result += fmt.Sprintf("%02x", sourceText[i]^key[i])
	}

	return result
}

func aes128cbcDecrypt(data, key, iv []byte) ([]byte, error) {
	aesCipher, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("error creating cipher: %w", err)
	}

	decrypter := cipher.NewCBCDecrypter(aesCipher, iv)
	decrypted := make([]byte, len(data))
	decrypter.CryptBlocks(decrypted, data)

	padding := decrypted[len(decrypted)-1]
	if int(padding) > len(decrypted) || padding == 0 {
		return nil, fmt.Errorf("invalid padding")
	}
	decrypted = decrypted[:len(decrypted)-int(padding)]

	return decrypted, nil
}

func decodeString(input string) (string, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(input)
	if err != nil {
		return "", fmt.Errorf("error decoding Base64: %w", err)
	}
	return string(decodedBytes), nil
}

func getReq(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		panic(err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Host", req.Host)
	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}

	if resp.Header.Get("Content-Encoding") == "gzip" {
		encodedBody := resp.Body
		gzipReader, err := gzip.NewReader(encodedBody)
		if err != nil {
			return nil, fmt.Errorf("error creating gzip reader: %w", err)
		}
		resp.Body = gzipReader
	}

	return resp, nil
}
