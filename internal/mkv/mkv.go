// Package mkv: pembaca header Matroska minimal untuk mendapatkan durasi + daftar
// track audio/subtitle tanpa membaca lampiran font.
//
// ffprobe harus melewati elemen Attachments (font 20–40 MB di rilis fansub) sebelum
// bisa membaca Tracks, sehingga "probe" bisa menghabiskan puluhan MB. Parser ini
// berhenti tepat setelah Info + Tracks, jadi hanya butuh beberapa KB dari awal file.
package mkv

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
)

// Track: satu track audio/subtitle. Index = index stream global seperti yang dipakai
// ffmpeg (`-map 0:N`), dihitung dari urutan TrackEntry/AttachedFile di file.
type Track struct {
	Index int
	Lang  string
	Title string
	Codec string
}

type Info struct {
	Duration float64 // detik
	Audio    []Track
	Subs     []Track
}

// Batas aman: header normal hanya puluhan KB. Kalau lebih, menyerah dan biarkan
// pemanggil memakai ffprobe.
const maxHeaderBytes = 4 << 20

const (
	idEBML           = 0x1A45DFA3
	idSegment        = 0x18538067
	idInfo           = 0x1549A966
	idTracks         = 0x1654AE6B
	idAttachments    = 0x1941A469
	idCluster        = 0x1F43B675
	idTimestampScale = 0x2AD7B1
	idDuration       = 0x4489
	idTrackEntry     = 0xAE
	idTrackNumber    = 0xD7
	idTrackType      = 0x83
	idCodecID        = 0x86
	idLanguage       = 0x22B59C
	idName           = 0x536E
	idAttachedFile   = 0x61A7
)

var errUnsupported = errors.New("mkv: header tidak dikenali")

// Probe: baca header Matroska sekuensial dari r dan berhenti setelah Tracks.
func Probe(r io.Reader) (Info, error) {
	cr := &countReader{r: bufio.NewReaderSize(r, 32<<10)}
	var info Info

	id, size, err := cr.element()
	if err != nil {
		return info, err
	}
	if id != idEBML {
		return info, errUnsupported
	}
	if err := cr.skip(size); err != nil {
		return info, err
	}

	id, size, err = cr.element()
	if err != nil {
		return info, err
	}
	if id != idSegment {
		return info, errUnsupported
	}
	segEnd := int64(-1)
	if size > 0 {
		segEnd = cr.read + size
	}

	streamIdx := 0
	haveInfo, haveTracks := false, false
	for {
		if segEnd > 0 && cr.read >= segEnd {
			break
		}
		id, size, err = cr.element()
		if err != nil {
			break // EOF / header terpotong: pakai yang sudah didapat
		}
		switch id {
		case idInfo:
			info.Duration, err = parseInfo(cr, size)
			haveInfo = true
		case idTracks:
			var n int
			n, err = parseTracks(cr, size, streamIdx, &info)
			streamIdx += n
			haveTracks = true
		case idAttachments:
			// Bisa muncul sebelum Tracks (jarang): lampiran juga jadi stream di ffmpeg,
			// jadi index harus tetap dihitung.
			var n int
			n, err = countAttachments(cr, size)
			streamIdx += n
		default:
			// Cluster/SeekHead/Cues/…: tidak diperlukan lagi.
			if haveInfo && haveTracks {
				return finish(info)
			}
			if id == idCluster {
				return finish(info)
			}
			err = cr.skip(size)
		}
		if err != nil {
			if haveInfo && haveTracks {
				return finish(info)
			}
			return info, err
		}
		if haveInfo && haveTracks {
			return finish(info)
		}
	}
	return finish(info)
}

func finish(info Info) (Info, error) {
	if info.Duration <= 0 || (len(info.Audio) == 0 && len(info.Subs) == 0) {
		return info, errUnsupported
	}
	return info, nil
}

// parseInfo: TimestampScale (ns) + Duration (dalam satuan scale).
func parseInfo(cr *countReader, size int64) (float64, error) {
	end := cr.read + size
	scale := 1000000.0 // default 1 ms
	var dur float64
	for cr.read < end {
		id, sz, err := cr.element()
		if err != nil {
			return 0, err
		}
		switch id {
		case idTimestampScale:
			b, err := cr.readN(sz)
			if err != nil {
				return 0, err
			}
			scale = float64(readUint(b))
		case idDuration:
			b, err := cr.readN(sz)
			if err != nil {
				return 0, err
			}
			switch len(b) {
			case 4:
				dur = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
			case 8:
				dur = math.Float64frombits(binary.BigEndian.Uint64(b))
			}
		default:
			if err := cr.skip(sz); err != nil {
				return 0, err
			}
		}
	}
	return dur * scale / 1e9, nil
}

// parseTracks: baca semua TrackEntry; mengembalikan jumlah track (untuk index stream).
func parseTracks(cr *countReader, size int64, streamIdx int, info *Info) (int, error) {
	end := cr.read + size
	n := 0
	for cr.read < end {
		id, sz, err := cr.element()
		if err != nil {
			return n, err
		}
		if id != idTrackEntry {
			if err := cr.skip(sz); err != nil {
				return n, err
			}
			continue
		}
		tr, err := parseTrackEntry(cr, sz)
		if err != nil {
			return n, err
		}
		idx := streamIdx + n
		n++
		switch tr.typ {
		case 2:
			info.Audio = append(info.Audio, Track{Index: idx, Lang: tr.lang, Title: tr.name, Codec: tr.codec})
		case 17:
			if !bitmapSub(tr.codec) {
				info.Subs = append(info.Subs, Track{Index: idx, Lang: tr.lang, Title: tr.name, Codec: tr.codec})
			}
		}
	}
	return n, nil
}

type trackEntry struct {
	typ        uint64
	lang, name string
	codec      string
}

func parseTrackEntry(cr *countReader, size int64) (trackEntry, error) {
	end := cr.read + size
	var t trackEntry
	for cr.read < end {
		id, sz, err := cr.element()
		if err != nil {
			return t, err
		}
		switch id {
		case idTrackType, idTrackNumber:
			b, err := cr.readN(sz)
			if err != nil {
				return t, err
			}
			if id == idTrackType {
				t.typ = readUint(b)
			}
		case idCodecID, idLanguage, idName:
			b, err := cr.readN(sz)
			if err != nil {
				return t, err
			}
			s := strings.TrimRight(string(b), "\x00")
			switch id {
			case idCodecID:
				t.codec = s
			case idLanguage:
				t.lang = s
			case idName:
				t.name = s
			}
		default:
			if err := cr.skip(sz); err != nil {
				return t, err
			}
		}
	}
	return t, nil
}

// countAttachments: lampiran juga tampil sebagai stream di ffmpeg, jadi index ikut naik.
func countAttachments(cr *countReader, size int64) (int, error) {
	end := cr.read + size
	n := 0
	for cr.read < end {
		id, sz, err := cr.element()
		if err != nil {
			return n, err
		}
		if id == idAttachedFile {
			n++
			if err := cr.skip(sz); err != nil {
				return n, err
			}
			continue
		}
		if err := cr.skip(sz); err != nil {
			return n, err
		}
	}
	return n, nil
}

// bitmapSub: subtitle bitmap tidak bisa dikonversi ke WebVTT → tidak ditawarkan.
func bitmapSub(codec string) bool {
	switch strings.ToUpper(codec) {
	case "S_HDMV/PGS", "S_VOBSUB", "S_DVBSUB", "S_HDMV/TEXTST":
		return true
	}
	return false
}

// countReader: reader + posisi, dibatasi maxHeaderBytes supaya tidak pernah membaca file penuh.
type countReader struct {
	r    *bufio.Reader
	read int64
}

func (c *countReader) element() (id uint64, size int64, err error) {
	id, err = c.readID()
	if err != nil {
		return 0, 0, err
	}
	size, err = c.readSize()
	return id, size, err
}

func (c *countReader) readByte() (byte, error) {
	if c.read >= maxHeaderBytes {
		return 0, fmt.Errorf("mkv: header melebihi %d byte", maxHeaderBytes)
	}
	b, err := c.r.ReadByte()
	if err == nil {
		c.read++
	}
	return b, err
}

func (c *countReader) readID() (uint64, error) {
	b0, err := c.readByte()
	if err != nil {
		return 0, err
	}
	if b0 == 0 {
		return 0, errUnsupported
	}
	length := vintLength(b0)
	if length > 4 {
		return 0, errUnsupported
	}
	v := uint64(b0)
	for i := 1; i < length; i++ {
		b, err := c.readByte()
		if err != nil {
			return 0, err
		}
		v = v<<8 | uint64(b)
	}
	return v, nil
}

func (c *countReader) readSize() (int64, error) {
	b0, err := c.readByte()
	if err != nil {
		return 0, err
	}
	if b0 == 0 {
		return 0, errUnsupported
	}
	length := vintLength(b0)
	v := uint64(b0 & (0xFF >> length))
	unknown := b0&(0xFF>>length) == (0xFF >> length)
	for i := 1; i < length; i++ {
		b, err := c.readByte()
		if err != nil {
			return 0, err
		}
		v = v<<8 | uint64(b)
		unknown = unknown && b == 0xFF
	}
	if unknown {
		return -1, nil // ukuran tidak diketahui (Segment streaming)
	}
	if v > math.MaxInt64 {
		return 0, errUnsupported
	}
	return int64(v), nil
}

func (c *countReader) readN(n int64) ([]byte, error) {
	if n < 0 || n > maxHeaderBytes {
		return nil, errUnsupported
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return nil, err
	}
	c.read += n
	return buf, nil
}

// skip: buang payload sebesar n byte (tanpa menyimpan di memori).
func (c *countReader) skip(n int64) error {
	if n <= 0 {
		return nil
	}
	if c.read+n > maxHeaderBytes {
		return fmt.Errorf("mkv: header melebihi %d byte", maxHeaderBytes)
	}
	got, err := io.CopyN(io.Discard, c.r, n)
	c.read += got
	return err
}

func vintLength(b byte) int {
	for i := 0; i < 8; i++ {
		if b&(0x80>>i) != 0 {
			return i + 1
		}
	}
	return 8
}

func readUint(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}
