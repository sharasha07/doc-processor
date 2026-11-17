# doc-processor

![CI](https://github.com/sharasha07/doc-processor/actions/workflows/ci.yml/badge.svg)

AI-Powered Document Processing &amp; Autofill System

## Architecture

```text
doc-processor/
├── cmd/
│   └── api/          # Server entry point
├── internal/
│   └── data/         # Database operations and data access
├── migrations/       # Database schema migration files
├── templates/        # Document templates and their schemas
├── Makefile          # Common development commands
└── README.md         # Project documentation
```

## Endpoints

### Health & Metrics

- `GET /health` - Check application health.
- `GET /metrics` - Retrieve application metrics.

### Templates

- `POST /v1/templates` - Create a document template and its schema.

### Document Upload & Review

- `POST /v1/documents` - Upload a document and temporarily store its extracted data.
- `GET /v1/documents/:id/review` - Retrieve temporarily stored document data for review.
- `PATCH /v1/documents/:id/review` - Update temporarily stored document data.
- `POST /v1/documents/:id/review/accept` - Accept the reviewed data and persistently store it.
- `POST /v1/documents/:id/review/reject` - Reject the reviewed data and delete it from temporary storage.

### Document Management

- `GET /v1/documents/:id` - Retrieve persistently stored document data.
- `DELETE /v1/documents/:id` - Delete persistently stored document data.

### Document Export

- `GET /v1/documents/:id/exports/docx` - Export persistently stored document data as a DOCX file.
- `GET /v1/documents/:id/exports/pdf` - Export persistently stored document data as a PDF file.

## Setup instructions
