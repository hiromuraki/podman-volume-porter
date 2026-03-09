package core

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type S3Storage struct {
	Endpoint  string
	AccessKey string
	SecretKey string
}

func (s *S3Storage) getS3Client(ctx context.Context) (*s3.Client, error) {
	staticCreds := credentials.NewStaticCredentialsProvider(s.AccessKey, s.SecretKey, "")
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(staticCreds),
	)
	if err != nil {
		return nil, fmt.Errorf("配置加载失败: %w", err)
	}

	s3Client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(s.Endpoint)
		o.UsePathStyle = true
	})

	return s3Client, nil
}

func (s *S3Storage) ListObjectKeysWithPrefix(ctx context.Context, bucketName string, prefix string) ([]string, error) {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return nil, err
	}

	var objectKeys []string

	paginator := s3.NewListObjectsV2Paginator(s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("无法获取 S3 对象列表: %w", err)
		}

		for _, obj := range page.Contents {
			if obj.Key != nil {
				objectKeys = append(objectKeys, *obj.Key)
			}
		}
	}

	return objectKeys, nil

}

func (s *S3Storage) ObjectExists(ctx context.Context, bucket string, key string) (bool, error) {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return false, err
	}

	_, err = s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
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

func (s S3Storage) GetObjectStream(ctx context.Context, bucket string, key string) (io.Reader, error) {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return nil, err
	}

	// 2. 发起 GetObject 请求，拿到 S3 的响应体 (它是一个自带网络流的 io.ReadCloser)
	// ⚠️ 注意：这里用的是原生 s3.Client，因为 transfermanager.Downloader 默认是并发切块下载，必须配合支持 WriteAt 的文件句柄使用，不适合纯流式管道。
	resp, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("获取 S3 对象失败: %w", err)
	}

	return resp.Body, nil
}

func (s S3Storage) UploadStream(ctx context.Context, bucket string, key string, reader io.Reader) error {
	s3Client, err := s.getS3Client(ctx)
	if err != nil {
		return err
	}

	// 4. 初始化 Transfer Manager
	tm := transfermanager.New(s3Client)

	// 5. 执行上传
	_, err = tm.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   reader,
	})

	return err
}
