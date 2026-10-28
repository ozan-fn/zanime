package tests

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	"zanime/internal/config"
	"zanime/internal/memstorage"
	"zanime/internal/s3store"
)

const torrentCacheLimit = 64 << 20

func TestS3PutObjectStreamsWithoutDisk(t *testing.T) {
	const want = "RAM-only upload test"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/test-bucket/test-object" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if string(got) != want {
			t.Errorf("body = %q, want %q", got, want)
		}
		w.Header().Set("ETag", `"test-etag"`)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := testS3Client(t, server.URL)
	if err := putS3Object(context.Background(), client, "test-bucket", "test-object", int64(len(want)), strings.NewReader(want)); err != nil {
		t.Fatalf("PutObject: %v", err)
	}
}

// Set S3_LIVE_TEST=1 to upload a permitted torrent's video files to a private bucket.
// A presigned URL is a bearer link and expires after 15 minutes.
func TestS3LiveUploadMagnet(t *testing.T) {
	if os.Getenv("S3_LIVE_TEST") != "1" {
		t.Skip("set S3_LIVE_TEST=1 to upload to Tigris")
	}
	if err := config.LoadDotEnv("../.env"); err != nil {
		t.Fatalf("load ../.env: %v", err)
	}
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		t.Fatal("S3_BUCKET is required; set it in .env")
	}
	magnetURI := os.Getenv("S3_TEST_MAGNET")
	if magnetURI == "" {
		t.Fatal("S3_TEST_MAGNET is required; use a torrent you have permission to upload")
	}
	endpoint := envOr("AWS_ENDPOINT_URL_S3", "https://t3.storage.dev")
	region := envOr("AWS_REGION", "auto")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()

	client, err := newS3Client(ctx, endpoint, region, false)
	if err != nil {
		t.Fatalf("configure S3 client: %v", err)
	}
	files, err := uploadTorrentVideos(ctx, client, bucket, magnetURI)
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		link, err := presignDownload(ctx, client, bucket, file.key)
		if err != nil {
			t.Fatalf("presign %s: %v", file.key, err)
		}
		t.Logf("s3://%s/%s", bucket, file.key)
		t.Logf("private download link (expires in 15 minutes): %s", link)
	}
}

// Small live check of the store the app uses: usage listing and ranged reads.
// It writes to an isolated zanime-test/ prefix and deletes the object again.
func TestS3LiveStoreUsageAndRange(t *testing.T) {
	if os.Getenv("S3_LIVE_TEST") != "1" {
		t.Skip("set S3_LIVE_TEST=1 to check the private bucket")
	}
	if err := config.LoadDotEnv("../.env"); err != nil {
		t.Fatalf("load ../.env: %v", err)
	}
	bucket := os.Getenv("S3_BUCKET")
	if bucket == "" {
		t.Fatal("S3_BUCKET is required; set it in .env")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := newS3Client(ctx, envOr("AWS_ENDPOINT_URL_S3", "https://t3.storage.dev"), envOr("AWS_REGION", "auto"), false)
	if err != nil {
		t.Fatalf("configure S3 client: %v", err)
	}

	content := []byte("range test 0123456789")
	store := s3store.New(client, bucket, fmt.Sprintf("zanime-test/%d", time.Now().UnixNano()), 1<<40)
	key := store.Key("hash", "clip.mp4")
	if _, err := store.Put(ctx, key, int64(len(content)), bytes.NewReader(content), nil); err != nil {
		t.Fatalf("Put: %v", err)
	}
	t.Cleanup(func() {
		if _, err := client.DeleteObject(context.Background(), &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		}); err != nil {
			t.Errorf("cleanup %s: %v", key, err)
		}
	})

	deadline := time.Now().Add(8 * time.Second)
	for store.Used() < int64(len(content)) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if used := store.Used(); used < int64(len(content)) {
		t.Fatalf("usage = %d B, want at least %d", used, len(content))
	}
	if !store.Has(ctx, key, int64(len(content))) {
		t.Fatal("Has reports a missing object after Put; replay would re-upload")
	}

	out, err := store.Get(ctx, key, "bytes=6-9")
	if err != nil {
		t.Fatalf("range Get: %v", err)
	}
	defer func() { _ = out.Body.Close() }()
	got, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read range: %v", err)
	}
	if string(got) != "test" {
		t.Fatalf("range body = %q, want %q", got, "test")
	}

	if app, err := s3store.FromEnv(ctx); err == nil && app != nil {
		t.Logf("bucket %s/%s saat ini: %d B dari batas %d B", app.Bucket(), app.Prefix(), app.Used(), app.Limit())
	}
}

type uploadedFile struct {
	key string
}

func uploadTorrentVideos(ctx context.Context, s3Client *s3.Client, bucket, magnetURI string) ([]uploadedFile, error) {
	mem := memstorage.New(torrentCacheLimit)
	cfg := torrent.NewDefaultClientConfig()
	cfg.DataDir = ""
	cfg.DefaultStorage = mem
	client, err := torrent.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("create in-memory torrent client: %w", err)
	}
	defer client.Close()

	spec, err := torrent.TorrentSpecFromMagnetUri(magnetURI)
	if err != nil {
		return nil, fmt.Errorf("parse magnet: %w", err)
	}
	t, _, err := client.AddTorrentSpec(spec)
	if err != nil {
		return nil, fmt.Errorf("add torrent: %w", err)
	}
	select {
	case <-t.GotInfo():
	case <-ctx.Done():
		return nil, fmt.Errorf("wait for torrent metadata: %w", ctx.Err())
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case ev := <-mem.Evicted():
				if ev.Hash == t.InfoHash() && ev.Index >= 0 && ev.Index < t.NumPieces() {
					t.Piece(ev.Index).UpdateCompletion()
				}
			case <-done:
				return
			}
		}
	}()

	var uploaded []uploadedFile
	for _, file := range t.Files() {
		if !isVideoFile(file.Path()) {
			continue
		}
		reader := file.NewReader()
		reader.SetReadahead(4 << 20)
		key := path.Join(t.InfoHash().String(), file.Path())
		err := putS3Object(ctx, s3Client, bucket, key, file.Length(), reader)
		_ = reader.Close()
		if err != nil {
			return nil, fmt.Errorf("upload %s: %w", file.Path(), err)
		}
		uploaded = append(uploaded, uploadedFile{key: key})
	}
	if len(uploaded) == 0 {
		return nil, fmt.Errorf("torrent contains no video files")
	}
	return uploaded, nil
}

func isVideoFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".mkv", ".mp4", ".m4v", ".webm", ".avi", ".mov", ".ts", ".m2ts":
		return true
	default:
		return false
	}
}

func putS3Object(ctx context.Context, client *s3.Client, bucket, key string, size int64, body io.Reader) error {
	uploader := transfermanager.New(client, func(options *transfermanager.Options) {
		options.PartSizeBytes = 8 << 20
		options.Concurrency = 1
	})
	_, err := uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentLength: aws.Int64(size),
	})
	return err
}

func presignDownload(ctx context.Context, client *s3.Client, bucket, key string) (string, error) {
	result, err := s3.NewPresignClient(client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

func newS3Client(ctx context.Context, endpoint, region string, pathStyle bool) (*s3.Client, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, err
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.Region = region
		options.UsePathStyle = pathStyle
	}), nil
}

func testS3Client(t *testing.T, endpoint string) *s3.Client {
	t.Helper()
	cfg := aws.Config{
		Region: "auto",
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{AccessKeyID: "local-test", SecretAccessKey: "local-test", Source: "test"}, nil
		}),
	}
	return s3.NewFromConfig(cfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
