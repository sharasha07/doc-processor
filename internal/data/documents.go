package data

import (
	"context"
	"errors"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Document struct {
	ID        uuid.UUID      `json:"id"`
	Type      string         `json:"type"`
	Fields    map[string]any `json:"fields"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type DocumentModel struct {
	pool *pgxpool.Pool
}

func (m DocumentModel) Insert(ctx context.Context, document *Document) error {
	query := `
		INSERT INTO documents (id, type, fields)
		VALUES ($1, $2, $3)
		RETURNING id, type, fields, created_at, updated_at`

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	args := []any{document.ID, document.Type, document.Fields}

	return m.pool.QueryRow(ctx, query, args...).Scan(
		&document.ID,
		&document.Type,
		&document.Fields,
		&document.CreatedAt,
		&document.UpdatedAt,
	)
}

func (m DocumentModel) Get(ctx context.Context, id uuid.UUID) (Document, error) {
	query := `
		SELECT id, type, fields, created_at, updated_at
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
		&document.UpdatedAt,
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

func (m DocumentModel) Delete(ctx context.Context, id uuid.UUID) error {
	query := `
		DELETE FROM documents
		WHERE id = $1`

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tag, err := m.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if tag.RowsAffected() == 0 {
		return ErrNoRecord
	}

	return nil
}
