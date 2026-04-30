package plugin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/DouDOU-start/airgate-core/ent"
	"github.com/DouDOU-start/airgate-core/ent/setting"
)

const DefaultAssetStorageDir = "data/assets"

type assetStorage struct {
	client        *minio.Client
	bucket        string
	prefix        string
	publicBaseURL string
	presignTTL    time.Duration
	localDir      string
	useS3         bool
}

type storedAsset struct {
	ID          string
	ObjectKey   string
	PublicURL   string
	ContentType string
	SizeBytes   int64
}

func newAssetStorage(ctx context.Context, db *ent.Client) (*assetStorage, error) {
	items, err := db.Setting.Query().Where(setting.GroupEQ("storage")).All(ctx)
	if err != nil {
		return nil, err
	}
	cfg := make(map[string]string, len(items))
	for _, item := range items {
		cfg[item.Key] = item.Value
	}

	storage := &assetStorage{
		prefix:        cleanAssetPrefix(cfg["s3_path_prefix"]),
		publicBaseURL: strings.TrimRight(strings.TrimSpace(cfg["s3_public_base_url"]), "/"),
		localDir:      strings.TrimSpace(cfg["local_storage_dir"]),
	}
	if storage.localDir == "" {
		storage.localDir = strings.TrimSpace(os.Getenv("ASSETS_DIR"))
	}
	if storage.localDir == "" {
		storage.localDir = DefaultAssetStorageDir
	}

	ttl := parseInt(cfg["s3_presign_ttl_minutes"])
	if ttl <= 0 {
		ttl = 360
	}
	storage.presignTTL = time.Duration(ttl) * time.Minute

	endpoint := strings.TrimSpace(cfg["s3_endpoint"])
	bucket := strings.TrimSpace(cfg["s3_bucket"])
	accessKey := strings.TrimSpace(cfg["s3_access_key"])
	secretKey := strings.TrimSpace(cfg["s3_secret_key"])
	if endpoint == "" || bucket == "" || accessKey == "" || secretKey == "" {
		return storage, nil
	}

	useSSL := parseBool(cfg["s3_use_ssl"])
	endpoint, endpointUseSSL := normalizeAssetEndpoint(endpoint)
	if endpointUseSSL != nil {
		useSSL = *endpointUseSSL
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
		Region: strings.TrimSpace(cfg["s3_region"]),
	})
	if err != nil {
		return nil, err
	}
	storage.client = client
	storage.bucket = bucket
	storage.useS3 = true
	return storage, nil
}

func (s *assetStorage) store(ctx context.Context, userID int64, scope, contentType, ext string, data []byte) (*storedAsset, error) {
	id, err := newAssetID()
	if err != nil {
		return nil, err
	}
	scope = cleanAssetPrefix(scope)
	if scope == "" {
		scope = "default"
	}
	objectKey := path.Join(s.prefix, scope, fmt.Sprintf("user-%d", userID), id+cleanAssetExtension(ext))
	if s.useS3 {
		_, err = s.client.PutObject(ctx, s.bucket, objectKey, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
			ContentType:  contentType,
			CacheControl: "private, max-age=31536000, immutable",
		})
		if err != nil {
			return nil, err
		}
	} else {
		localPath, err := s.localPath(objectKey)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(localPath, data, 0o644); err != nil {
			return nil, err
		}
	}
	publicURL, err := s.publicURL(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	return &storedAsset{ID: id, ObjectKey: objectKey, PublicURL: publicURL, ContentType: contentType, SizeBytes: int64(len(data))}, nil
}

func (s *assetStorage) publicURL(ctx context.Context, objectKey string) (string, error) {
	if !s.useS3 {
		return "/assets-runtime/" + escapeAssetKey(objectKey), nil
	}
	if s.publicBaseURL != "" {
		return strings.TrimRight(s.publicBaseURL, "/") + "/" + strings.TrimLeft(objectKey, "/"), nil
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, objectKey, s.presignTTL, nil)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *assetStorage) getBytes(ctx context.Context, objectKey string) ([]byte, string, error) {
	if !s.useS3 {
		localPath, err := s.localPath(objectKey)
		if err != nil {
			return nil, "", err
		}
		data, err := os.ReadFile(localPath)
		if err != nil {
			return nil, "", err
		}
		return data, contentTypeForAssetKey(objectKey), nil
	}
	obj, err := s.client.GetObject(ctx, s.bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = obj.Close() }()
	info, err := obj.Stat()
	if err != nil {
		return nil, "", err
	}
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", err
	}
	return data, info.ContentType, nil
}

func (s *assetStorage) localPath(objectKey string) (string, error) {
	clean := strings.TrimPrefix(path.Clean("/"+objectKey), "/")
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("invalid object key")
	}
	return filepath.Join(s.localDir, filepath.FromSlash(clean)), nil
}

func newAssetID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func normalizeAssetEndpoint(endpoint string) (string, *bool) {
	if parsed, err := url.Parse(endpoint); err == nil && parsed.Host != "" {
		useSSL := parsed.Scheme == "https"
		return parsed.Host, &useSSL
	}
	return strings.TrimRight(strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://"), "http://"), "/"), nil
}

func cleanAssetPrefix(prefix string) string {
	prefix = strings.Trim(strings.TrimSpace(prefix), "/")
	if prefix == "." {
		return ""
	}
	return prefix
}

func cleanAssetExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ".bin"
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	for _, r := range ext[1:] {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ".bin"
		}
	}
	return ext
}

func contentTypeForAssetKey(objectKey string) string {
	switch strings.ToLower(path.Ext(objectKey)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "application/octet-stream"
	}
}

func escapeAssetKey(objectKey string) string {
	parts := strings.Split(strings.TrimLeft(objectKey, "/"), "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func parseInt(raw string) int {
	var out int
	_, _ = fmt.Sscanf(strings.TrimSpace(raw), "%d", &out)
	return out
}

func parseBool(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "yes", "on":
		return true
	default:
		return false
	}
}
