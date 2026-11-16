package data

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNoRecord = errors.New("no record found")
)

type Models struct {
	Documents DocumentModel
}

func NewModels(pool *pgxpool.Pool) Models {
	return Models{
		Documents: DocumentModel{pool: pool},
	}
}
