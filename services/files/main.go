package main

import (
	"context"
	"log"
	"net"
	"os"
	"time"

	gv1 "godrive/proto/godrive/v1"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
)

type server struct {
	gv1.UnimplementedFilesServiceServer
	db      *pgxpool.Pool
	storage gv1.StorageServiceClient
}

func main() {
	dsn := env("DB_DSN", "postgres://godrive:godrive@localhost:5432/godrive?sslmode=disable")
	storageAddr := env("STORAGE_ADDR", "storage:50053")

	db, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatal(err)
	}
	if err := migrate(db); err != nil {
		log.Fatal(err)
	}

	storageConn, err := grpc.Dial(storageAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatal(err)
	}

	s := &server{
		db:      db,
		storage: gv1.NewStorageServiceClient(storageConn),
	}

	grpcSrv := grpc.NewServer()
	gv1.RegisterFilesServiceServer(grpcSrv, s)

	lis, err := net.Listen("tcp", ":50052")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("files service on :50052")

	if err := grpcSrv.Serve(lis); err != nil {
		log.Fatal(err)
	}
}

func (s *server) List(ctx context.Context, in *gv1.ListFilesRequest) (*gv1.ListFilesResponse, error) {
	page := in.Page
	if page < 1 {
		page = 1
	}
	pageSize := in.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}

	offset := (page - 1) * pageSize

	rows, err := s.db.Query(ctx, `
		SELECT id,
			owner_id,
			name,
			mime,
			size_bytes,
			created_at,
			COALESCE(version_id, '') AS version_id
		FROM files
		WHERE owner_id = $1
		AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`, in.OwnerId, pageSize, offset)
	if err != nil {
		log.Printf("List query error: %v", err)
		return nil, err
	}
	defer rows.Close()

	var files []*gv1.FileItem
	for rows.Next() {
		var f gv1.FileItem
		var created time.Time
		if err := rows.Scan(&f.Id, &f.OwnerId, &f.Name, &f.Mime, &f.SizeBytes, &created, &f.VersionId); err != nil {
			log.Printf("List scan error: %v", err)
			return nil, err
		}
		f.CreatedAt = created.UTC().Format(time.RFC3339)
		files = append(files, &f)
	}

	if err := rows.Err(); err != nil {
		log.Printf("List rows error: %v", err)
		return nil, err
	}

	nextPage := int32(0)
	if int32(len(files)) == pageSize {
		nextPage = page + 1
	}

	return &gv1.ListFilesResponse{Files: files, NextPage: nextPage}, nil
}

func (s *server) ConfirmUpload(ctx context.Context, in *gv1.ConfirmUploadRequest) (*gv1.ConfirmUploadResponse, error) {
	var (
		id      int64
		created time.Time
		version *string
	)

	err := s.db.QueryRow(ctx, `
INSERT INTO files(owner_id, name, mime, size_bytes, object_key)
VALUES($1, $2, $3, $4, $5)
RETURNING id, created_at, version_id`,
		in.OwnerId, in.Filename, in.Mime, in.SizeBytes, in.ObjectKey,
	).Scan(&id, &created, &version)
	if err != nil {
		return nil, err
	}

	v := ""
	if version != nil {
		v = *version
	}

	return &gv1.ConfirmUploadResponse{
		File: &gv1.FileItem{
			Id:        id,
			OwnerId:   in.OwnerId,
			Name:      in.Filename,
			Mime:      in.Mime,
			SizeBytes: in.SizeBytes,
			CreatedAt: created.UTC().Format(time.RFC3339),
			VersionId: v,
		},
	}, nil
}

func (s *server) GetDownloadURL(ctx context.Context, in *gv1.DownloadURLRequest) (*gv1.DownloadURLResponse, error) {
	var (
		ownerID   int64
		objectKey string
	)

	err := s.db.QueryRow(ctx,
		`SELECT owner_id, object_key FROM files WHERE id = $1`,
		in.FileId,
	).Scan(&ownerID, &objectKey)
	if err != nil {
		return nil, err
	}

	if ownerID != in.OwnerId {
		return nil, grpc.Errorf(grpc.Code(grpc.ErrClientConnClosing), "unauthorized")
	}

	// Ask storage to presign a GET URL for this object.
	p, err := s.storage.PresignDownload(ctx, &gv1.PresignDownloadRequest{ObjectKey: objectKey})
	if err != nil {
		return nil, err
	}

	return &gv1.DownloadURLResponse{DownloadUrl: p.Url, ExpiresAt: p.ExpiresAt}, nil
}

func (s *server) Delete(ctx context.Context, in *gv1.DeleteFileRequest) (*gv1.DeleteFileResponse, error) {
	ct, err := s.db.Exec(ctx, `
		UPDATE files
		SET deleted_at = NOW()
		WHERE id = $1
		AND owner_id = $2
		AND deleted_at IS NULL
		`, in.FileId, in.OwnerId)
	if err != nil {
		return nil, err
	}

	return &gv1.DeleteFileResponse{Ok: ct.RowsAffected() > 0}, nil
}

func migrate(p *pgxpool.Pool) error {
	_, err := p.Exec(context.Background(), `
CREATE TABLE IF NOT EXISTS files (
  id BIGSERIAL PRIMARY KEY,
  owner_id BIGINT NOT NULL,
  name TEXT NOT NULL,
  mime TEXT NOT NULL,
  size_bytes BIGINT NOT NULL,
  object_key TEXT NOT NULL,
  version_id TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ
);`)
	return err
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
