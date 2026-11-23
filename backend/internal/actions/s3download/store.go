package s3download

import (
	"context"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// objectInfo beschreibt ein Objekt im Bucket.
type objectInfo struct {
	Key         string
	Size        int64
	ContentType string
}

// objectStore ist der Zugriff auf den Bucket (in Tests ersetzbar).
type objectStore interface {
	// List liefert höchstens limit Objekte unterhalb von prefix, rekursiv.
	List(ctx context.Context, prefix string, limit int) ([]objectInfo, error)
	Stat(ctx context.Context, key string) (objectInfo, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
}

type minioConfig struct {
	Endpoint        string
	Secure          bool
	Bucket          string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	PathStyle       bool
}

type minioStore struct {
	client *minio.Client
	bucket string
}

func newMinioStore(c minioConfig) (*minioStore, error) {
	lookup := minio.BucketLookupAuto
	if c.PathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(c.Endpoint, &minio.Options{
		Creds:        credentials.NewStaticV4(c.AccessKeyID, c.SecretAccessKey, ""),
		Secure:       c.Secure,
		Region:       c.Region,
		BucketLookup: lookup,
		// Standard-Transport: Proxy (HTTPS_PROXY/NO_PROXY) und CAs (SSL_CERT_DIR) kommen aus der Umgebung.
		Transport: http.DefaultTransport,
	})
	if err != nil {
		return nil, err
	}
	return &minioStore{client: client, bucket: c.Bucket}, nil
}

func (s *minioStore) List(ctx context.Context, prefix string, limit int) ([]objectInfo, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var infos []objectInfo
	for o := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return nil, o.Err
		}
		infos = append(infos, objectInfo{Key: o.Key, Size: o.Size, ContentType: o.ContentType})
		if len(infos) >= limit {
			break
		}
	}
	return infos, nil
}

func (s *minioStore) Stat(ctx context.Context, key string) (objectInfo, error) {
	o, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return objectInfo{}, err
	}
	return objectInfo{Key: o.Key, Size: o.Size, ContentType: o.ContentType}, nil
}

func (s *minioStore) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
}
