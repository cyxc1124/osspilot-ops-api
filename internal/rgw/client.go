package rgw

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var ErrNotConfigured = errors.New("S3/RGW is not configured")

type Client struct {
	s3 *s3.Client
}

func New(endpoint, accessKey, secretKey string) *Client {
	endpoint = strings.TrimRight(strings.TrimSpace(endpoint), "/")
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil
	}
	cli := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider(accessKey, secretKey, ""),
	}, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	return &Client{s3: cli}
}

type BucketInfo struct {
	Name         string
	CreationDate *time.Time
}

func (c *Client) ListBuckets(ctx context.Context) ([]BucketInfo, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	out, err := c.s3.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}
	items := make([]BucketInfo, 0, len(out.Buckets))
	for _, b := range out.Buckets {
		if b.Name == nil {
			continue
		}
		items = append(items, BucketInfo{Name: *b.Name, CreationDate: b.CreationDate})
	}
	return items, nil
}

func (c *Client) GetBucketPolicy(ctx context.Context, bucket string) (map[string]any, error) {
	if c == nil {
		return nil, ErrNotConfigured
	}
	out, err := c.s3.GetBucketPolicy(ctx, &s3.GetBucketPolicyInput{Bucket: aws.String(bucket)})
	if err != nil {
		// ponytail: RGW often returns NoSuchBucketPolicy as a string code, not a typed error.
		if strings.Contains(err.Error(), "NoSuchBucketPolicy") {
			return nil, nil
		}
		return nil, err
	}
	if out.Policy == nil || *out.Policy == "" {
		return nil, nil
	}
	var policy map[string]any
	if err := json.Unmarshal([]byte(*out.Policy), &policy); err != nil {
		return nil, err
	}
	return policy, nil
}

func (c *Client) PutBucketPolicy(ctx context.Context, bucket string, policy map[string]any) error {
	if c == nil {
		return ErrNotConfigured
	}
	raw, err := json.Marshal(policy)
	if err != nil {
		return err
	}
	_, err = c.s3.PutBucketPolicy(ctx, &s3.PutBucketPolicyInput{
		Bucket: aws.String(bucket), Policy: aws.String(string(raw)),
	})
	return err
}

func (c *Client) DeleteBucketPolicy(ctx context.Context, bucket string) error {
	if c == nil {
		return ErrNotConfigured
	}
	_, err := c.s3.DeleteBucketPolicy(ctx, &s3.DeleteBucketPolicyInput{Bucket: aws.String(bucket)})
	return err
}
