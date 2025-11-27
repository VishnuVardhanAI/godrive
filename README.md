# GoDrive – Cloud Storage Backend in Go (Microservices)

GoDrive is a mini Google Drive–style backend built with Go microservices.  
It supports:

- User signup & login with JWT
- Presigned uploads directly to S3-compatible storage (MinIO)
- Event-driven ingest of completed uploads via NATS
- File metadata listing / download URLs
- Soft-delete with async background purge (janitor worker)
- Fully Dockerized stack (Postgres, MinIO, NATS, Jaeger, services)

---

## Features

- **Microservices architecture**
  - `gateway` – HTTP API (Gin) → internal gRPC
  - `auth` – user auth, password hashing, JWT issuing / verification
  - `files` – file metadata & ownership in Postgres
  - `storage` – S3/MinIO presigned URLs & object deletion
  - `ingest` – consumes MinIO → NATS events, confirms uploads
  - `janitor` – async soft-delete cleanup (deletes objects + DB rows)

- **Auth**
  - Email + password signup
  - Bcrypt hashing
  - Stateless JWT access tokens (HS256)
  - Gateway middleware for `Authorization: Bearer <token>`

- **File handling**
  - Presigned PUT URLs → client uploads directly to MinIO
  - Files are stored under `user/<uid>/<timestamp>_<filename>`
  - Metadata stored in Postgres (`files` table)
  - Listing supports pagination
  - Download URLs are presigned GET URLs from storage service

- **Deletion model**
  - `DELETE /files/:id` → marks row as soft-deleted (`deleted_at`)
  - `janitor` periodically:
    - finds soft-deleted rows older than a grace period
    - calls `storage.DeleteObject` to delete from MinIO
    - removes the DB row if storage delete succeeds

- **Infra**
  - Postgres 16
  - MinIO (S3-compatible)
  - NATS
  - Docker Compose for local dev

---

## Architecture Overview

### High-level architecture

```mermaid
graph TD
    Client[Client (Postman / Frontend)] -->|HTTP JSON| Gateway

    subgraph Services
        Gateway[Gateway (Gin HTTP)] -->|gRPC| Auth[AuthService]
        Gateway -->|gRPC| Files[FilesService]
        Gateway -->|gRPC| Storage[StorageService]

        Files -->|gRPC (presign download)| Storage
        Ingest[IngestService] -->|gRPC (ConfirmUpload)| Files
        Janitor[JanitorService] -->|gRPC (DeleteObject)| Storage
        Janitor -->|SQL| PG[(Postgres)]
        Auth -->|SQL| PG
        Files -->|SQL| PG
    end

    subgraph Infra
        MinIO[(MinIO S3)]
        NATS[(NATS)]
    end

    Storage -->|S3 SDK| MinIO
    MinIO -->|Bucket Notifications| NATS
    NATS -->|subject godrive.uploaded| Ingest
