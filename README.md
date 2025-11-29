# GoDrive – Cloud Storage Backend (Go + Microservices)

GoDrive is a cloud-storage backend that lets users upload, store, and manage files — similar to the core backend behind services like Google Drive or Dropbox.  
It handles user authentication, direct-to-storage uploads, file metadata management, and automated cleanup, all running across independent services.

Under the hood, GoDrive uses a modern microservice architecture built with Go, gRPC, PostgreSQL, MinIO (S3-compatible object storage), and NATS for asynchronous background processing.

---

## How It Works (High-Level)

1. **Users sign up and log in** using a JWT-based auth service.
2. **Uploads happen via presigned URLs**, which allow files to go directly to MinIO without passing through backend services.
3. **MinIO triggers upload events**, which are published to NATS.
4. **Background workers consume these events** and insert file metadata into PostgreSQL.
5. **Files can be soft-deleted**, and a cleanup worker permanently removes them after a grace period.
6. **All internal services communicate over gRPC**, while external clients use an HTTP gateway built with Gin.

This design mirrors real cloud-storage architectures: storage is decoupled, metadata is centralized, and all heavy processing is event-driven.

---

## Features

- **User authentication** (bcrypt + JWT)
- **Presigned URLs** for direct file uploads to MinIO
- **Event-driven background processing** (MinIO → NATS → worker)
- **File metadata service** backed by PostgreSQL
- **Presigned download URLs** for secure file access
- **Soft deletion** with automatic cleanup workers
- **Independent microservices** communicating over gRPC
- **HTTP Gateway** (Gin) for external access
- **Containerized setup** using Docker Compose

---

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

---

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

---

## Tech Stack

- **Language:** Go  
- **APIs:** gRPC, REST (Gin)  
- **Storage:** MinIO (S3-compatible object store), PostgreSQL  
- **Messaging:** NATS  
- **Auth:** JWT, bcrypt  
- **Containerization:** Docker Compose  
- **Dev Tools:** Makefiles, Protoc, Postman  
