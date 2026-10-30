// Package hls serves the selected video as an on-demand HLS stream. mkvgo
// (pure Go, no ffmpeg) reads the server's own /stream endpoint over HTTP Range,
// demuxes the tracks and remuxes them into CMAF/fMP4 segments; nothing is
// re-encoded and nothing is written to disk.
//
// mkvgo never transcodes, so the browser must support the source codecs
// (H.264/HEVC/AV1 video, AAC/Opus/AC-3/E-AC-3 audio). Segments are built on
// demand by mp4.HLSPlan and kept in a small RAM cache.
package hls

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gravity-zero/mkvgo/httpfs"
	"github.com/gravity-zero/mkvgo/mp4"
)

const (
	// segmentSeconds is the CMAF segment length handed to the planner.
	segmentSeconds = 4
	// cacheLimit bounds the RAM used by already-produced resources.
	cacheLimit = 24 << 20
	// planTimeout bounds the head reads a plan needs over /stream.
	planTimeout = 30 * time.Second
)

// ErrNoSuchFile is returned for names outside the session.
var ErrNoSuchFile = errors.New("sesi hls tidak tersedia untuk file ini")

// Subtitle is one text subtitle rendition of the file.
type Subtitle struct {
	File   string // WebVTT utuh untuk player: subs0.vtt
	Name   string
	Format string // selalu vtt
	index  int    // rendition sub%d (1-based) di plan
}

// Status is a snapshot of the running session for the state JSON.
type Status struct {
	Index     int
	Ready     bool
	Live      bool // plan gagal: playlist tidak bisa disusun
	Transcode bool // dipertahankan demi state JSON lama; mkvgo tidak meng-encode
	AudioKbps int
	Playlist  string
	Subs      []Subtitle
	Error     string
}

// Job is one HLS session: the mkvgo plan plus a RAM cache of what it served.
type Job struct {
	index int
	ready bool
	err   string

	mu   sync.Mutex
	plan *mp4.HLSPlan
	subs []Subtitle
}

// Manager keeps a single HLS session: only one episode is served at a time.
type Manager struct {
	mu  sync.Mutex
	job *Job
}

func NewManager() *Manager { return &Manager{} }

// Start publishes the playlist of index from streamURL (the server's own
// /stream endpoint). Planning runs in the background; requests wait for it.
// mkvgo reads the duration and track layout from the file header itself.
func (m *Manager) Start(ctx context.Context, index int, streamURL string) {
	m.mu.Lock()
	if m.job != nil {
		if m.job.index == index {
			m.mu.Unlock()
			return
		}
		m.job = nil
	}
	job := &Job{index: index}
	m.job = job
	m.mu.Unlock()

	go job.prepare(ctx, streamURL, m)
}

func (j *Job) prepare(ctx context.Context, streamURL string, m *Manager) {
	ctx, cancel := context.WithTimeout(ctx, planTimeout)
	defer cancel()
	opts := mp4.Options{SegmentMs: segmentSeconds * 1000}
	if httpfs.IsURL(streamURL) {
		opts.FS = httpfs.New().Port()
		opts.StreamFromFS = true
	}

	plan, err := mp4.PlanHLS(ctx, streamURL, opts)
	j.mu.Lock()
	defer j.mu.Unlock()
	if m.current() != j {
		return // sesi lain sudah menggantikan
	}
	if err != nil {
		j.err = err.Error()
		log.Printf("hls: index %d: %v", j.index, err)
		return
	}
	j.plan, j.subs, j.ready = plan, subtitleList(plan), true
	log.Printf("hls siap: index %d (%d segmen, %d subtitle)", j.index, plan.NumSegments(), len(j.subs))
}

// subtitleList publishes every text subtitle rendition as one whole WebVTT
// file, subsN.vtt (source index N). Bitmap subtitles never reach a plan.
func subtitleList(plan *mp4.HLSPlan) []Subtitle {
	var out []Subtitle
	for _, name := range plan.Resources() {
		n, ok := strings.CutPrefix(name, "sub")
		if !ok || !strings.HasSuffix(name, ".m3u8") {
			continue
		}
		i, err := strconv.Atoi(strings.TrimSuffix(n, ".m3u8"))
		if err != nil || i < 1 {
			continue
		}
		out = append(out, Subtitle{
			File:   fmt.Sprintf("subs%d.vtt", i-1),
			Name:   fmt.Sprintf("Subtitle %d", i),
			Format: "vtt",
			index:  i,
		})
	}
	return out
}

// Stop ends the running session and drops its cached segments.
func (m *Manager) Stop() {
	m.mu.Lock()
	m.job = nil
	m.mu.Unlock()
}

func (m *Manager) current() *Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.job
}

func (m *Manager) currentIndex(index int) *Job {
	job := m.current()
	if job == nil || job.index != index {
		return nil
	}
	return job
}

// wait blocks until the plan is ready; nil when the session is gone or failed.
func (j *Job) wait(ctx context.Context) *mp4.HLSPlan {
	for {
		j.mu.Lock()
		plan, err := j.plan, j.err
		j.mu.Unlock()
		switch {
		case plan != nil:
			return plan
		case err != "":
			return nil
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// Status reports the current session for state JSON.
func (m *Manager) Status() (Status, bool) {
	job := m.current()
	if job == nil {
		return Status{}, false
	}
	job.mu.Lock()
	defer job.mu.Unlock()
	return Status{Index: job.index, Ready: job.ready, Live: job.err != "",
		Playlist: "master.m3u8", Subs: append([]Subtitle(nil), job.subs...), Error: job.err}, true
}

// Playlist returns one playlist resource of the session for index.
func (m *Manager) Playlist(ctx context.Context, index int, name string) ([]byte, bool) {
	job := m.currentIndex(index)
	if job == nil {
		return nil, false
	}
	plan := job.wait(ctx)
	if plan == nil {
		return nil, false
	}
	data, _, err := plan.Resource(ctx, name)
	return data, err == nil
}

// Segment returns one media/init/subtitle resource, built on demand.
func (m *Manager) Segment(ctx context.Context, index int, name string) ([]byte, string, error) {
	job := m.currentIndex(index)
	if job == nil {
		return nil, "", ErrNoSuchFile
	}
	plan := job.wait(ctx)
	if plan == nil {
		return nil, "", ErrNoSuchFile
	}
	return plan.Resource(ctx, name)
}

// Subtitle returns the whole WebVTT of one subtitle rendition (subsN.vtt).
func (m *Manager) Subtitle(ctx context.Context, index int, name string) ([]byte, error) {
	job := m.currentIndex(index)
	if job == nil {
		return nil, ErrNoSuchFile
	}
	job.mu.Lock()
	subs := append([]Subtitle(nil), job.subs...)
	job.mu.Unlock()
	for _, sub := range subs {
		if sub.File == name {
			plan := job.wait(ctx)
			if plan == nil {
				return nil, ErrNoSuchFile
			}
			return plan.Subtitle(ctx, sub.index-1)
		}
	}
	return nil, ErrNoSuchFile
}
