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

sequenceDiagram
    participant C as Client
    participant G as Gateway
    participant A as AuthService
    participant S as StorageService
    participant M as MinIO
    participant I as IngestService
    participant F as FilesService
    participant PG as Postgres

    C->>G: POST /login (email, password)
    G->>A: Login(Credentials)
    A-->>G: Token (JWT)
    G-->>C: { token }

    C->>G: POST /files/upload-intent (JWT, filename, mime, size)
    G->>S: PresignUpload(object_key, mime, size)
    S->>M: Generate presigned PUT URL
    M-->>S: URL
    S-->>G: PresignUploadResponse(url)
    G-->>C: { upload_url, object_key }

    C->>M: PUT upload_url (file bytes)

    M-->>NATS: event "godrive.uploaded"
    NATS-->>I: event payload
    I->>F: ConfirmUpload(owner_id, object_key, filename, size)
    F->>PG: INSERT INTO files(...)
    PG-->>F: row id
    F-->>I: ConfirmUploadResponse

sequenceDiagram
    participant C as Client
    participant G as Gateway
    participant F as FilesService
    participant PG as Postgres
    participant J as Janitor
    participant S as StorageService
    participant M as MinIO

    C->>G: DELETE /files/:id (JWT)
    G->>F: Delete(owner_id, file_id)
    F->>PG: UPDATE files SET deleted_at = now()
    PG-->>F: rows affected
    F-->>G: { ok: true }
    G-->>C: { deleted: id }

    loop every minute (janitor)
        J->>PG: SELECT id, object_key FROM files WHERE deleted_at < now() - grace
        PG-->>J: rows to purge
        J->>S: DeleteObject(object_key)
        S->>M: Delete object
        M-->>S: OK
        S-->>J: { ok: true }
        J->>PG: DELETE FROM files WHERE id = ?
        PG-->>J: OK
    end
