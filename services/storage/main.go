// Package main implements the Storage gRPC service.
// Responsibilities:
//   - Create short-lived presigned URLs for uploads (PUT) and downloads (GET)
//   - Talk to an S3-compatible backend (MinIO in dev, AWS S3 in prod)
//   - Ensure target bucket exists on startup (dev convenience)

package main

import (
	"context" // carry deadlines and cancellation into SDK calls
	"log"     // basic logging
	"net"     // TCP listener
	"os"      // read env config
	"time"    // presign expiries

	gv1 "github.com/VishnuVardhanAI/godrive/proto/godrive/v1" // generated gRPC stubs
	"google.golang.org/grpc"                                  // gRPC server

	"github.com/minio/minio-go/v7" // MinIO/S3 SDK
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// server holds the MinIO client and bucket name.
type server struct {
	gv1.UnimplementedStorageServiceServer

	mc     *minio.Client // MinIO client
	bucket string        // bucket where we store objects
}

func main() {
	// Read storage configuration from environment.
	endpoint := get("S3_ENDPOINT", "http://localhost:9000")
	ak := get("S3_ACCESS_KEY", "godrive")
	sk := get("S3_SECRET_KEY", "godrivepass")
	bucket := get("S3_BUCKET", "godrive")

	// Create MinIO client. Secure=false for local dev; enable TLS in prod.
	mc, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(ak, sk, ""),
		Secure: false,
		Region: "us-east-1",
	})
	if err != nil {
		log.Fatal(err)
	}
	// Ensure bucket exists (idempotent) so presign works immediately in dev.
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

	// Start gRPC server on :50053
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

// PresignUpload returns a short-lived HTTP PUT URL so clients can upload
// directly to object storage without routing bytes through the gateway.
func (s *server) PresignUpload(ctx context.Context, in *gv1.PresignUploadRequest) (*gv1.PresignUploadResponse, error) {
	// Choose an expiry window. Keep it short (e.g., 10–15 minutes).
	exp := time.Now().Add(15 * time.Minute)

	// Ask MinIO to generate a presigned PUT URL for this object key.
	url, err := s.mc.PresignedPutObject(ctx, s.bucket, in.ObjectKey, time.Until(exp))
	if err != nil {
		return nil, err
	}

	return &gv1.PresignUploadResponse{
		Url:       url.String(),        // client uploads bytes here via HTTP PUT
		Headers:   map[string]string{}, // e.g., Content-Type if your policy requires
		ExpiresAt: exp.Format(time.RFC3339),
	}, nil
}

// PresignDownload returns a short-lived HTTP GET URL for clients to download
// an object directly from storage.
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

// get is a small env helper with default.
func get(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}

	return d
}
