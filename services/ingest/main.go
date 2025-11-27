package main

import (
	"context"
	"encoding/json"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	gv1 "godrive/proto/godrive/v1"

	"github.com/nats-io/nats.go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Minimal S3/MinIO event payload model.
type minioEvent struct {
	Records []struct {
		EventName string `json:"eventName"`
		S3        struct {
			Bucket struct {
				Name string `json:"name"`
			} `json:"bucket"`
			Object struct {
				Key  string `json:"key"`
				Size int64  `json:"size"`
			} `json:"object"`
		} `json:"s3"`
	} `json:"Records"`
}

func main() {
	natsURL := env("NATS_URL", "nats://localhost:4222")
	filesAddr := env("FILES_ADDR", "files:50052")

	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatalf("connect NATS: %v", err)
	}
	defer nc.Drain()

	filesConn, err := grpc.NewClient(
		filesAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Fatalf("dial files: %v", err)
	}
	defer filesConn.Close()

	filesClient := gv1.NewFilesServiceClient(filesConn)

	const subject = "godrive.uploaded"

	_, err = nc.QueueSubscribe(subject, "ingest-workers", func(msg *nats.Msg) {
		if err := handleEvent(context.Background(), filesClient, msg.Data); err != nil {
			log.Printf("handleEvent failed: %v", err)
		}
	})
	if err != nil {
		log.Fatalf("subscribe NATS: %v", err)
	}

	log.Println("ingest service listening on NATS subject:", subject)

	select {}
}

func handleEvent(ctx context.Context, filesClient gv1.FilesServiceClient, data []byte) error {
	var evt minioEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return err
	}

	for _, rec := range evt.Records {
		if !strings.HasPrefix(rec.EventName, "s3:ObjectCreated:") {
			continue
		}

		rawKey := rec.S3.Object.Key

		decodedKey, err := url.QueryUnescape(rawKey)
		if err != nil {
			log.Printf("failed to unescape key %q: %v", rawKey, err)
			continue
		}

		objectKey := decodedKey
		size := rec.S3.Object.Size

		ownerID, filename, ok := parseObjectKey(objectKey)
		if !ok {
			log.Printf("skip object with unexpected key format: %q", objectKey)
			continue
		}

		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err = filesClient.ConfirmUpload(cctx, &gv1.ConfirmUploadRequest{
			OwnerId:   ownerID,
			ObjectKey: objectKey,
			Filename:  filename,
			Mime:      "application/octet-stream",
			SizeBytes: size,
		})
		cancel()

		if err != nil {
			log.Printf("ConfirmUpload failed for %q: %v", objectKey, err)
		} else {
			log.Printf("confirmed upload: uid=%d key=%q", ownerID, objectKey)
		}
	}

	return nil
}

// Expect keys like: user/<uid>/<timestamp>_<filename>
func parseObjectKey(key string) (ownerID int64, filename string, ok bool) {
	parts := strings.Split(key, "/")
	if len(parts) < 3 || parts[0] != "user" {
		return 0, "", false
	}

	uidStr := parts[1]
	last := parts[len(parts)-1]

	idx := strings.Index(last, "_")
	if idx <= 0 || idx+1 >= len(last) {
		filename = last
	} else {
		filename = last[idx+1:]
	}

	uid, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil {
		return 0, "", false
	}

	return uid, filename, true
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
