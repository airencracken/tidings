package reader

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/airencracken/comfylib/ui"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const imageDownloadLimit = 5 << 20
const imageCacheLimit = 100 << 20
const imageCacheAge = 7 * 24 * time.Hour

type imageFlight struct {
	done chan struct{}
	data []byte
	err  error
}
type imageCache struct {
	directory string
	mu        sync.Mutex
	flights   map[string]*imageFlight
	capacity  chan struct{}
	pruneMu   sync.Mutex
}

func newImageCache(directory string) *imageCache {
	return &imageCache{directory: filepath.Join(directory, "cache", "images"), flights: map[string]*imageFlight{}, capacity: make(chan struct{}, 2)}
}

func (c *imageCache) get(ctx context.Context, address string, client *http.Client) ([]byte, error) {
	address, err := WebURL(address)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(address))
	key := hex.EncodeToString(sum[:]) + ".jpg"
	path := filepath.Join(c.directory, key)
	if data, err := readCachedImage(path); err == nil {
		return data, nil
	}
	c.mu.Lock()
	if flight := c.flights[key]; flight != nil {
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-flight.done:
			return flight.data, flight.err
		}
	}
	flight := &imageFlight{done: make(chan struct{})}
	c.flights[key] = flight
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.flights, key); close(flight.done); c.mu.Unlock() }()
	select {
	case c.capacity <- struct{}{}:
	case <-ctx.Done():
		flight.err = ctx.Err()
		return nil, flight.err
	}
	defer func() { <-c.capacity }()
	// A previous request may have published the file between the first read
	// and taking the per-image flight.
	if data, err := readCachedImage(path); err == nil {
		flight.data = data
		return data, nil
	}
	flight.data, flight.err = downloadImage(ctx, address, client)
	if flight.err != nil {
		return nil, flight.err
	}
	flight.err = c.save(path, flight.data)
	return flight.data, flight.err
}

func readCachedImage(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > imageDownloadLimit || time.Since(info.ModTime()) > imageCacheAge {
		return nil, errors.New("cached image expired or is not private")
	}
	return os.ReadFile(path)
}

func downloadImage(ctx context.Context, address string, client *http.Client) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Tidings (+https://github.com/airencracken/tidings)")
	req.Header.Set("Accept", "image/jpeg,image/png,image/webp,image/gif")
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image publisher returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, imageDownloadLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > imageDownloadLimit {
		return nil, errors.New("image is larger than " + ui.Size(imageDownloadLimit))
	}
	return previewImage(data)
}

func previewImage(data []byte) ([]byte, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width < 2 || config.Height < 2 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 12_000_000 {
		return nil, errors.New("image dimensions exceed preview limits")
	}
	source, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	w, h := config.Width, config.Height
	if w > 1280 {
		h = h * 1280 / w
		w = 1280
	}
	if h > 1280 {
		w = w * 1280 / h
		h = 1280
	}
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	preview := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(preview, preview.Bounds(), image.White, image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(preview, preview.Bounds(), source, source.Bounds(), draw.Over, nil)
	var encoded bytes.Buffer
	if err = jpeg.Encode(&encoded, preview, &jpeg.Options{Quality: 85}); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}

func (c *imageCache) save(path string, data []byte) (err error) {
	if err = os.MkdirAll(c.directory, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(c.directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("image cache directory must be private")
	}
	f, err := os.CreateTemp(c.directory, ".image-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	c.pruneMu.Lock()
	defer c.pruneMu.Unlock()
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	return c.prune(imageCacheLimit)
}

func (c *imageCache) prune(limit int64) error {
	files, err := os.ReadDir(c.directory)
	if err != nil {
		return err
	}
	type cachedFile struct {
		name     string
		size     int64
		modified time.Time
	}
	var cached []cachedFile
	var total int64
	for _, file := range files {
		if len(file.Name()) != 68 || !strings.HasSuffix(file.Name(), ".jpg") {
			continue
		}
		if _, err := hex.DecodeString(strings.TrimSuffix(file.Name(), ".jpg")); err != nil {
			continue
		}
		info, err := file.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		cached = append(cached, cachedFile{file.Name(), info.Size(), info.ModTime()})
		total += info.Size()
	}
	sort.Slice(cached, func(i, j int) bool { return cached[i].modified.Before(cached[j].modified) })
	remaining := len(cached)
	for _, file := range cached {
		if remaining <= 2048 && total <= limit && time.Since(file.modified) <= imageCacheAge {
			continue
		}
		if err = os.Remove(filepath.Join(c.directory, file.name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		total -= file.size
		remaining--
	}
	return nil
}
