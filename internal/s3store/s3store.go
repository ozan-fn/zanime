// Package s3store keeps torrent videos in a private S3-compatible bucket.
// Uploads stream straight from the torrent reader (no local file), and the
// oldest objects are removed when the configured byte budget is full.
package s3store

import (
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"zanime/internal/config"
)

const (
	// DefaultPrefix is the folder inside the bucket.
	DefaultPrefix = "zanime"
	// DefaultLimit is the bucket budget: 4 GiB.
	DefaultLimit = 4 << 30
	usageTTL     = 15 * time.Second
)

// Store is a private bucket with one shared byte budget.
type Store struct {
	client *s3.Client
	bucket string
	prefix string
	limit  int64

	mu         sync.Mutex
	used       int64
	usedAt     time.Time
	refreshing bool
}

// FromEnv builds the store from S3_BUCKET/AWS_*/S3_PREFIX/S3_LIMIT_MB. It
// returns nil (and no error) when S3_BUCKET is empty, which keeps the app
// streaming from the torrent alone.
func FromEnv(ctx context.Context) (*Store, error) {
	bucket := strings.TrimSpace(os.Getenv("S3_BUCKET"))
	if bucket == "" {
		return nil, nil
	}
	region := config.String("AWS_REGION", "auto")
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(config.String("AWS_ENDPOINT_URL_S3", "https://t3.storage.dev"))
		o.Region = region
	})
	limitMB := config.Int("S3_LIMIT_MB", DefaultLimit>>20)
	return New(client, bucket, config.String("S3_PREFIX", DefaultPrefix), limitMB<<20), nil
}

// New builds a store around an already configured client.
func New(client *s3.Client, bucket, prefix string, limit int64) *Store {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "" {
		prefix = DefaultPrefix
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	return &Store{client: client, bucket: bucket, prefix: prefix, limit: limit}
}

func (s *Store) Bucket() string { return s.bucket }
func (s *Store) Prefix() string { return s.prefix }
func (s *Store) Limit() int64   { return s.limit }

// Key maps a torrent file to <prefix>/<infohash>/<file>. Cleaning the name
// keeps a torrent from writing outside its own folder.
func (s *Store) Key(infoHash, name string) string {
	clean := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, `\`, "/")), "/")
	if clean == "" || clean == "." {
		clean = "video"
	}
	return path.Join(s.prefix, infoHash, clean)
}

// Used returns the last known bucket usage. A stale value triggers a
// background refresh, so the status socket never waits on the network.
func (s *Store) Used() int64 {
	s.mu.Lock()
	used := s.used
	start := time.Since(s.usedAt) > usageTTL && !s.refreshing
	if start {
		s.refreshing = true
	}
	s.mu.Unlock()
	if start {
		go s.refresh()
	}
	return used
}

func (s *Store) refresh() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, total, err := s.list(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshing = false
	if err == nil {
		s.used = total
		s.usedAt = time.Now()
	}
}

// Put evicts the oldest objects until size fits, then uploads body as key.
// partSize is the upload buffer: the part is sent once it is full. keep names
// objects that must survive (the video currently playing). It returns the
// evicted keys so callers can drop their stale cache markers.
func (s *Store) Put(ctx context.Context, key string, size, partSize int64, body io.Reader, keep map[string]bool) ([]string, error) {
	if size <= 0 {
		return nil, fmt.Errorf("empty file")
	}
	objects, used, err := s.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("list bucket: %w", err)
	}
	// A PUT replaces the object at this key, so it is neither counted nor a
	// eviction candidate.
	kept := objects[:0]
	for _, o := range objects {
		if o.key == key {
			used -= o.size
			continue
		}
		kept = append(kept, o)
	}
	drop, err := evictions(kept, used, size, s.limit, keep)
	if err != nil {
		return nil, err
	}
	evicted := make([]string, 0, len(drop))
	for _, old := range drop {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(old.key),
		}); err != nil {
			return evicted, fmt.Errorf("delete %s: %w", old.key, err)
		}
		used -= old.size
		evicted = append(evicted, old.key)
	}

	if partSize <= 0 {
		partSize = 8 << 20
	}
	partSize = max(partSize, 5<<20) // S3 minimum per part
	partSize = min(partSize, 5<<30) // S3 maximum per part
	uploader := transfermanager.New(s.client, func(o *transfermanager.Options) {
		o.PartSizeBytes = partSize
		o.Concurrency = 1
	})
	_, err = uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
		ContentType:   aws.String(contentType(key)),
	})
	if err != nil {
		return evicted, fmt.Errorf("upload %s: %w", key, err)
	}

	s.mu.Lock()
	s.used = used + size
	s.usedAt = time.Now()
	s.mu.Unlock()
	return evicted, nil
}

// Has reports whether key already exists with exactly size bytes. Any lookup
// error counts as "missing", so an S3 hiccup only costs a re-upload.
func (s *Store) Has(ctx context.Context, key string, size int64) bool {
	out, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err == nil && aws.ToInt64(out.ContentLength) == size
}

// Presign returns a temporary GET URL so the browser can read the object
// straight from S3 instead of through this server.
func (s *Store) Presign(ctx context.Context, key string, ttl time.Duration) (string, error) {
	out, err := s3.NewPresignClient(s.client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	return out.URL, nil
}

// Get streams an object, passing the client's Range header through.
func (s *Store) Get(ctx context.Context, key, rangeHeader string) (*s3.GetObjectOutput, error) {
	in := &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)}
	if rangeHeader != "" {
		in.Range = aws.String(rangeHeader)
	}
	return s.client.GetObject(ctx, in)
}

type object struct {
	key      string
	size     int64
	modified time.Time
}

func (s *Store) list(ctx context.Context) ([]object, int64, error) {
	var out []object
	var total int64
	pages := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(s.prefix + "/"),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, 0, err
		}
		for _, item := range page.Contents {
			size := aws.ToInt64(item.Size)
			total += size
			out = append(out, object{key: aws.ToString(item.Key), size: size, modified: aws.ToTime(item.LastModified)})
		}
	}
	return out, total, nil
}

// evictions picks the oldest objects to delete so used+need fits in limit.
// Objects in keep are never picked; if the budget still cannot fit, it errors.
func evictions(objects []object, used, need, limit int64, keep map[string]bool) ([]object, error) {
	if need > limit {
		return nil, fmt.Errorf("file %d B is larger than the S3 limit %d B", need, limit)
	}
	if used+need <= limit {
		return nil, nil
	}
	candidates := append([]object(nil), objects...)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].modified.Before(candidates[j].modified)
	})
	remaining := used
	var drop []object
	for _, o := range candidates {
		if remaining+need <= limit {
			break
		}
		if keep[o.key] {
			continue
		}
		drop = append(drop, o)
		remaining -= o.size
	}
	if remaining+need > limit {
		return nil, fmt.Errorf("S3 limit %d B full: %d B used and %d B needed", limit, remaining, need)
	}
	return drop, nil
}

func contentType(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".mkv":
		return "video/x-matroska"
	case ".mp4", ".m4v":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	case ".avi":
		return "video/x-msvideo"
	case ".mov":
		return "video/quicktime"
	case ".ts", ".m2ts":
		return "video/mp2t"
	}
	return "application/octet-stream"
}
