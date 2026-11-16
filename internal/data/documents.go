package data

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Document struct {
	ID        int64          `json:"id"`
	Type      string         `json:"type"`
	Fields    map[string]any `json:"fields"`
	CreatedAt time.Time      `json:"created_at"`
	Version   int32          `json:"-"`
}

type DocumentModel struct {
	pool *pgxpool.Pool
}

func (m DocumentModel) Insert(ctx context.Context, document *Document) error {
	query := `
		INSERT INTO documents (type, fields)
		VALUES ($1, $2)
		RETURNING id, type, fields, created_at, version`

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return m.pool.QueryRow(ctx, query, document.Type, document.Fields).Scan(
		&document.ID,
		&document.Type,
		&document.Fields,
		&document.CreatedAt,
		&document.Version,
	)
}

func (m DocumentModel) Get(ctx context.Context, id int64) (Document, error) {
	query := `
		SELECT id, type, fields, created_at, version
		FROM documents
		WHERE id = $1`

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var document Document
	err := m.pool.QueryRow(ctx, query, id).Scan(
		&document.ID,
		&document.Type,
		&document.Fields,
		&document.CreatedAt,
		&document.Version,
	)

	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return Document{}, ErrNoRecord
		default:
			return Document{}, err
		}
	}

	return document, nil
}
