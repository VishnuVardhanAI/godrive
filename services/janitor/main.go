package main

import (
	"context"
	"log"
	"os"
	"time"

	gv1 "godrive/proto/godrive/v1"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
)

type janitor struct {
	db      *pgxpool.Pool
	storage gv1.StorageServiceClient
	// how long we wait before hard-deleting (after user delete)
	gracePeriod time.Duration
}

func main() {
	dsn := env("DB_DSN", "postgres://godrive:godrive@postgres:5432/godrive?sslmode=disable")
	storageAddr := env("STORAGE_ADDR", "storage:50053")
	// e.g. "720h" = 30 days. For dev you can use "1m" or "5m".
	graceStr := env("GRACE_PERIOD", "1m")

	grace, err := time.ParseDuration(graceStr)
	if err != nil {
		log.Fatalf("invalid GRACE_PERIOD: %v", err)
	}

	db, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}

	conn, err := grpc.Dial(storageAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("dial storage: %v", err)
	}
	storageClient := gv1.NewStorageServiceClient(conn)

	j := &janitor{
		db:          db,
		storage:     storageClient,
		gracePeriod: grace,
	}

	log.Printf("janitor started, grace period = %s", grace)

	// simple loop: run every minute
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		if err := j.runOnce(context.Background()); err != nil {
			log.Printf("janitor run failed: %v", err)
		}
		<-ticker.C
	}
}

func (j *janitor) runOnce(ctx context.Context) error {
	// select candidates
	rows, err := j.db.Query(ctx, `
SELECT id, object_key
FROM files
WHERE deleted_at IS NOT NULL
  AND deleted_at < NOW() - $1::interval
LIMIT 100
`, j.gracePeriod.String())
	if err != nil {
		return err
	}
	defer rows.Close()

	type row struct {
		id        int64
		objectKey string
	}

	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.objectKey); err != nil {
			return err
		}
		batch = append(batch, r)
	}

	if len(batch) == 0 {
		return nil
	}

	log.Printf("janitor: found %d files to purge", len(batch))

	for _, r := range batch {
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, err := j.storage.DeleteObject(cctx, &gv1.DeleteObjectRequest{ObjectKey: r.objectKey})
		cancel()
		if err != nil {
			log.Printf("delete object %q failed, keep row: %v", r.objectKey, err)
			continue
		}

		// only delete DB row if object delete succeeded
		_, err = j.db.Exec(ctx, `DELETE FROM files WHERE id = $1`, r.id)
		if err != nil {
			log.Printf("delete db row id=%d failed (object already gone): %v", r.id, err)
			continue
		}

		log.Printf("purged file id=%d key=%q", r.id, r.objectKey)
	}

	return nil
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
