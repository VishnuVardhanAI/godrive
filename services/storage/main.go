package main

import (
	"context"
	"log"
	"net"
	"os"
	"time"

	gv1 "godrive/proto/godrive/v1"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"google.golang.org/grpc"
)

type server struct {
	gv1.UnimplementedStorageServiceServer
	mc     *minio.Client
	bucket string
}

func main() {
	endpoint := env("S3_ENDPOINT", "minio:9000")
	ak := env("S3_ACCESS_KEY", "godrive")
	sk := env("S3_SECRET_KEY", "godrivepass")
	bucket := env("S3_BUCKET", "godrive")

	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(ak, sk, ""),
		Secure: false,
		Region: "us-east-1",
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	exists, err := mc.BucketExists(ctx, bucket)
	if err != nil {
		log.Fatal(err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			log.Fatal(err)
		}
	}

	s := &server{mc: mc, bucket: bucket}
	grpcSrv := grpc.NewServer()
	gv1.RegisterStorageServiceServer(grpcSrv, s)

	lis, err := net.Listen("tcp", ":50053")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("storage service on :50053")

	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

func (s *server) PresignUpload(ctx context.Context, in *gv1.PresignUploadRequest) (*gv1.PresignUploadResponse, error) {
	exp := time.Now().Add(15 * time.Minute)

	url, err := s.mc.PresignedPutObject(ctx, s.bucket, in.ObjectKey, time.Until(exp))
	if err != nil {
		return nil, err
	}

	return &gv1.PresignUploadResponse{
		Url:       url.String(),
		Headers:   map[string]string{},
		ExpiresAt: exp.Format(time.RFC3339),
	}, nil
}

func (s *server) PresignDownload(ctx context.Context, in *gv1.PresignDownloadRequest) (*gv1.PresignDownloadResponse, error) {
	exp := time.Now().Add(15 * time.Minute)

	url, err := s.mc.PresignedGetObject(ctx, s.bucket, in.ObjectKey, time.Until(exp), nil)
	if err != nil {
		return nil, err
	}

	return &gv1.PresignDownloadResponse{
		Url:       url.String(),
		ExpiresAt: exp.Format(time.RFC3339),
	}, nil
}

func (s *server) DeleteObject(ctx context.Context, in *gv1.DeleteObjectRequest) (*gv1.DeleteObjectResponse, error) {
	err := s.mc.RemoveObject(ctx, s.bucket, in.ObjectKey, minio.RemoveObjectOptions{})
	if err != nil {
		return nil, err
	}
	return &gv1.DeleteObjectResponse{Ok: true}, nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
