package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

const (
	// uploadPartSizeBytes 是分片上传的单片大小，64 MiB × 10000 片约为 S3 单对象上限的一半。
	uploadPartSizeBytes = 64 << 20
	// uploadConcurrency 是并发上传的分片数，内存峰值约为 (uploadConcurrency+1) × uploadPartSizeBytes。
	uploadConcurrency = 3
)

type S3Storage struct {
	EndpointUrl  string
	AccessKey    string
	SecretKey    string
	BucketName   string
	Region       string
	UsePathStyle bool
}

func (s *S3Storage) getS3Client(ctx context.Context) (*s3.Client, error) {
	staticCreds := credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, "")
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(s.Region),
		config.WithCredentialsProvider(staticCreds),
	)
	if err != nil {
		return nil, fmt.Errorf("配置加载失败: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s.EndpointUrl)
		o.UsePathStyle = s.UsePathStyle
	})

	return s3Client, nil
}

func (s *S3Storage) IsAvailable(ctx context.Context) (bool, error) {
	client, err := s.getS3Client(ctx)
	if err != nil {
		return false, err
	}

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.BucketName),
	})

	return err == nil, err
}

// ObjectInfo 描述 S3 上的一个对象。
type ObjectInfo struct {
	Key          string
	Size         int64
	LastModified time.Time
}

func (s *S3Storage) ListObjects(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return nil, err
	}

	var objects []ObjectInfo

	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.BucketName),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("无法获取 S3 对象列表: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key == nil {
				continue
			}

			info := ObjectInfo{Key: *obj.Key}
			if obj.Size != nil {
				info.Size = *obj.Size
			}
			if obj.LastModified != nil {
				info.LastModified = *obj.LastModified
			}
			objects = append(objects, info)
		}
	}

	return objects, nil
}

func (s *S3Storage) ObjectExists(ctx context.Context, key string) (bool, error) {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return false, err
	}

	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(key),
	})
	if err == nil {
		return true, nil
	}

	// 关键：判断错误是否是因为“找不到”
	var nsk *types.NoSuchKey
	var nf *types.NotFound
	if errors.As(err, &nsk) || errors.As(err, &nf) {
		return false, nil
	}

	// 其他错误（如网络问题、权限不足等）
	return false, err
}

func (s S3Storage) GetObjectStream(ctx context.Context, key string) (io.ReadCloser, error) {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return nil, err
	}

	// 2. 发起 GetObject 请求，拿到 S3 的响应体 (它是一个自带网络流的 io.ReadCloser)
	// 注意：这里用的是原生 s3.Client，因为 transfermanager.Downloader 默认是并发切块下载，必须配合支持 WriteAt 的文件句柄使用，不适合纯流式管道。
	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.BucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("获取 S3 对象失败: %w", err)
	}

	return resp.Body, nil
}

func (s S3Storage) UploadStream(ctx context.Context, key string, reader io.Reader) error {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return err
	}

	// 4. 初始化 Transfer Manager
	// 流式上传无法预知对象大小，默认 8 MiB 分片 × 10000 片会在压缩后约 78 GiB 处失败。
	// 这里提高到 64 MiB 分片（上限约 625 GiB），并发降到 3 以控制内存峰值（约 256 MiB）。
	tm := transfermanager.New(s3Client, func(o *transfermanager.Options) {
		o.PartSizeBytes = uploadPartSizeBytes
		o.Concurrency = uploadConcurrency
	})

	// 5. 执行上传
	_, err = tm.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:            aws.String(s.BucketName),
		Key:               aws.String(key),
		Body:              reader,
		ChecksumAlgorithm: tmtypes.ChecksumAlgorithmSha256,
		StorageClass:      tmtypes.StorageClass(types.ObjectStorageClassStandard),
	})

	return err
}
