GoDrive – Cloud Storage Backend (Go Microservices)

GoDrive is a cloud-storage backend built with Go and a modern microservice architecture.
It supports authenticated uploads, event-driven processing, and background cleanup — all running across independent services tied together with gRPC and Docker.

Features

User signup & login (bcrypt + JWT)

Presigned URLs for direct file uploads to MinIO

Event-driven ingest pipeline using MinIO bucket notifications + NATS

Metadata service backed by Postgres (file info, pagination, ownership)

Download URLs via presigned GET

Soft delete with a background worker that removes old files from storage

Microservices architecture connected via gRPC

Single HTTP gateway for the outside world (Gin)

Architecture Overview (plain English)

The system is split into several small services:

Gateway

The public-facing HTTP API.
All external requests go through this. It talks to internal services using gRPC.

Auth Service

Handles signup, login, password hashing, and JWT verification.

Files Service

Manages file metadata in Postgres.
Does not store file bytes—just file info, ownership, timestamps, and soft-delete flags.

Storage Service

Generates presigned upload/download URLs and deletes objects from MinIO.

Ingest Service

Listens to MinIO → NATS events.
Whenever a user finishes uploading through a presigned URL, MinIO emits an event, and the ingest service confirms the upload by inserting a row into Postgres.

Janitor Service

Background worker that periodically scans for files that were soft-deleted and are old enough to purge.
If storage deletion succeeds, it removes the metadata row.

Infrastructure

Postgres for user & file metadata

MinIO for actual file storage

NATS for upload-completed events

Docker Compose to run everything locally

How the Upload Flow Works

Client logs in and receives a JWT.

Client asks the gateway for an upload URL.

Gateway → Storage Service: “generate a presigned URL for this file.”

Storage returns a signed PUT URL.

Client uploads file bytes directly to MinIO (backend never touches them).

MinIO emits an “object created” event to NATS.

Ingest Service receives the event, extracts the user + filename, and calls Files Service to record the metadata.

File now appears in GET /files.

This mirrors how S3-backed systems handle uploads.

Delete Flow (Soft Delete + Background Purge)

Client sends DELETE /files/:id

Files Service marks the row with a deleted_at timestamp

A background worker (janitor) periodically:

Finds old soft-deleted rows

Calls Storage Service to remove the file from MinIO

Deletes the DB row if removal succeeds

This avoids long waits during a DELETE and follows the pattern used by most cloud storage platforms.

Tech Stack

Go 1.22+

Gin (Gateway)

gRPC + Protocol Buffers

Postgres (pgx)

MinIO (S3-compatible object storage)

NATS (event bus)

Docker & Docker Compose

Buf (for proto generation)
