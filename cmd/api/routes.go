package main

import (
	"net/http"

	"github.com/julienschmidt/httprouter"
)

func (app *application) routes() http.Handler {
	router := httprouter.New()
	router.MethodNotAllowed = http.HandlerFunc(app.methodNotAllowedResponse)
	router.NotFound = http.HandlerFunc(app.notFoundResponse)

	// health & metrics
	router.HandlerFunc(http.MethodGet, "/health", app.healthHandler)

	// documents
	router.HandlerFunc(http.MethodPost, "/v1/documents", app.uploadDocumentHandler)
	router.HandlerFunc(http.MethodGet, "/v1/documents/:id/review", app.getReviewHandler)
	router.HandlerFunc(http.MethodPatch, "/v1/documents/:id/review", app.updateReviewHandler)
	router.HandlerFunc(http.MethodPost, "/v1/documents/:id/review/accept", app.acceptReviewHandler)
	router.HandlerFunc(http.MethodPost, "/v1/documents/:id/review/reject", app.rejectReviewHandler)

	router.HandlerFunc(http.MethodGet, "/v1/documents/:id", app.getDocumentHandler)
	router.HandlerFunc(http.MethodDelete, "/v1/documents/:id", app.deleteDocumentHandler)

	router.HandlerFunc(http.MethodGet, "/v1/documents/:id/exports/docx", app.generateDOCXHandler)
	router.HandlerFunc(http.MethodGet, "/v1/documents/:id/exports/pdf", app.generatePDFHandler)

	return app.recoverPanic(router)
}
