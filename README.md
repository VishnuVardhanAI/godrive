# GoDrive – Cloud Storage Backend (Go + Microservices)

GoDrive is a cloud-storage backend built with Go and a modern microservice architecture.  
It supports authenticated uploads, direct-to-storage presigned URLs, event-driven ingest, and background cleanup — all running across independent services connected with gRPC and Docker.


## Features

- **User signup & login** (bcrypt + JWT)  
- **Presigned URLs** for direct file uploads to MinIO  
- **Event-driven ingest pipeline** using MinIO → NATS  
- **File metadata service** backed by Postgres  
- **Presigned download URLs** for secure file access  
- **Soft delete** with automatic background purge  
- **Microservices connected via gRPC**  
- **HTTP Gateway** for external clients (Gin)


## Architecture Overview

The system is split into several small services, each responsible for one thing:

### **Gateway**
The public-facing HTTP API.  
Handles authentication and routes user actions to internal services over gRPC.

### **Auth Service**
Manages signup, login, password hashing, and JWT verification.

### **Files Service**
Stores metadata in Postgres — filenames, owners, timestamps, sizes, soft-deletes.  
This service never touches raw file bytes.

### **Storage Service**
Generates presigned PUT/GET URLs and performs actual file deletion in MinIO.

### **Ingest Service**
Receives upload-completion events from MinIO through NATS.  
Extracts object info and confirms the upload by inserting metadata.

### **Janitor Service**
A background worker that periodically:  
- Finds soft-deleted files,  
- Deletes them from MinIO,  
- Removes metadata rows once cleanup succeeds.

### **Infrastructure**
- **Postgres** → metadata  
- **MinIO** → file storage  
- **NATS** → event bus  
- **Docker Compose** → dev orchestration

## How the Upload Flow Works

1. Client logs in and receives a JWT.  
2. Client requests an upload URL from the Gateway.  
3. Gateway → Storage Service: “Give me a presigned PUT URL.”  
4. Storage returns the signed URL.  
5. Client uploads file bytes directly to MinIO.  
6. MinIO emits an object-created event to NATS.  
7. Ingest Service receives the event, parses the object key,  
   and calls Files Service to insert metadata.  
8. The file now appears in `/files`.


## Delete Flow (Soft Delete + Background Purge)

1. Client calls `DELETE /files/:id`.  
2. Files Service marks the DB row with `deleted_at`.  
3. Janitor Service routinely:  
   - Finds expired soft-deleted rows  
   - Asks Storage Service to remove the object  
   - Deletes the metadata row if successful  

Keeps deletes fast for the user and reliable on the backend.

## Tech Stack

- Go 1.22+  
- Gin  
- gRPC + Protocol Buffers  
- Postgres (pgx)  
- MinIO (S3-compatible object store)  
- NATS  
- Docker & Docker Compose  
- Buf (for proto codegen)
